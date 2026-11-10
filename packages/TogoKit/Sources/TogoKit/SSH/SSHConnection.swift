@preconcurrency import Citadel
import Foundation
@preconcurrency import LocalAuthentication
import NIOCore
@preconcurrency import NIOSSH
import Security
import Synchronization

/// One authenticated SSH connection to a host profile, through its jump hosts.
///
/// Credentials are resolved before any socket opens: the biometric context is evaluated,
/// the key is loaded with it, a password is read or asked for. The handshake then signs
/// with an already-authorised key and never shows a prompt on the NIO event loop.
public actor SSHConnection: TCPChannelOpener {
    let profile: HostProfile
    private let keys: KeychainKeyStore
    private let secrets: any SecretStore
    private let biometrics: BiometricSession
    let hostKeys: any HostKeyStore
    private let prompter: any ConnectionPrompter
    private let connectTimeout: Duration
    private let credentialFallback: Bool
    let routeBook: RouteBook

    var clients: [SSHClient] = []
    var root: (any Channel)?
    /// The address the target hop reached, for Mosh's UDP flow.
    public internal(set) var remoteAddress: String?
    /// The route that won, as the machine's routes name it; nil through a jump host.
    public internal(set) var route: String?
    /// The interface a link-local `remoteAddress` was reached on, such as `en0`; the
    /// address names no machine without it.
    public internal(set) var remoteInterface: String?

    /// The SSH port, the same on every route.
    public nonisolated var port: Int { profile.endpoint.port }

    /// With `credentialFallback` off, only the profile's own credential is offered: a refused
    /// key ends in `.authFailed` instead of a password or keyboard-interactive prompt. A key
    /// the server accepts but wants a second step for, such as a one-time code, still prompts. Adding a
    /// machine uses this so a key that is not authorised yet can be explained as such.
    public init(
        profile: HostProfile, keys: KeychainKeyStore, secrets: any SecretStore, biometrics: BiometricSession,
        hostKeys: any HostKeyStore, prompter: any ConnectionPrompter, connectTimeout: Duration = .seconds(8),
        credentialFallback: Bool = true, routeBook: RouteBook = .shared
    ) {
        self.profile = profile
        self.keys = keys
        self.secrets = secrets
        self.biometrics = biometrics
        self.hostKeys = hostKeys
        self.prompter = prompter
        self.connectTimeout = connectTimeout
        self.credentialFallback = credentialFallback
        self.routeBook = routeBook
    }

    var isConnected: Bool { root?.isActive ?? false }

    public func connect() async throws {
        guard root == nil else { return }
        var credentials = try await prepareCredentials()
        if let routes = try await racedRoutes() {
            try await connectRacing(routes, credentials: &credentials)
            keepReplacedPassword(credentials)
            return
        }
        var parent: SSHClient?
        let hops = profile.jumps + [profile.endpoint]
        let autofill = PasswordAutofill()
        do {
            for (index, endpoint) in hops.enumerated() {
                let isTarget = index == hops.count - 1
                let (client, channel) = try await connectHop(
                    endpoint, via: parent, credentials: &credentials, mayAskPassword: isTarget,
                    autofill: isTarget ? autofill : nil
                )
                clients.append(client)
                parent = client
                if isTarget { root = channel }
            }
        } catch {
            await close()
            throw error
        }
        if profile.jumps.isEmpty {
            route = profile.endpoint.host
            await routeBook.recordUse(profile.endpoint.host, for: profile.id)
        }
        keepReplacedPassword(credentials)
    }

    /// The saved password could no longer be opened here; the one that just worked takes its place.
    private func keepReplacedPassword(_ credentials: Credentials) {
        if credentials.replacesSaved, let password = credentials.password {
            try? secrets.set(Data(password.utf8), for: Self.passwordAccount(profile.id), requireUserPresence: true)
        }
    }

    public func openExec(_ command: String) async throws -> any ExecChannel {
        SSHExecChannel(session: try await SessionChannel.open(on: try requireRoot(), pty: nil, command: command))
    }

    func openPTY(_ command: String, cols: Int, rows: Int) async throws -> SessionChannel {
        let pty = SSHChannelRequestEvent.PseudoTerminalRequest(
            wantReply: true, term: "xterm-256color", terminalCharacterWidth: cols, terminalRowHeight: rows,
            terminalPixelWidth: 0, terminalPixelHeight: 0, terminalModes: SSHTerminalModes([:])
        )
        return try await SessionChannel.open(on: try requireRoot(), pty: pty, command: command)
    }

    /// Opens a TCP connection from the target machine to `host:port` on that machine's side.
    /// Only the machine's own loopback addresses are reachable this way.
    public func openDirectTCPIP(host: String, port: Int) async throws -> any ExecChannel {
        guard ForwardTarget.isAllowed(host: host, port: port) else {
            throw DirectTCPIPError.failed(String(localized: "Only ports on the machine itself can be opened.", bundle: .module))
        }
        guard isConnected, let client = clients.last else { throw SSHError.channelClosed(exitStatus: nil) }
        return try await TCPIPChannel.open(on: client, host: host, port: port)
    }

    public func waitUntilClosed() async {
        guard let root else { return }
        let future = root.closeFuture
        let waiter = Mutex<CheckedContinuation<Void, Never>?>(nil)
        await withTaskCancellationHandler {
            await withCheckedContinuation { continuation in
                waiter.withLock { $0 = continuation }
                future.whenComplete { _ in waiter.withLock { $0.take() }?.resume() }
                if Task.isCancelled { waiter.withLock { $0.take() }?.resume() }
            }
        } onCancel: {
            waiter.withLock { $0.take() }?.resume()
        }
    }

    public func run(_ command: String, timeout: Duration) async throws -> (stdout: Data, stderr: Data, status: Int32) {
        let session = try await SessionChannel.open(on: try requireRoot(), pty: nil, command: command)
        return try await withThrowingTaskGroup(of: (stdout: Data, stderr: Data, status: Int32)?.self) { group in
            group.addTask {
                var stdout = Data(), stderr = Data()
                var status: Int32?
                for await event in session.events {
                    switch event {
                    case .stdout(let data): stdout += data
                    case .stderr(let data): stderr += data
                    case .exit(let code): status = code
                    case .closed: break
                    }
                    session.requestMore()
                }
                guard let status else { throw SSHError.channelClosed(exitStatus: nil) }
                return (stdout, stderr, status)
            }
            group.addTask {
                try await Task.sleep(for: timeout)
                return nil
            }
            let first = try await group.next() ?? nil
            group.cancelAll()
            await session.close()
            guard let first else { throw SSHError.timeout }
            return first
        }
    }

    public func close() async {
        for client in clients.reversed() {
            try? await client.close()
        }
        clients = []
        root = nil
    }

    private func requireRoot() throws -> any Channel {
        guard let root, root.isActive else { throw SSHError.channelClosed(exitStatus: nil) }
        return root
    }

    // MARK: Credentials

    struct Credentials: CustomStringConvertible {
        var key: NIOSSHPrivateKey?
        var password: String?
        var passwordWasSaved = false
        /// The saved password exists but this device can no longer unlock it.
        var replacesSaved = false

        var description: String { "Credentials(<redacted>)" }
    }

    private func prepareCredentials() async throws -> Credentials {
        let reason = "Unlock your SSH key for \(profile.endpoint.host)"
        switch profile.auth {
        case .key(let id):
            return Credentials(key: try await unlocking(reason: reason) { try keys.privateKey(id: id, context: $0) })
        case .password:
            let saved: Data?
            do {
                saved = try await unlocking(reason: reason) { try secrets.get(Self.passwordAccount(profile.id), context: $0) }
            } catch let error as SecretStoreError {
                // Unreadable even after a fresh unlock: ask for the password where someone can
                // answer, and keep the keychain's answer where no one can.
                let prompt = AuthPrompt.password(user: profile.endpoint.user, host: profile.endpoint.host)
                guard case .secret(let password) = await prompter.answer(prompt) else { throw error }
                return Credentials(password: password, replacesSaved: true)
            }
            if let saved {
                return Credentials(password: String(decoding: saved, as: UTF8.self), passwordWasSaved: true)
            }
            return Credentials(password: try await askPassword())
        }
    }

    /// Reads the key or password with the shared unlock. A read that context could not unlock
    /// drops it and asks once more for a fresh unlock, so the person sees a prompt rather than
    /// the same refusal; a second refusal is reported, and the next attempt starts fresh too.
    private func unlocking<T>(reason: String, _ read: (LAContext) throws -> T) async throws -> T {
        var tries = 0
        while true {
            tries += 1
            let context = try await authorise(reason: reason)
            do {
                return try read(context)
            } catch let error as SecretStoreError {
                await biometrics.invalidate()
                guard tries < 2, Self.isUnlockRefusal(error) else { throw error }
            }
        }
    }

    /// The keychain wanted an unlock the context did not carry, as opposed to a broken item.
    static func isUnlockRefusal(_ error: SecretStoreError) -> Bool {
        [errSecAuthFailed, errSecInteractionNotAllowed, -25330].contains(error.status)
    }

    private func authorise(reason: String) async throws -> LAContext {
        do {
            return try await biometrics.context(reason: reason)
        } catch let error as LAError where [.userCancel, .appCancel, .systemCancel].contains(error.code) {
            throw SSHError.cancelled
        }
    }

    private func askPassword() async throws -> String {
        let prompt = AuthPrompt.password(user: profile.endpoint.user, host: profile.endpoint.host)
        guard case .secret(let password) = await prompter.answer(prompt) else { throw SSHError.cancelled }
        return password
    }

    /// Where a machine's saved password lives in the secret store, by the profile id.
    public static func passwordAccount(_ id: UUID) -> String { "password:\(id.uuidString)" }

    // MARK: Hops

    /// `pinnedAs` checks the key against another address's pin, the machine's saved one, while
    /// racing its other routes; such a hop never asks to trust a new key.
    func connectHop(
        _ endpoint: HostEndpoint, via parent: SSHClient?, credentials: inout Credentials, mayAskPassword: Bool,
        autofill: PasswordAutofill?, pinnedAs: String? = nil, gate: RouteGate? = nil, token: Int = 0
    ) async throws -> (SSHClient, any Channel) {
        let hostID = pinnedAs ?? HostKeyTrust.hostID(endpoint)
        // Two passes at most: one to learn and trust a new host key, one to log in.
        var passes = 0
        while true {
            passes += 1
            let trusted = try await hostKeys.fingerprints(for: hostID)
            let validator = SnapshotHostKeyValidator(trusted: trusted, gate: gate, token: token)
            autofill?.load(credentials.password)
            let responder = KeyboardInteractiveResponder(prompter: prompter, autofill: autofill)
            let delegate = PreparedAuthDelegate(
                username: endpoint.user, key: credentials.key, password: credentials.password,
                keyboardInteractive: responder, keyboardInteractiveAfterKeyOnly: !credentialFallback
            )
            do {
                defer { responder.cancelPending() }
                return try await handshake(endpoint, via: parent, validator: validator, delegate: delegate)
            } catch {
                if validator.lost { throw RouteRace.Lost() }
                if Task.isCancelled || delegate.keyboardInteractiveCancelled { throw SSHError.cancelled }
                guard let presented = validator.presented else { throw Self.networkError(error) }
                if !trusted.contains(presented.fingerprint) {
                    if !trusted.isEmpty || gate != nil {
                        throw SSHError.hostKeyChanged(host: hostID, expected: trusted, presented: presented.fingerprint)
                    }
                    guard passes == 1, await prompter.trustNewHostKey(
                        host: endpoint.host, keyType: presented.keyType, fingerprint: presented.fingerprint
                    ) else { throw SSHError.hostKeyRejected }
                    try await hostKeys.trust(presented.fingerprint, for: hostID)
                    continue
                }
                // A key the server refused falls back to a password it offers, asked for once.
                if credentialFallback, mayAskPassword, credentials.password == nil, delegate.serverOffersPassword, passes <= 2 {
                    credentials = Credentials(key: nil, password: try await askPassword())
                    continue
                }
                // No credential offered means the link failed before or during auth setup.
                guard !delegate.tried.isEmpty || error is AuthExhausted else { throw Self.networkError(error) }
                throw SSHError.authFailed(tried: delegate.tried)
            }
        }
    }

    func handshake(
        _ endpoint: HostEndpoint, via parent: SSHClient?,
        validator: SnapshotHostKeyValidator, delegate: PreparedAuthDelegate
    ) async throws -> (SSHClient, any Channel) {
        var settings = SSHClientSettings(
            host: endpoint.host, port: endpoint.port,
            authenticationMethod: { .custom(delegate) }, hostKeyValidator: .custom(validator)
        )
        let parts = connectTimeout.components
        settings.connectTimeout = .milliseconds(parts.seconds * 1000 + parts.attoseconds / 1_000_000_000_000_000)
        settings.loginTimeout = KeyboardInteractiveResponder.loginTimeout
        let client: SSHClient
        if let parent {
            client = try await parent.jump(to: settings)
        } else {
            client = try await SSHClient.connect(to: settings)
        }
        // Session channels with a PTY and exec are opened on the root channel directly,
        // because Citadel opens a PTY only with a login shell.
        let channel = client.channel
        if parent == nil { await Self.keepAlive(channel) }
        // Mosh needs the address SSH reached, not a name that may resolve elsewhere later.
        if parent == nil, profile.jumps.isEmpty {
            remoteAddress = channel.remoteAddress?.ipAddress
            remoteInterface = Self.scopeInterface(channel.remoteAddress)
        }
        return (client, channel)
    }

    /// TCP keepalive on the socket: a link that stops passing traffic (the Mac slept, a NAT
    /// entry expired) fails after about 30 s of quiet instead of the system's two hours, so
    /// terminals and feeds on it see the close and reconnect. Best effort.
    static func keepAlive(_ channel: any Channel) async {
        let options: [(ChannelOptions.Types.SocketOption, SocketOptionValue)] = [
            (ChannelOptions.socketOption(.so_keepalive), 1),
            (ChannelOptions.tcpOption(.init(rawValue: TCP_KEEPALIVE)), 15),
            (ChannelOptions.tcpOption(.init(rawValue: TCP_KEEPINTVL)), 5),
            (ChannelOptions.tcpOption(.init(rawValue: TCP_KEEPCNT)), 3),
        ]
        for (option, value) in options {
            try? await channel.setOption(option, value: value).get()
        }
    }

    static func networkError(_ error: any Error) -> SSHError {
        if let error = error as? SSHError { return error }
        if case ChannelError.connectTimeout = error { return .timeout }
        if isRefused(error) { return .refused }
        return .unreachable(String(localized: "The machine could not be reached. Check the address and your network", bundle: .module))
    }

    /// True when the error, or any address's error inside a connect that tried several,
    /// is a refused TCP connection. NIO wraps those per address, so they are found by walking
    /// the error's fields rather than by its type.
    static func isRefused(_ error: any Error, depth: Int = 0) -> Bool {
        if let io = error as? IOError { return io.errnoCode == ECONNREFUSED }
        if let posix = error as? POSIXError { return posix.code == .ECONNREFUSED }
        guard depth < 4 else { return false }
        for child in Mirror(reflecting: error).children {
            if let nested = child.value as? any Error, isRefused(nested, depth: depth + 1) { return true }
            if let list = child.value as? [Any] {
                for element in list {
                    if let nested = element as? any Error, isRefused(nested, depth: depth + 1) { return true }
                    for field in Mirror(reflecting: element).children {
                        if let nested = field.value as? any Error, isRefused(nested, depth: depth + 1) { return true }
                    }
                }
            }
        }
        return false
    }
}

