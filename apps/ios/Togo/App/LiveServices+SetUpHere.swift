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

    /// Saves a machine just set up on this device, as an update of its synced record under the
    /// same id. Only this device remembers the key it signed in with; the record gains only the
    /// addresses and helper found here, and keeps its name, address, login and sign-in, so the
    /// other devices keep theirs. The machine's monitor, which stopped retrying when it had no
    /// key, then connects again, and once it is online its helper pairs for notifications.
    func setUpHere(_ record: HostRecord) async throws {
        if case .key(let keyID) = record.profile.auth {
            deviceMachines.setUp(record.id, keyID: keyID)
        }
        let synced = await hostStore.hosts().first { $0.id == record.id }
        try await saveHost(synced.map { Self.settingUp($0, with: record) } ?? deviceMachines.syncedVersion(of: record))
        await watches[record.id]?.refresh()
    }

    /// `synced` with what setting it up on this device found: `setUp` is the same machine,
    /// under the same id, as this device connected to it.
    static func settingUp(_ synced: HostRecord, with setUp: HostRecord) -> HostRecord {
        MachineMerge.merged(synced, absorbing: setUp)
    }
}
