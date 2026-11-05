import Foundation
import TogoKit
import TogoPush

/// `push.unpair` over the app's helper channels, for machines that are being removed or
/// already were.
enum HelperUnpair {
    /// The host record a later retry connects with. Profiles hold addresses, users and key
    /// references; passwords and private keys stay in the Keychain.
    static func connection(_ record: HostRecord) -> Data? {
        try? JSONEncoder().encode(record)
    }

    static func now(_ record: HostRecord, helpers: any HelperCalling) -> (String) async throws -> Void {
        { deviceID in try await helpers.call(record, prompter: nil) { try await $0.unpair(deviceId: deviceID) } }
    }

    /// Unpairs from the record kept with the entry, then closes the channel. A machine that
    /// is in Togo again (`skipping`) is left alone; pairing it again clears the entry.
    static func retry(
        helpers: any HelperCalling, skipping current: Set<HostID>
    ) -> (PendingUnpair, String) async throws -> Void {
        { entry, deviceID in
            guard let data = entry.connection, let record = try? JSONDecoder().decode(HostRecord.self, from: data),
                  !current.contains(record.id)
            else { throw LiveServiceError.unknownMachine }
            do {
                try await helpers.call(record, prompter: nil) { try await $0.unpair(deviceId: deviceID) }
            } catch {
                await helpers.drop(record.id)
                throw error
            }
            await helpers.drop(record.id)
        }
    }
}
