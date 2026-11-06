import Foundation
import os
import TogoKit

/// Keeping each machine's wake target in step with what its helper reports, while the app
/// can send wake packets.
extension LiveServices {
    private static let wakeLog = Logger(subsystem: "app.togo", category: "wake")

    /// Stores the target `snapshot` reports for `record` when it changed.
    func learnWake(from snapshot: HostSnapshot, record: HostRecord) {
        guard deps.wakeEnabled, let target = WakeLearning.updated(record, from: snapshot)?.wake else { return }
        Task { await learnWake(record.id, target) }
    }

    /// Stores a reported wake target on the machine's latest stored record, so an edit made
    /// since the report arrived is kept. Saving only changes what the machine said.
    func learnWake(_ id: HostID, _ target: WakeTarget) async {
        guard var current = await hostStore.hosts().first(where: { $0.id == id }), current.wake != target else { return }
        current.wake = target
        do {
            try await saveHost(current)
        } catch {
            Self.wakeLog.error("Could not store the wake address for a machine: \(error.localizedDescription, privacy: .public)")
        }
    }
}
