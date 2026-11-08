import Foundation

/// A non-interactive command on the host (the helper's stdio, a mosh-server bootstrap).
public protocol ExecChannel: Sendable {
    /// Single consumer: the channel's user.
    var incoming: ByteStream { get }
    /// Sends all of `data` before returning, with no other bytes in between, so a frame
    /// reaches the host whole. Callers never overlap calls: HelperClient writes from a
    /// single task, so implementations need no queue of their own.
    func write(_ data: Data) async throws
    func close() async
    /// The command's exit status once it ended, if the host reported one.
    var exitStatus: Int32? { get }
}

extension ExecChannel {
    public var exitStatus: Int32? { nil }
}