/// One direct-tcpip channel as a byte pipe. Reads are paced like `SSHExecChannel`: the
/// next read is asked for only after `incoming` accepted the previous one, so a slow
/// local reader stops the SSH window and the machine's side stops sending.
private final class TCPIPChannel: ExecChannel {
    static let bufferLimit = 256 << 10

    let incoming = ByteStream(limit: bufferLimit)
    private let channel: any Channel

    private init(channel: any Channel, reads: AsyncStream<Data>) {
        self.channel = channel
        let incoming = self.incoming
        Task {
            for await data in reads {
                guard await incoming.write(data) else { break }
                channel.read()
            }
            incoming.finish()
            try? await channel.close()
        }
    }

    static func open(on client: SSHClient, host: String, port: Int) async throws -> TCPIPChannel {
        let (reads, continuation) = AsyncStream<Data>.makeStream()
        let settings = SSHChannelType.DirectTCPIP(
            targetHost: host, targetPort: port, originatorAddress: try SocketAddress(ipAddress: "127.0.0.1", port: 0)
        )
        let channel: any Channel
        do {
            channel = try await client.createDirectTCPIPChannel(using: settings) { child in
                child.setOption(ChannelOptions.autoRead, value: false).flatMap {
                    child.pipeline.addHandler(TCPIPReader(continuation: continuation))
                }
            }
        } catch let error as NIOSSHError where error.type == .channelSetupRejected {
            continuation.finish()
            throw DirectTCPIPError(rejection: error)
        } catch {
            continuation.finish()
            throw error
        }
        channel.read()
        return TCPIPChannel(channel: channel, reads: reads)
    }

