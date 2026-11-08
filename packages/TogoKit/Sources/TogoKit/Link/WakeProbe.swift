import Foundation
import Network
import Synchronization

/// Waits for a host that may be asleep. Tailscale and Wake-on-LAN machines take a few
/// seconds to answer, so a connect starts only once the SSH port accepts a TCP handshake.
public enum WakeProbe {
    /// Delays before each attempt; they add up to 30 s.
    static let delays: [Duration] = [.zero, .seconds(1), .seconds(2), .seconds(4), .seconds(8), .seconds(15)]

    public static func reach(
        host: String, port: Int, clock: any LinkClock, onWaking: @Sendable (String) -> Void,
        attempt: @Sendable () async -> Bool
    ) async throws {
        for (index, delay) in delays.enumerated() {
            if index == 1 { onWaking(host) }
            if delay > .zero { try await clock.sleep(for: delay) }
            if await attempt() { return }
        }
        throw SSHError.unreachable(String(localized: "\(host) did not answer for 30 s; it may be asleep", bundle: .module))
    }

    /// True as soon as any of `hosts` accepts a TCP handshake on `port`; the other probes stop.
    public static func anyReachable(hosts: [String], port: Int, timeout: Duration) async -> Bool {
        await withTaskGroup(of: Bool.self) { group in
            for host in hosts {
                group.addTask { await tcpConnect(host: host, port: port, timeout: timeout) }
            }
            for await reached in group where reached {
                group.cancelAll()
                return true
            }
            return false
        }
    }

    public static func tcpConnect(host: String, port: Int, timeout: Duration) async -> Bool {
        guard let port = NWEndpoint.Port(rawValue: UInt16(clamping: port)) else { return false }
        let probe = TCPProbe(connection: NWConnection(host: NWEndpoint.Host(host), port: port, using: .tcp))
        return await probe.run(timeout: timeout)
    }
}

/// Owns one NWConnection for a single attempt. NWConnection reports on its own queue,
/// so the first outcome (ready, failure or timeout) wins and the rest are ignored.
private final class TCPProbe: Sendable {
    private let connection: NWConnection
    private let continuation = Mutex<CheckedContinuation<Bool, Never>?>(nil)
    private let queue = DispatchQueue(label: "app.togo.wakeprobe")

    init(connection: NWConnection) {
        self.connection = connection
    }

    func run(timeout: Duration) async -> Bool {
        await withTaskCancellationHandler {
            await start(timeout: timeout)
        } onCancel: {
            finish(false)
        }
    }

    private func start(timeout: Duration) async -> Bool {
        await withCheckedContinuation { continuation in
            self.continuation.withLock { $0 = continuation }
            if Task.isCancelled {
                finish(false)
                return
            }
            connection.stateUpdateHandler = { [self] state in
                switch state {
                case .ready: finish(true)
                // A refused port still means the machine is awake; the connect that follows
                // reports why it turned SSH away.
                case .failed(.posix(.ECONNREFUSED)), .waiting(.posix(.ECONNREFUSED)): finish(true)
                case .failed, .waiting, .cancelled: finish(false)
                default: break
                }
            }
            connection.start(queue: queue)
            let seconds = Double(timeout.components.seconds) + Double(timeout.components.attoseconds) / 1e18
            queue.asyncAfter(deadline: .now() + seconds) { [self] in finish(false) }
        }
    }

    private func finish(_ reached: Bool) {
        guard let continuation = continuation.withLock({ $0.take() }) else { return }
        connection.stateUpdateHandler = nil
        connection.cancel()
        continuation.resume(returning: reached)
    }
}
