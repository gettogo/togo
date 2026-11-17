import Foundation

public enum ConnectionPurpose: String, Sendable { case requests, monitor, terminal }

/// Route timing without addresses, credentials, commands or library error text.
public struct ConnectionAttemptEvent: Sendable {
    public enum Stage: Sendable, Equatable { case credentials, route(HostRoute.Kind) }
    public enum Outcome: String, Sendable { case started, succeeded, failed, cancelled }
    public let id: UUID
    public let hostID: UUID
    public let stage: Stage
    public let purpose: ConnectionPurpose
    public let outcome: Outcome
    public let started: ContinuousClock.Instant
    public let elapsedMilliseconds: Int

    public init(id: UUID, hostID: UUID, stage: Stage, outcome: Outcome,
                started: ContinuousClock.Instant, elapsedMilliseconds: Int = 0, purpose: ConnectionPurpose = .requests) {
        self.id = id; self.hostID = hostID; self.stage = stage; self.outcome = outcome
        self.started = started; self.purpose = purpose; self.elapsedMilliseconds = max(0, elapsedMilliseconds)
    }
}

struct ConnectionAttemptSpan: Sendable {
    let id = UUID()
    let hostID: UUID
    let stage: ConnectionAttemptEvent.Stage
    let purpose: ConnectionPurpose
    let started = ContinuousClock.now
    let report: @Sendable (ConnectionAttemptEvent) -> Void

    init(hostID: UUID, stage: ConnectionAttemptEvent.Stage, purpose: ConnectionPurpose = .requests, report: @escaping @Sendable (ConnectionAttemptEvent) -> Void) {
        self.hostID = hostID; self.stage = stage; self.purpose = purpose; self.report = report
        report(.init(id: id, hostID: hostID, stage: stage, outcome: .started, started: started, purpose: purpose))
    }

    func end(_ outcome: ConnectionAttemptEvent.Outcome) {
        let duration = started.duration(to: .now).components
        let milliseconds = Double(duration.seconds) * 1000 + Double(duration.attoseconds) / 1e15
        report(.init(id: id, hostID: hostID, stage: stage, outcome: outcome, started: started,
                     elapsedMilliseconds: Int(min(Double(Int.max - 1), max(0, milliseconds))), purpose: purpose))
    }

    static func failure(_ error: any Error) -> ConnectionAttemptEvent.Outcome {
        if Task.isCancelled || error is CancellationError { return .cancelled }
        if case SSHError.cancelled = error { return .cancelled }
        return .failed
    }
}

/// Cold attempts precede a successful connection. Normal reconnects retain their backoff.
public enum ConnectionTiming {
    public static let retryPause: Duration = .seconds(2)
    public static let ordinaryTimeout: Duration = .seconds(8)
    public static let initialTailnetTimeout: Duration = .seconds(20)

    static func timeout(kind: HostRoute.Kind, succeeded: Bool, direct: Bool, override: Duration?) -> Duration {
        if let override { return override }
        return kind == .tailscale && direct && !succeeded ? initialTailnetTimeout : ordinaryTimeout
    }
}
