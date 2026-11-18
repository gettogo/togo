import Foundation
import TogoKit

/// Keeping each machine's wake target in step with what its helper reports, while the app
/// can send wake packets.
extension LiveServices {
    /// Stores the target `snapshot` reports for `record` when it changed.
    func learnWake(from snapshot: HostSnapshot, record: HostRecord) {
        guard deps.wakeEnabled, let target = WakeLearning.updated(record, from: snapshot)?.wake else { return }
        Task { await learnWake(record.id, target) }
    }

    /// Stores a reported wake target on the machine's latest stored record, so an edit made
    /// since the report arrived is kept. Saving only changes what the machine said.
    func learnWake(_ id: HostID, _ target: WakeTarget) async {
        guard await hostStore.update(id, { $0.wake = target }) != nil else { return }
        reconcile(await hostStore.hosts())
    }
}
