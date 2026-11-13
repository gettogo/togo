import Foundation

/// One live connection to a machine. `HostMonitor` owns its lifetime: it calls `open`
/// once, then `snapshot`, reads `events`, and calls `close` when it is done.
public protocol SessionSource: Sendable {
    func open() async throws -> (HelperPresence, Set<String>)
    /// Subscribes to events, or lists sessions and approvals when the host has no `events.v1`.
    func snapshot() async throws -> EventSnapshot
    /// Empty when the host has no `events.v1`. Ends when the connection does. Single consumer: HostMonitor.
    var events: AsyncStream<EventEnvelope> { get }
    func call(_ op: SessionOp) async throws -> Session?
    func canAnswerRequests() async -> Bool
    /// Applies to the next `snapshot`, which subscribes with this foreground flag.
    func setForeground(_ on: Bool) async
    /// The machine's keep-awake state, or nil when the host has no `awake.v1`.
    func awakeStatus() async throws -> AwakeStatus?
    /// The machine's network addresses for waking it, or nil when the host has no `hostinfo.v1`.
    func hostInfo() async throws -> HostInfo?
    func close() async
}

extension SessionSource {
    public func canAnswerRequests() async -> Bool { false }
    public func setForeground(_ on: Bool) async {}
    public func awakeStatus() async throws -> AwakeStatus? { nil }
    public func hostInfo() async throws -> HostInfo? { nil }
}

public enum SessionOp: Sendable {
    case create(CreateSessionParams)
    case kill(String)
    case rename(String, String)
    case setMute(String, Bool)
    case answer(String, Decision)
}

public typealias SourceFactory = @Sendable (HostRecord) async throws -> any SessionSource

/// The helper over SSH: `togo serve --stdio` on an exec channel, spoken to by `HelperClient`.
///
/// It never asks the user anything. A missing password, a one-time code or an unknown
/// host key fails the connect, and the monitor reports that sign-in is needed.
public struct HelperSource: SessionSource {
    public let events: AsyncStream<EventEnvelope>
    private let link: HelperLink

    public init(record: HostRecord, env: SessionEnvironment) {
        let command = record.helperPath.map { shellQuote([$0, "serve", "--stdio"]) } ?? HelperCommand.serveStdio
        self.init(host: record.profile.name, clock: SystemClock()) {
            let connection = SSHConnection(
                profile: record.profile, keys: env.keys, secrets: env.secrets, biometrics: env.biometrics,
                hostKeys: env.hostKeys, prompter: SilentPrompter()
            )
            do {
                try await connection.connect()
                return HelperDial(channel: try await connection.openExec(command), hangUp: { await connection.close() })
            } catch {
                await connection.close()
                throw error
            }
        }
    }

    /// Runs over a connection that is already signed in, such as one the user just answered a
    /// one-time code for. The source owns it from here and closes it when done.
    public init(record: HostRecord, connected connection: SSHConnection) {
        let command = record.helperPath.map { shellQuote([$0, "serve", "--stdio"]) } ?? HelperCommand.serveStdio
        self.init(host: record.profile.name, clock: SystemClock()) {
            do {
                return HelperDial(channel: try await connection.openExec(command), hangUp: { await connection.close() })
            } catch {
                await connection.close()
                throw error
            }
        }
    }

    init(
        host: String, clock: any LinkClock, callTimeout: Duration = .seconds(10),
        dial: @escaping @Sendable () async throws -> HelperDial
    ) {
        // A consumer that falls behind loses the oldest events; the seq gap makes it resubscribe.
        let (events, sink) = AsyncStream<EventEnvelope>.makeStream(bufferingPolicy: .bufferingNewest(256))
        self.events = events
        link = HelperLink(host: host, clock: clock, callTimeout: callTimeout, dial: dial, events: sink)
    }

