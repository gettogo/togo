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

    /// The id of the record kept with a pending entry, whose channel a retry opens.
    static func machine(_ connection: Data?) -> HostID? {
        connection.flatMap { try? JSONDecoder().decode(HostRecord.self, from: $0) }?.id
    }

    static func now(_ record: HostRecord, helpers: any HelperCalling) -> (String) async throws -> Void {
        { deviceID in try await helpers.call(record, prompter: nil) { try await $0.unpair(deviceId: deviceID) } }
    }

    /// `authkeys.remove` for this device's key, over the machine's channel, which the unpair
    /// before it used.
    static func removeKey(_ record: HostRecord, helpers: any HelperCalling) -> (String) async throws -> Int {
        { publicKey in try await helpers.call(record, prompter: nil) { try await $0.removeAuthorizedKey(publicKey: publicKey) } }
    }

    /// Removes the key from the record kept with the entry. The caller closes the channel after.
    static func retryKey(helpers: any HelperCalling) -> (PendingKeyRemoval) async throws -> Int {
        { entry in
            guard let record = entry.record else { throw LiveServiceError.unknownMachine }
            return try await removeKey(record, helpers: helpers)(entry.key.publicKey)
        }
    }

    /// Unpairs from the record kept with the entry. A failed channel is closed; an open one
    /// stays for the key removal after it, and the caller closes it. A machine that is in Togo
    /// again (`skipping`) is left alone; pairing it again clears the entry.
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
        }
    }
}
