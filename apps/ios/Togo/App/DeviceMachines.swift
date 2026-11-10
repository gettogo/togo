import Foundation
import TogoKit

/// What this device alone knows about each machine: whether it has signed in to it, and the key
/// it signs in with when that is not the key the machine's synced record names.
///
/// Machines sync through iCloud, but SSH keys never leave the device that made them. A machine
/// added on another device is therefore not set up here until this device's key is on it, which
/// is a different thing to show than a sign-in that stopped working. Nothing here syncs.
@MainActor final class DeviceMachines {
    private static let signedInKey = "machines.signedIn"
    private static let keysKey = "machines.keys"

    private let backend: any KeyValueBackend
    private let hasKey: (String) -> Bool
    private let deviceKeyID: () -> String?
    /// The sign-in each machine's synced record names, before `resolve` swaps in this device's key.
    private var synced: [HostID: AuthMethodRef] = [:]
    private var signedInIDs: Set<HostID>
    /// The key chosen on this device for a machine, when it set the machine up itself.
    private var keys: [HostID: String]

    /// `hasKey` says whether a key id is in this device's keychain; `deviceKeyID` is the key Togo
    /// made on this device, nil until it made one.
    init(backend: any KeyValueBackend, hasKey: @escaping (String) -> Bool, deviceKeyID: @escaping () -> String?) {
        self.backend = backend
        self.hasKey = hasKey
        self.deviceKeyID = deviceKeyID
        signedInIDs = backend.data(forKey: Self.signedInKey)
            .flatMap { try? JSONDecoder().decode(Set<HostID>.self, from: $0) } ?? []
        keys = backend.data(forKey: Self.keysKey)
            .flatMap { try? JSONDecoder().decode([String: String].self, from: $0) }
            .map { Dictionary(uniqueKeysWithValues: $0.compactMap { key, value in HostID(uuidString: key).map { ($0, value) } }) } ?? [:]
    }

    static func inMemory() -> DeviceMachines {
        DeviceMachines(backend: MemoryBackend(), hasKey: { _ in false }, deviceKeyID: { nil })
    }

    // MARK: Records

    /// The records as this device connects with them. A machine whose record names a key that is
    /// not here signs in with the key chosen here, or else this device's own key.
    func resolve(_ records: [HostRecord]) -> [HostRecord] {
        synced = Dictionary(records.map { ($0.id, $0.profile.auth) }, uniquingKeysWith: { _, last in last })
        return records.map { record in
            var record = record
            if let key = Self.key(for: record.profile.auth, chosen: keys[record.id], hasKey: hasKey, deviceKeyID: deviceKeyID()) {
                record.profile.auth = .key(id: key)
            }
            return record
        }
    }

    /// The key to sign in with instead of the record's, or nil to keep the record's.
    static func key(
        for auth: AuthMethodRef, chosen: String?, hasKey: (String) -> Bool, deviceKeyID: String?
    ) -> String? {
        guard case .key(let named) = auth else { return nil }
        if let chosen, hasKey(chosen) { return chosen }
        if hasKey(named) { return nil }
        return deviceKeyID.flatMap { hasKey($0) ? $0 : nil }
    }

    /// `record` with the sign-in its synced record names, so saving it never sends this
    /// device's key to the other devices.
    func syncedVersion(of record: HostRecord) -> HostRecord {
        var record = record
        if let auth = synced[record.id] { record.profile.auth = auth }
        return record
    }

    /// An edit of a machine, made from the record as `resolve` showed it. A sign-in left as it
    /// was shown goes back to the one the synced record names, so editing the name or address
    /// here never hands this device's key to the other devices, which have no such key. A
    /// sign-in the user changed is saved as chosen.
    func editedVersion(of record: HostRecord) -> HostRecord {
        guard let auth = synced[record.id] else { return record }
        let shown = Self.key(for: auth, chosen: keys[record.id], hasKey: hasKey, deviceKeyID: deviceKeyID())
            .map { AuthMethodRef.key(id: $0) } ?? auth
        guard record.profile.auth == shown else { return record }
        var record = record
        record.profile.auth = auth
        return record
    }

    // MARK: Set up here

    /// Whether `reach` means the machine is waiting for this device's key rather than for a
    /// sign-in: it signs in with a key, and this device never signed in to it or chose a key for it.
    func needsSetUp(_ id: HostID, reach: HostSnapshot.Reach) -> Bool {
        Self.needsSetUp(
            reach: reach, auth: synced[id], signedInHere: signedInIDs.contains(id) || keys[id] != nil,
            deviceKeyID: deviceKeyID()
        )
    }

    static func needsSetUp(
        reach: HostSnapshot.Reach, auth: AuthMethodRef?, signedInHere: Bool, deviceKeyID: String?
    ) -> Bool {
        guard !signedInHere, case .key(let named)? = auth, named != deviceKeyID else { return false }
        switch reach {
        case .keyMissing, .authNeeded: return true
        case .connecting, .waking, .online, .offline, .failed, .hostKeyChanged, .deviceOffline: return false
        }
    }

    /// The machine took this device's sign-in.
    func signedIn(to id: HostID) {
        guard signedInIDs.insert(id).inserted else { return }
        saveSignedIn()
    }

    /// The machine was set up here with `keyID`, which it now accepts.
    func setUp(_ id: HostID, keyID: String) {
        keys[id] = keyID
        saveKeys()
        signedIn(to: id)
    }

    /// `dropped` was folded into `kept`, a record for the same machine. What this device knew
    /// about `dropped` moves over unless `kept` has its own: the key chosen here, and that
    /// this device signed in.
    func merged(_ dropped: HostID, into kept: HostID) {
        if let key = keys.removeValue(forKey: dropped) {
            if keys[kept] == nil { keys[kept] = key }
            saveKeys()
        }
        if signedInIDs.remove(dropped) != nil {
            signedInIDs.insert(kept)
            saveSignedIn()
        }
    }

    /// `dropped`, folded into `kept`, signed in with a key on this device: this device goes on
    /// signing in to the machine with it, unless it chose a key for `kept` already.
    func keepKey(of dropped: HostRecord, for kept: HostID) {
        guard keys[kept] == nil, case .key(let id) = dropped.profile.auth, hasKey(id) else { return }
        keys[kept] = id
        saveKeys()
    }

    /// Whether the record's synced sign-in is this device's own key, so this device added it.
    func addedHere(_ record: HostRecord) -> Bool {
        guard case .key(let named) = record.profile.auth, let own = deviceKeyID() else { return false }
        return named == own
    }

    func forget(_ id: HostID) {
        if keys.removeValue(forKey: id) != nil { saveKeys() }
        if signedInIDs.remove(id) != nil { saveSignedIn() }
    }

    private func saveSignedIn() {
        let ids = signedInIDs.map(\.uuidString).sorted()
        backend.set(try? JSONEncoder().encode(ids), forKey: Self.signedInKey)
    }

    private func saveKeys() {
        let byID = Dictionary(uniqueKeysWithValues: keys.map { ($0.key.uuidString, $0.value) })
        backend.set(try? JSONEncoder().encode(byID), forKey: Self.keysKey)
    }
}
