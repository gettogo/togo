import Foundation
import Network
import Synchronization

/// Opens a TCP connection from the machine to one of its own ports, carried over SSH.
public protocol TCPChannelOpener: Sendable {
    func openDirectTCPIP(host: String, port: Int) async throws -> any ExecChannel
    /// Returns once the connection that carries the channels is gone, at once if it is
    /// already gone, or when the calling task is cancelled.
    func waitUntilClosed() async
}

/// Why the machine would not open a forwarded connection.
public enum DirectTCPIPError: Error, Equatable, Sendable {
    /// Nothing accepted the connection on the target port.
    case refused
    case failed(String)
}

public enum ForwardEvent: Sendable, Equatable {
    case upstreamRefused
    case upstreamFailed(String)
    case stopped
}

/// Serves one port of the machine on a loopback port of this device.
///
/// The listener binds 127.0.0.1, so nothing else on the network can reach the machine
/// through it. Each accepted connection gets its own direct-tcpip channel to the target;
/// the two are piped both ways until either closes.
/// Every hop waits for the previous write to finish before reading more, so a slow
/// reader on either side throttles the other instead of growing a buffer.
///
/// When either side ends its stream the whole connection closes; a client that half-closes
/// and still waits for an answer loses it. Browsers and URLSession never do that.
///
/// The listener closes as soon as the SSH connection is gone, or on `stop()`.
public actor LocalForward {
    static let connectionLimit = 32
    static let readSize = 64 << 10
    /// Server-supplied failure text is shown to people; keep it to a line or two.
    static let detailLimit = 200

    private struct Link {
        let connection: NWConnection
        var channel: (any ExecChannel)?
        var task: Task<Void, Never>?
    }

    public nonisolated let events: AsyncStream<ForwardEvent>
    private let sink: AsyncStream<ForwardEvent>.Continuation
    private let opener: any TCPChannelOpener
    private let target: ForwardTarget
    private let preferredLocalPort: Int?
    /// The preferred port or nothing: a sign-in callback must land on the very port its
    /// redirect names, so a taken port fails `start()` instead of moving elsewhere.
    private let exactLocalPort: Bool
    /// Called on each connection accepted, before its channel opens.
    private let onAccept: (@Sendable () -> Void)?
    private let queue = DispatchQueue(label: "app.togo.forward")

    private var listener: NWListener?
    /// Every listener ever bound, so `stop()` and `deinit` can never miss one.
    private var listeners: [NWListener] = []
    private var starting: Task<Int, any Error>?
    /// Opens once the bound listener has released its port.
    private var released: ReadyGate?
    private var links: [UInt64: Link] = [:]
    private var nextID: UInt64 = 0
    private var stopped = false
    /// Ends the forward when the SSH connection goes; cancelled by `stop()`.
    private var watch: Task<Void, Never>?
    /// The address the listener is bound to, once started.
    private(set) var boundHost: String?

    public init(
        opener: any TCPChannelOpener, target: ForwardTarget, preferredLocalPort: Int?,
        exactLocalPort: Bool = false, onAccept: (@Sendable () -> Void)? = nil
    ) {
        self.opener = opener
        self.target = target
        self.preferredLocalPort = preferredLocalPort
        self.exactLocalPort = exactLocalPort && preferredLocalPort != nil
        self.onAccept = onAccept
        (events, sink) = AsyncStream.makeStream(bufferingPolicy: .bufferingNewest(16))
    }

    deinit {
        for listener in listeners { listener.cancel() }
    }

    /// Starts listening and returns the local port. The preferred port is used when it is
    /// free, so a reopened forward keeps the address a preview already shows.
    /// Concurrent calls share one listener and get the same port.
    public func start() async throws -> Int {
        if stopped { throw SSHError.channelClosed(exitStatus: nil) }
        if let listener, let port = listener.port { return Int(port.rawValue) }
        if let starting { return try await starting.value }
        let task = Task { try await self.bind() }
        starting = task
        defer { starting = nil }
        let port = try await task.value
        watchConnection()
        return port
    }

    private func bind() async throws -> Int {
        if exactLocalPort, let preferredLocalPort {
            guard preferredLocalPort > 0, preferredLocalPort <= 65_535,
                  let port = NWEndpoint.Port(rawValue: UInt16(preferredLocalPort)) else {
                throw NWError.posix(.EINVAL)
            }
            return try await listen(on: port)
        }
        if let preferredLocalPort, let port = NWEndpoint.Port(rawValue: UInt16(clamping: preferredLocalPort)),
           preferredLocalPort > 0, let bound = try? await listen(on: port) {
            return bound
        }
        return try await listen(on: .any)
    }

    /// Closes the listener and every piped connection, then ends `events`.
    public func stop() async {
        guard !stopped else { return }
        stopped = true
        watch?.cancel()
        watch = nil
        for listener in listeners { listener.cancel() }
        listener = nil
        // Cancelling is asynchronous; the port must refuse connections once this returns.
        try? await released?.wait()
        let current = links.values
        for link in current {
            link.connection.cancel()
            await link.channel?.close()
        }
        // A connection still waiting for its channel closes it when the open returns;
        // waiting for it here would hold `stop()` as long as the server takes to answer.
        for link in current where link.channel != nil {
            await link.task?.value
        }
        sink.yield(.stopped)
        sink.finish()
    }

    // MARK: Listening

    private func listen(on port: NWEndpoint.Port) async throws -> Int {
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: .ipv4(.loopback), port: port)
        // Binding loopback is what keeps the network out. `acceptLocalOnly` is not set: on iOS
        // a listener with it never receives connections made to 127.0.0.1.
        parameters.allowLocalEndpointReuse = false
        let listener = try NWListener(using: parameters)
        listeners.append(listener)
        let ready = ReadyGate()
        let released = ReadyGate()
        listener.stateUpdateHandler = { state in
            switch state {
            case .ready: ready.finish(.success(()))
            case .failed(let error), .waiting(let error): ready.finish(.failure(error))
            case .cancelled:
                ready.finish(.failure(NWError.posix(.ECANCELED)))
                released.finish(.success(()))
            default: break
            }
        }
        listener.newConnectionHandler = { [weak self] connection in
            guard let self else { return connection.cancel() }
            Task { await self.accept(connection) }
        }
        listener.start(queue: queue)
        do {
            try await ready.wait()
        } catch {
            listener.cancel()
            throw error
        }
        guard !stopped, let bound = listener.port else {
            listener.cancel()
            throw SSHError.channelClosed(exitStatus: nil)
        }
        self.listener = listener
        self.released = released
        if case .hostPort(let host, _) = listener.parameters.requiredLocalEndpoint { boundHost = "\(host)" }
        return Int(bound.rawValue)
    }

    private func accept(_ connection: NWConnection) {
        guard !stopped, links.count < Self.connectionLimit else {
            connection.cancel()
            return
        }
        let id = nextID
        nextID += 1
        links[id] = Link(connection: connection)
        onAccept?()
        connection.start(queue: queue)
        links[id]?.task = Task { await self.serve(id, connection) }
    }

    private func serve(_ id: UInt64, _ connection: NWConnection) async {
        defer { links[id] = nil }
        let channel: any ExecChannel
        do {
            channel = try await opener.openDirectTCPIP(host: target.host, port: target.port)
        } catch {
            connection.cancel()
            report(error)
            return
        }
        guard !stopped, links[id] != nil else {
            connection.cancel()
            await channel.close()
            return
        }
        links[id]?.channel = channel
        await Self.pipe(connection, channel)
    }

    private func report(_ error: any Error) {
        guard !stopped else { return }
        switch error {
        case DirectTCPIPError.refused:
            sink.yield(.upstreamRefused)
        case DirectTCPIPError.failed(let detail):
            sink.yield(.upstreamFailed(String(detail.prefix(Self.detailLimit))))
        case SSHError.channelClosed:
            // The SSH connection is gone, so no later connection can be served either.
            connectionLost()
        default:
            sink.yield(.upstreamFailed(String(error.localizedDescription.prefix(Self.detailLimit))))
        }
    }

    /// Closes the forward when the SSH connection drops, without waiting for a connect.
    private func watchConnection() {
        guard watch == nil, !stopped else { return }
        watch = Task { [weak self, opener] in
            await opener.waitUntilClosed()
            await self?.connectionLost()
        }
    }

    private func connectionLost() {
        guard !stopped else { return }
        sink.yield(.upstreamFailed(SSHError.channelClosed(exitStatus: nil).localizedDescription))
        // Stopping waits for connection tasks, which may include the caller's, so it runs on its own.
        Task { await self.stop() }
    }

    // MARK: Piping

    private static func pipe(_ connection: NWConnection, _ channel: any ExecChannel) async {
        await withTaskGroup(of: Void.self) { group in
            group.addTask { await upload(from: connection, to: channel) }
            group.addTask { await download(from: channel, to: connection) }
            await group.next()
            connection.cancel()
            await channel.close()
            await group.waitForAll()
        }
    }

    private static func upload(from connection: NWConnection, to channel: any ExecChannel) async {
        while true {
            let (data, ended) = await receive(connection)
            if let data, !data.isEmpty {
                do {
                    try await channel.write(data)
                } catch {
                    return
                }
            }
            if ended { return }
        }
    }

    private static func download(from channel: any ExecChannel, to connection: NWConnection) async {
        for await chunk in channel.incoming {
            guard await send(chunk, isComplete: false, on: connection) else { return }
        }
        _ = await send(nil, isComplete: true, on: connection)
    }

    private static func receive(_ connection: NWConnection) async -> (Data?, ended: Bool) {
        await withCheckedContinuation { continuation in
            connection.receive(minimumIncompleteLength: 1, maximumLength: readSize) { data, _, isComplete, error in
                continuation.resume(returning: (data, isComplete || error != nil))
            }
        }
    }

    private static func send(_ data: Data?, isComplete: Bool, on connection: NWConnection) async -> Bool {
        await withCheckedContinuation { continuation in
            connection.send(
                content: data, contentContext: isComplete ? .finalMessage : .defaultMessage, isComplete: isComplete,
                completion: .contentProcessed { error in continuation.resume(returning: error == nil) }
            )
        }
    }
}

/// Resumes a listener's start exactly once, with its first decisive state.
private final class ReadyGate: Sendable {
    private let state = Mutex<(continuation: CheckedContinuation<Void, any Error>?, outcome: Result<Void, any Error>?)>((nil, nil))

    func wait() async throws {
        try await withCheckedThrowingContinuation { continuation in
            let early = state.withLock { state -> Result<Void, any Error>? in
                if let outcome = state.outcome { return outcome }
                state.continuation = continuation
                return nil
            }
            if let early { continuation.resume(with: early) }
        }
    }

    func finish(_ outcome: Result<Void, any Error>) {
        let continuation = state.withLock { state -> CheckedContinuation<Void, any Error>? in
            guard state.outcome == nil else { return nil }
            state.outcome = outcome
            return state.continuation.take()
        }
        continuation?.resume(with: outcome)
    }
}
