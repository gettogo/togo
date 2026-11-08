import Foundation
import Network
import Synchronization

public protocol NetworkPathSource: Sendable {
    /// Each access returns a new stream with a single consumer (a TerminalLink or a HostMonitor).
    var changes: AsyncStream<Void> { get }
    /// True while this device has no usable network at all, so a machine that does not answer
    /// is not to blame. False when unknown.
    var isOffline: Bool { get }
}

extension NetworkPathSource {
    public var isOffline: Bool { false }
}

/// Yields when the network a connection would use changes, such as Wi-Fi to cellular, once
/// it has settled for a second. The first path update describes the current network and is
/// not a change. Only the status and the preferred physical interface count: cellular
/// coverage coming and going under Wi-Fi, or a VPN tunnel flapping, would otherwise tear
/// down every connection; a link that really died is found by its keepalives.
public struct SystemPathSource: NetworkPathSource {
    static let settle: Duration = .seconds(1)

    /// Starts the shared status monitor, so the first connect already knows the network.
    public init() { _ = PathStatus.shared }

    public var isOffline: Bool { PathStatus.shared.isOffline }

    public var changes: AsyncStream<Void> {
        AsyncStream { continuation in
            let monitor = NWPathMonitor()
            let queue = DispatchQueue(label: "app.togo.path")
            let settle = PathSettle()
            let delay = Double(Self.settle.components.seconds)
            monitor.pathUpdateHandler = { path in
                let signature = PathSettle.signature(status: path.status, interfaces: path.availableInterfaces.map(\.type))
                guard settle.update(signature) else { return }
                queue.asyncAfter(deadline: .now() + delay) {
                    if settle.settle() { continuation.yield() }
                }
            }
            continuation.onTermination = { _ in monitor.cancel() }
            monitor.start(queue: queue)
        }
    }
}

/// The settled network signature. `update` takes every path update and says when a settle
/// check should be scheduled; `settle` says whether the network ended up somewhere new.
final class PathSettle: Sendable {
    private struct State {
        var yielded: String?
        var latest: String?
        var scheduled = false
    }

    private let state = Mutex(State())

    /// Status plus the first physical interface in the system's order of preference.
    static func signature(status: NWPath.Status, interfaces: [NWInterface.InterfaceType]) -> String {
        let primary = interfaces.first { $0 != .other && $0 != .loopback }
        return "\(status) \(primary.map { "\($0)" } ?? "none")"
    }

    /// True when a settle check should be scheduled now.
    func update(_ signature: String) -> Bool {
        state.withLock { state in
            state.latest = signature
            guard let yielded = state.yielded else {
                state.yielded = signature
                return false
            }
            guard !state.scheduled, signature != yielded else { return false }
            state.scheduled = true
            return true
        }
    }

    /// True when the network settled on a signature other than the last one reported.
    func settle() -> Bool {
        state.withLock { state in
            state.scheduled = false
            guard let latest = state.latest, latest != state.yielded else { return false }
            state.yielded = latest
            return true
        }
    }
}

/// The device's current network status, from one process-wide monitor started on first use.
final class PathStatus: Sendable {
    static let shared = PathStatus()

    private let status = Mutex<NWPath.Status?>(nil)
    private let monitor = NWPathMonitor()

    private init() {
        monitor.pathUpdateHandler = { [weak self] path in self?.status.withLock { $0 = path.status } }
        monitor.start(queue: DispatchQueue(label: "app.togo.pathstatus"))
    }

    var isOffline: Bool { status.withLock { $0 } == .unsatisfied }
}
