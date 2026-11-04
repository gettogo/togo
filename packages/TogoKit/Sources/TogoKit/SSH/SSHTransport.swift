import Foundation

/// A PTY running `argv` over an `SSHConnection`, the transport behind a terminal tab.
public actor SSHTransport: ByteTransport {
    public nonisolated let incoming = ByteStream()
    public nonisolated let state: AsyncStream<TransportState>

    private let stateContinuation: AsyncStream<TransportState>.Continuation
    private let connection: SSHConnection
    private let argv: [String]
    private var size: (cols: Int, rows: Int)
    private var session: SessionChannel?
    private var closeRequested = false

    public init(connection: SSHConnection, argv: [String], cols: Int, rows: Int) {
        self.connection = connection
        self.argv = argv
        size = (cols, rows)
        (state, stateContinuation) = AsyncStream<TransportState>.makeStream()
    }

    public func connect() async throws {
        stateContinuation.yield(.connecting)
        do {
            try await connection.connect()
            let session = try await connection.openPTY(shellQuote(argv), cols: size.cols, rows: size.rows)
            self.session = session
            // close() may have run while the channel was opening; nothing will read it now.
            if closeRequested {
                await session.close()
                throw SSHError.cancelled
            }
            stateContinuation.yield(.connected)
            Task { await self.pump(session) }
        } catch {
            if !closeRequested { finish(.failed(String(describing: error))) }
            throw error
        }
    }

    public func send(_ data: Data) async throws {
        guard let session else { throw SSHError.channelClosed(exitStatus: nil) }
        try await session.write(data)
    }

    public func resize(cols: Int, rows: Int) async throws {
        size = (cols, rows)
        try await session?.windowChange(cols: cols, rows: rows)
    }

    public func close() async {
        closeRequested = true
        await session?.close()
        if session == nil { finish(.requested) }
    }

    private func pump(_ session: SessionChannel) async {
        var status: Int32?
        events: for await event in session.events {
            switch event {
            case .stdout(let data), .stderr(let data):
                // Awaiting the write is the backpressure: no new read until the terminal drains.
                // A refused write means the reader is gone, so the session ends here.
                guard await incoming.write(data) else {
                    closeRequested = true
                    await session.close()
                    break events
                }
            case .exit(let code):
                status = code
            case .closed:
                break
            }
            session.requestMore()
        }
        let reason: TransportCloseReason
        if closeRequested {
            reason = .requested
        } else if status != nil {
            reason = .remoteExited(status: status)
        } else {
            reason = await connection.isConnected ? .remoteExited(status: nil) : .networkLost
        }
        self.session = nil
        finish(reason)
    }

    private func finish(_ reason: TransportCloseReason) {
        incoming.finish()
        stateContinuation.yield(.closed(reason))
        stateContinuation.finish()
    }
}
