import Foundation

/// Reaches a machine for a sign-in callback: through the SSH connection behind its helper
/// channel, whichever route that connection took (LAN, `.local`, a Tailscale name or a 100.x
/// address). Nothing here ever names the machine's address; a forwarded connection is a
/// direct-tcpip channel on that SSH session to the machine's own loopback port.
@MainActor public protocol CallbackTunnel: AnyObject {
    /// The connection to open channels on. `fresh` drops one that closed and connects again,
    /// on whatever route works now.
    func opener(hostID: UUID, fresh: Bool) async throws -> any TCPChannelOpener
    /// The loopback address the agent listens on for `port`: "127.0.0.1" or "::1".
    func loopbackAddress(hostID: UUID, sessionID: String?, port: Int) async -> String
}

/// Listens on 127.0.0.1:PORT on this phone, the very port a sign-in's redirect names, and
/// carries every connection to PORT on the machine. The browser's redirect to
/// `http://localhost:PORT/callback?code=…` then reaches the agent waiting there.
///
/// When the SSH connection drops mid sign-in (a network change, the app paused), the forward
/// opens again on the same port over a new connection, so a redirect that comes later still
/// lands.
@MainActor public final class CallbackForward {
    public enum State: Equatable, Sendable {
        case idle
        case listening
        /// Another listener has the port on this phone.
        case busy
        case reconnecting
        case lost(String)
        case stopped
    }

    public let port: Int
    public private(set) var state: State = .idle
    /// A connection came in: the browser followed the redirect.
    public var onCallback: (@MainActor () -> Void)?
    /// The forward was lost for good.
    public var onLost: (@MainActor (Loss) -> Void)?

    public enum Loss: Equatable, Sendable {
        /// Something else on this phone took the port while reconnecting.
        case portTaken
        /// The machine couldn't be reached again; the last error's text.
        case unreachable(String)
    }

    private let tunnel: any CallbackTunnel
    private let hostID: UUID
    private let sessionID: String?
    private var forward: LocalForward?
    private var watching: Task<Void, Never>?
    private var target: ForwardTarget?
    /// Pauses between reconnects; the app may be suspended through some of them.
    private let backoff: [Duration]

    public init(
        tunnel: any CallbackTunnel, hostID: UUID, sessionID: String?, port: Int,
        backoff: [Duration] = [.seconds(1), .seconds(2), .seconds(4), .seconds(8), .seconds(15), .seconds(30)]
    ) {
        self.tunnel = tunnel
        self.hostID = hostID
        self.sessionID = sessionID
        self.port = port
        self.backoff = backoff
    }

    /// Opens the forward. Returns false when the port is taken on this phone.
    public func start() async throws -> Bool {
        guard state == .idle || state == .stopped else { return state == .listening }
        let address = await tunnel.loopbackAddress(hostID: hostID, sessionID: sessionID, port: port)
        guard let target = ForwardTarget(ListeningPort(port: port, address: address, pid: 0, command: "")) else {
            state = .busy
            return false
        }
        self.target = target
        let opener = try await tunnel.opener(hostID: hostID, fresh: false)
        guard try await bind(opener, target) else {
            state = .busy
            return false
        }
        state = .listening
        return true
    }

    public func stop() async {
        state = .stopped
        watching?.cancel()
        watching = nil
        let open = forward
        forward = nil
        await open?.stop()
    }

    // MARK: Listening

    /// Binds the exact port. False when it is taken; throws when the machine can't be reached.
    private func bind(_ opener: any TCPChannelOpener, _ target: ForwardTarget) async throws -> Bool {
        let forward = LocalForward(
            opener: opener, target: target, preferredLocalPort: port, exactLocalPort: true,
            onAccept: { [weak self] in Task { @MainActor in self?.accepted() } }
        )
        do {
            _ = try await forward.start()
        } catch {
            await forward.stop()
            return false
        }
        self.forward = forward
        watch(forward)
        return true
    }

    private func watch(_ forward: LocalForward) {
        watching?.cancel()
        watching = Task { [weak self] in
            for await event in forward.events where event == .stopped {
                await self?.dropped(forward)
                return
            }
        }
    }

    private func accepted() {
        guard state == .listening else { return }
        onCallback?()
    }

    /// The forward stopped without `stop()`: its SSH connection went. Opens it again on a new one.
    private func dropped(_ old: LocalForward) async {
        guard state == .listening, forward === old, let target else { return }
        forward = nil
        state = .reconnecting
        var reason = ""
        for pause in [Duration.zero] + backoff {
            try? await Task.sleep(for: pause)
            guard state == .reconnecting, !Task.isCancelled else { return }
            do {
                let opener = try await tunnel.opener(hostID: hostID, fresh: true)
                guard state == .reconnecting else { return }
                if try await bind(opener, target) {
                    state = .listening
                    return
                }
                state = .busy
                onLost?(.portTaken)
                return
            } catch {
                reason = error.localizedDescription
            }
        }
        state = .lost(reason)
        onLost?(.unreachable(reason))
    }
}