    public func open() async throws -> (HelperPresence, Set<String>) { try await link.open() }
    public func snapshot() async throws -> EventSnapshot { try await link.snapshot() }
    public func call(_ op: SessionOp) async throws -> Session? { try await link.call(op) }
    public func canAnswerRequests() async -> Bool { await link.canAnswerRequests() }
    public func setForeground(_ on: Bool) async { await link.setForeground(on) }
    public func awakeStatus() async throws -> AwakeStatus? { try await link.awakeStatus() }
    public func hostInfo() async throws -> HostInfo? { try await link.hostInfo() }
    public func close() async { await link.close() }
}

/// The channel `togo serve --stdio` runs on, and how to end the connection under it.
struct HelperDial: Sendable {
    let channel: any ExecChannel
    let hangUp: @Sendable () async -> Void
}

public enum HelperSourceError: Error, Equatable, LocalizedError {
    case missing(host: String)
    case outdated(host: String)
    /// The helper is newer than this app; only updating the app helps.
    case newer(host: String)
    /// The helper is there but stopped before it answered, such as a daemon that could not
    /// start. Unlike `missing`, retrying can help, so the monitor keeps trying.
    case didNotStart(host: String, status: Int32)

    public var errorDescription: String? {
        switch self {
        case .missing(let host): String(localized: "The Togo helper is not installed on \(host).", bundle: .module)
        case .outdated(let host): String(localized: "The Togo helper on \(host) is too old for this app. Update it on \(host).", bundle: .module)
        case .newer(let host): String(localized: "The Togo helper on \(host) is newer than this app. Update Togo from the App Store.", bundle: .module)
        case .didNotStart(let host, _):
            String(localized: "The Togo helper on \(host) is installed but did not start. Togo keeps trying; running togo doctor on \(host) shows why.", bundle: .module)
        }
    }

    /// Exit status of a shell that could not find the command.
    static let notFound: Int32 = 127
}

