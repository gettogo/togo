import Foundation
import TogoKit

/// Reconnecting, and setting up on this device a machine that came from another one.
extension LiveServices {
    func refresh() async {
        await withTaskGroup(of: Void.self) { group in
            for watch in watches.values {
                group.addTask { await watch.refresh() }
            }
        }
    }

    /// Saves a machine just set up on this device. Only this device remembers the key it signed
    /// in with; the synced record keeps its own sign-in, so the other devices keep theirs. The
    /// machine's monitor, which stopped retrying when it had no key, then connects again, and
    /// once it is online its helper pairs for notifications.
    func setUpHere(_ record: HostRecord) async throws {
        if case .key(let keyID) = record.profile.auth {
            deviceMachines.setUp(record.id, keyID: keyID)
        }
        try await saveHost(deviceMachines.syncedVersion(of: record))
        await watches[record.id]?.refresh()
    }
}
