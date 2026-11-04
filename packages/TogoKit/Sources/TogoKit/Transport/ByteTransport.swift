import Foundation

public enum TransportState: Sendable, Equatable {
    case connecting
    case connected
    case roaming
    case closed(TransportCloseReason)
}

public enum TransportCloseReason: Sendable, Equatable {
    case requested
    case remoteExited(status: Int32?)
    case networkLost
    case failed(String)
}

public protocol ByteTransport: Actor {
    func connect() async throws
    func send(_ data: Data) async throws
    /// Single consumer: TerminalLink.
    nonisolated var incoming: ByteStream { get }
    /// Single consumer: TerminalLink.
    nonisolated var state: AsyncStream<TransportState> { get }
    func resize(cols: Int, rows: Int) async throws
    func close() async
}
