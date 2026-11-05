import Foundation
import Network
import Synchronization

public protocol NetworkPathSource: Sendable {
    /// Each access returns a new stream with a single consumer (a TerminalLink or a HostMonitor).
    var changes: AsyncStream<Void> { get }
}

/// Yields when the set of usable interfaces changes, such as Wi-Fi to cellular.
/// The first path update describes the current network and is not a change.
public struct SystemPathSource: NetworkPathSource {
    public init() {}

    public var changes: AsyncStream<Void> {
        AsyncStream { continuation in
            let monitor = NWPathMonitor()
            let last = PathSignature()
            monitor.pathUpdateHandler = { path in
                let signature = "\(path.status) " + path.availableInterfaces.map(\.name).joined(separator: ",")
                if last.replace(with: signature) { continuation.yield() }
            }
            continuation.onTermination = { _ in monitor.cancel() }
            monitor.start(queue: DispatchQueue(label: "app.togo.path"))
        }
    }
}

private final class PathSignature: Sendable {
    private let value = Mutex<String?>(nil)

    /// Returns true when a previous signature existed and differs.
    func replace(with signature: String) -> Bool {
        value.withLock { previous in
            defer { previous = signature }
            return previous != nil && previous != signature
        }
    }
}
