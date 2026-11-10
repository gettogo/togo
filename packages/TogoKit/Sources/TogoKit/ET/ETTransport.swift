import ETSession
import Foundation

/// Where an etserver session lives, and the credentials etterminal handed out for it.
public struct ETEndpoint: Sendable, Equatable, CustomStringConvertible, CustomDebugStringConvertible {
    public var host: String
    public var port: UInt16
    public var clientID: String
    public var passkey: Data

    public init(host: String, port: UInt16 = 2022, clientID: String, passkey: Data) {
        self.host = host
        self.port = port
        self.clientID = clientID
        self.passkey = passkey
    }

    public var description: String { "ETEndpoint(\(host):\(port), passkey: <redacted>)" }
    public var debugDescription: String { description }
}

/// An Eternal Terminal session carrying one terminal. After connecting it types `argv`
/// into the remote shell with `exec`, so the session ends when that program does.
public actor ETTransport: ByteTransport, CustomStringConvertible, CustomDebugStringConvertible {
    public nonisolated let incoming = ByteStream()
    public nonisolated let state: AsyncStream<TransportState>
    public nonisolated let description: String
    public nonisolated var debugDescription: String { description }

    private let session: ETTerminalSession
    private let stateContinuation: AsyncStream<TransportState>.Continuation
    private let startInput: Data?
    private let stopServer: (@Sendable () async -> Void)?
    private var cols: Int
    private var rows: Int
    private var pump: Task<Void, Never>?
    private var watcher: Task<Void, Never>?
    private var closed = false

    public init(session: ETTerminalSession, cols: Int, rows: Int) {
        self.init(session: session, startInput: nil, label: "ETTransport", cols: cols, rows: rows, stopServer: nil)
    }

    /// `stopServer` runs when `connect` fails, to stop the etterminal this start launched.
    public init(
        endpoint: ETEndpoint, argv: [String], cols: Int, rows: Int, stopServer: (@Sendable () async -> Void)? = nil
    ) throws {
        let session = try ETTerminalSession(
            host: endpoint.host, port: endpoint.port, clientID: endpoint.clientID, passkey: endpoint.passkey
        )
        self.init(
            session: session, startInput: ETFactory.attachInput(argv),
            label: "ETTransport(\(endpoint))", cols: cols, rows: rows, stopServer: stopServer
        )
    }

    private init(
        session: ETTerminalSession, startInput: Data?, label: String, cols: Int, rows: Int,
        stopServer: (@Sendable () async -> Void)?
    ) {
        self.session = session
        self.startInput = startInput
        self.stopServer = stopServer
        self.description = label
        self.cols = cols
        self.rows = rows
        (state, stateContinuation) = AsyncStream.makeStream(bufferingPolicy: .bufferingNewest(16))
    }

    public func connect() async throws {
        stateContinuation.yield(.connecting)
        startWatching()
        do {
            try await session.connect()
            try await session.resize(rows: rows, cols: cols)
            if let startInput { try await session.send(startInput) }
        } catch {
            await finish(.failed(String(describing: error)))
            await stopServer?()
            throw error
        }
        let output = session.output
        let incoming = incoming
        pump = Task {
            for await chunk in output {
                guard await incoming.write(chunk) else { break }
            }
            await self.finish(.remoteExited(status: nil))
        }
    }

    public func send(_ data: Data) async throws {
        try await session.send(data)
    }

    public func resize(cols: Int, rows: Int) async throws {
        self.cols = cols
        self.rows = rows
        try await session.resize(rows: rows, cols: cols)
    }

    public func close() async {
        await finish(.requested)
    }

    private func startWatching() {
        guard watcher == nil else { return }
        let changes = session.stateChanges
        watcher = Task { [weak self] in
            for await change in changes {
                guard let self else { return }
                await self.apply(change)
            }
        }
    }

    private func apply(_ change: ETConnectionState) async {
        switch change {
        case .connected: stateContinuation.yield(.connected)
        case .disconnected, .reconnecting: stateContinuation.yield(.roaming)
        case .sessionEnded: await finish(.remoteExited(status: nil))
        case .failed(let error): await finish(.failed(String(describing: error)))
        case .idle, .bootstrapping, .connecting, .closed: break
        }
    }

    private func finish(_ reason: TransportCloseReason) async {
        guard !closed else { return }
        closed = true
        pump?.cancel()
        watcher?.cancel()
        incoming.finish()
        stateContinuation.yield(.closed(reason))
        stateContinuation.finish()
        await session.close()
    }
}
