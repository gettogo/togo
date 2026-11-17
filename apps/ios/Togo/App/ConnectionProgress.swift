import Foundation
import Observation
import SwiftUI
import Synchronization
import TogoKit

/// Coalesced UI snapshots; the producer always removes ended attempts before notifying.
/// Dropping an intermediate notification cannot strand a stale route on screen.
@MainActor @Observable final class ConnectionProgress {
    static let shared = ConnectionProgress()
    @ObservationIgnored nonisolated let report: @Sendable (ConnectionAttemptEvent) -> Void
    @ObservationIgnored private let changes: AsyncStream<Void>.Continuation
    @ObservationIgnored private var consumer: Task<Void, Never>?
    private var active: [ConnectionAttemptEvent] = []
    private let clockOrigin = ContinuousClock.now
    private let dateOrigin = Date()

    struct Summary: Equatable {
        let started: ContinuousClock.Instant
        let kinds: [HostRoute.Kind]
        let preparing: Bool
        let origin: Date

        var label: String {
            if !preparing && kinds.contains(.tailscale) { return String(localized: "connection.tailnet", defaultValue: "Connecting over Tailscale…") }
            if !preparing && kinds.contains(where: { $0 == .lan || $0 == .local }) {
                return String(localized: "connection.local", defaultValue: "Connecting over local network…")
            }
            return preparing ? String(localized: "connection.preparing", defaultValue: "Preparing SSH connection…")
                : String(localized: "connection.ssh", defaultValue: "Connecting over SSH…")
        }

        func seconds(at now: ContinuousClock.Instant = .now) -> TimeInterval {
            let duration = started.duration(to: now).components
            return max(0, Double(duration.seconds) + Double(duration.attoseconds) / 1e18)
        }
    }

    init(diagnostics: DiagnosticsRecorder = .shared) {
        let state = Mutex<[UUID: ConnectionAttemptEvent]>([:])
        let stream = AsyncStream<Void>.makeStream(bufferingPolicy: .bufferingNewest(1))
        changes = stream.continuation
        report = { event in
            state.withLock { active in
                if event.outcome == .started { active[event.id] = event }
                else { active[event.id] = nil }
            }
            diagnostics.connectionAttempt(event)
            stream.continuation.yield(())
        }
        consumer = Task { @MainActor [weak self] in
            for await _ in stream.stream { self?.active = state.withLock { Array($0.values) } }
        }
    }

    deinit { consumer?.cancel(); changes.finish() }

    func summary(for host: UUID, purpose: ConnectionPurpose? = nil) -> Summary? {
        let attempts = active.filter { $0.hostID == host && (purpose == nil || purpose == $0.purpose) }
        let routes = attempts.filter { if case .route = $0.stage { true } else { false } }
        let shown = routes.isEmpty ? attempts : routes
        guard let started = shown.map(\.started).min() else { return nil }
        let kinds = routes.compactMap { event -> HostRoute.Kind? in
            if case .route(let kind) = event.stage { return kind }; return nil
        }.sorted { $0.rawValue < $1.rawValue }
        let elapsed = clockOrigin.duration(to: started).components
        let origin = dateOrigin.addingTimeInterval(Double(elapsed.seconds) + Double(elapsed.attoseconds) / 1e18)
        return Summary(started: started, kinds: kinds, preparing: routes.isEmpty, origin: origin)
    }
}

/// Timed status, never an announcement or an animated counter.
struct ConnectionProgressLabel: View {
    @Environment(\.loopsPaused) private var paused
    let summary: ConnectionProgress.Summary
    var body: some View {
        TimelineView(.clock(from: summary.origin, paused: paused)) { _ in
            Text("\(summary.label) · \(ChatCopy.clock(summary.seconds()))")
                .monospacedDigit()
                .fixedSize(horizontal: false, vertical: true)
                .contentTransition(.identity)
                .transaction { $0.animation = nil }
        }
    }
}

extension DiagnosticsRecorder {
    func connectionAttempt(_ event: ConnectionAttemptEvent) {
        let stage: String
        switch event.stage {
        case .credentials: stage = "credentials"
        case .route(let kind): stage = kind.rawValue
        }
        let phase = event.outcome == .succeeded ? (event.stage == .credentials ? "prepared" : "connected") : event.outcome.rawValue
        // The ID is generated for this attempt, not read from a host or credential. Keep
        // its numeric correlation separate from strings that the privacy filter redacts.
        let uuid = event.id.uuid
        let correlation = withUnsafeBytes(of: uuid) { bytes in
            bytes.prefix(8).reduce(UInt64(0)) { ($0 << 8) | UInt64($1) }
        }
        record(.link, "connection_attempt", ["machine": .machine(log.token(for: event.hostID)),
            "attempt": .int(Int(truncatingIfNeeded: correlation & UInt64(Int.max))), "route": .word(stage),
            "phase": .word(phase), "purpose": .word(event.purpose.rawValue), "ms": .int(event.elapsedMilliseconds)])
    }
}