actor HelperLink {
    /// The helper drops the foreground lease 45 s after the last request.
    static let keepaliveInterval: Duration = .seconds(20)

    private let host: String
    private let clock: any LinkClock
    private let callTimeout: Duration
    private let dial: @Sendable () async throws -> HelperDial
    private let sink: AsyncStream<EventEnvelope>.Continuation
    private var hangUp: (@Sendable () async -> Void)?
    private var client: HelperClient?
    private var isClosed = false
    private var presence: HelperPresence = .unknown
    private var capabilities: Set<String> = []
    private var foreground = true
    private var forwarder: Task<Void, Never>?
    private var keepalive: Task<Void, Never>?

    init(
        host: String, clock: any LinkClock, callTimeout: Duration,
        dial: @escaping @Sendable () async throws -> HelperDial, events: AsyncStream<EventEnvelope>.Continuation
    ) {
        self.host = host
        self.clock = clock
        self.callTimeout = callTimeout
        self.dial = dial
        sink = events
    }

    func open() async throws -> (HelperPresence, Set<String>) {
        let dialed = try await dial()
        guard !isClosed else {
            await dialed.hangUp()
            throw HelperError.closed
        }
        hangUp = dialed.hangUp
        let client = HelperClient(channel: dialed.channel, callTimeout: callTimeout)
        self.client = client
        do {
            let hello = try await client.hello(client: SessionAttach.client)
            presence = .present(version: hello.helper.version)
            capabilities = Set(hello.capabilities)
        } catch HelperError.closed {
            // SSH is up, so a channel that ends before hello means the helper did not run:
            // not found (127, or no status), or found but stopped, which is worth retrying.
            if let status = dialed.channel.exitStatus, status != HelperSourceError.notFound, status != 0 {
                await dialed.hangUp()
                self.client = nil
                hangUp = nil
                throw HelperSourceError.didNotStart(host: host, status: status)
            }
            presence = .missing
        } catch HelperError.rpc(let error) where error.code == RPCErrorCode.unsupportedVersion {
            presence = error.helperIsAhead ? .newer : .outdated("")
        }
        if capabilities.contains(Capability.events) {
            let notifications = client.notifications
            forwarder = Task { [sink] in
                for await event in notifications { sink.yield(event) }
                sink.finish()
            }
        } else {
            sink.finish()
        }
        return (presence, capabilities)
    }

    func snapshot() async throws -> EventSnapshot {
        let client = try readyClient()
        if capabilities.contains(Capability.events) {
            let snapshot = try await client.subscribe(foreground: foreground)
            restartKeepalive()
            return snapshot
        }
        let sessions = try await client.listSessions()
        let approvals = capabilities.contains(Capability.approvals) ? try await client.approvals(sessionId: nil) : []
        return EventSnapshot(seq: 0, sessions: sessions, approvals: approvals)
    }

    func call(_ op: SessionOp) async throws -> Session? {
        let client = try readyClient()
        do {
            return try await perform(op, on: client)
        } catch HelperError.timeout(let method) {
            // A slow answer or a dead connection: one more ping tells which. A dead one is
            // closed, which ends `events` and makes the monitor reconnect.
            if capabilities.contains(Capability.events) {
                Task { [weak self] in
                    if await HelperLiveness.isDead(ping: { try await client.keepalive() }) { await self?.close() }
                }
            }
            throw HelperError.timeout(method: method)
        }
    }

    func canAnswerRequests() async -> Bool {
        guard !isClosed, let client else { return false }
        return await client.canAnswerRequests
    }

    private func perform(_ op: SessionOp, on client: HelperClient) async throws -> Session? {
        switch op {
        case .create(let params):
            return try await client.createSession(params)
        case .kill(let id):
            try await client.killSession(id: id)
            return nil
        case .rename(let id, let name):
            return try await client.renameSession(id: id, name: name)
        case .setMute(let id, let muted):
            return try await client.setMute(id: id, muted: muted)
        case .answer(let requestId, let decision):
            try await client.answer(requestId: requestId, decision: decision)
            return nil
        }
    }

    func awakeStatus() async throws -> AwakeStatus? {
        guard capabilities.contains(Capability.awake) else { return nil }
        return try await readyClient().awakeStatus()
    }

    func hostInfo() async throws -> HostInfo? {
        guard capabilities.contains(Capability.hostInfo) else { return nil }
        return try await readyClient().hostInfo()
    }

    func setForeground(_ on: Bool) {
        foreground = on
        if !on {
            keepalive?.cancel()
            keepalive = nil
        }
    }

    /// State is cleared before the first await: a snapshot finishing while this waits
    /// must find the link closed and start nothing.
    func close() async {
        guard !isClosed else { return }
        isClosed = true
        forwarder?.cancel()
        keepalive?.cancel()
        keepalive = nil
        let client = self.client, hangUp = self.hangUp
        self.client = nil
        self.hangUp = nil
        sink.finish()
        await client?.close()
        await hangUp?()
    }

    private func readyClient() throws -> HelperClient {
        switch presence {
        case .missing: throw HelperSourceError.missing(host: host)
        case .outdated: throw HelperSourceError.outdated(host: host)
        case .newer: throw HelperSourceError.newer(host: host)
        case .unknown, .present: break
        }
        guard let client else { throw HelperError.closed }
        return client
    }

    /// Any request renews the lease, so the timer counts from the latest subscribe. The same
    /// pings find a connection that went dead without closing: the link is then closed, which
    /// ends `events`, and the monitor marks the machine and reconnects.
    private func restartKeepalive() {
        keepalive?.cancel()
        keepalive = nil
        guard foreground, !isClosed, let client else { return }
        keepalive = Task { [clock, weak self] in
            let outcome = await HelperLiveness.watch(clock: clock, interval: Self.keepaliveInterval) {
                try await client.keepalive()
            }
            if outcome == .dead || outcome == .closed { await self?.close() }
        }
    }
}

/// Background connections must not put up sign-in prompts; declining makes the
/// connect fail with an error the monitor reports as needing sign-in.
private struct SilentPrompter: ConnectionPrompter {
    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? { nil }
    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool { false }
}