    func write(_ data: Data) async throws {
        do {
            try await channel.writeAndFlush(ByteBuffer(bytes: data))
        } catch {
            throw SSHError.channelClosed(exitStatus: nil)
        }
    }

    func close() async {
        try? await channel.close()
        incoming.finish()
    }
}

private final class TCPIPReader: ChannelInboundHandler, Sendable {
    typealias InboundIn = ByteBuffer

    private let continuation: AsyncStream<Data>.Continuation

    init(continuation: AsyncStream<Data>.Continuation) {
        self.continuation = continuation
    }

    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        continuation.yield(Data(unwrapInboundIn(data).readableBytesView))
    }

    func userInboundEventTriggered(context: ChannelHandlerContext, event: Any) {
        // The machine's side finished sending; what was read is still delivered.
        if case ChannelEvent.inputClosed = event { continuation.finish() }
        context.fireUserInboundEventTriggered(event)
    }

    func channelInactive(context: ChannelHandlerContext) {
        continuation.finish()
    }

    func errorCaught(context: ChannelHandlerContext, error: any Error) {
        context.close(promise: nil)
    }
}

extension DirectTCPIPError {
    /// SSH reports why a channel was not opened as a reason code and text. Code 2 is a
    /// failed connect; OpenSSH says why in the text, NIO servers leave it empty.
    init(rejection: NIOSSHError) {
        let detail = String(describing: rejection).components(separatedBy: "Reason: ").last ?? ""
        let code = detail.prefix { $0.isNumber }
        let text = detail.dropFirst(code.count).trimmingCharacters(in: .whitespaces)
        if code == "2", text.isEmpty || text.localizedCaseInsensitiveContains("refused") {
            self = .refused
        } else {
            self = .failed(text.isEmpty ? "The machine would not open the connection." : text)
        }
    }
}
