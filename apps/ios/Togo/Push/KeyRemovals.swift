import Foundation
import Synchronization
import TogoKit

/// This device's line in a machine's `authorized_keys`, split the way the helper needs it.
struct DeviceKeyLine: Codable, Equatable, Sendable {
    /// Type and base64 blob, without options or comment: all the helper is ever sent.
    var publicKey: String
    /// The comment at the end of the line, such as `togo@Sams-iPhone`, which names the line
    /// for someone removing it by hand.
    var comment: String

    private static let types: Set<String> = ["ssh-ed25519", "ecdsa-sha2-nistp256", "ssh-rsa"]

    /// Nil for anything but an OpenSSH public key line of a type the helper accepts.
    init?(line: String) {
        let fields = line.split(whereSeparator: \.isWhitespace).map(String.init)
        guard fields.count >= 2, Self.types.contains(fields[0]), Data(base64Encoded: fields[1]) != nil else { return nil }
        publicKey = fields[0] + " " + fields[1]
        comment = fields.dropFirst(2).joined(separator: " ")
    }

    /// The helper removes only lines whose comment starts with `togo@`, the comment Togo gives
    /// the keys it makes.
    var helperCanRemove: Bool { comment.hasPrefix("togo@") }

    /// The key `record` signs in with when it is this device's own key, the one Togo made;
    /// nil for a password or a key the user imported, which the helper would leave in place.
    static func of(_ record: HostRecord, deviceKeyID: String?, line: (String) -> String?) -> DeviceKeyLine? {
        guard case .key(let id) = record.profile.auth, id == deviceKeyID, let text = line(id),
              let key = DeviceKeyLine(line: text), key.helperCanRemove
        else { return nil }
        return key
    }
}

/// What removing a machine does about this device's key on it, once the helper forgot this
/// device: `removeKey` is nil when the machine is out of reach, `supported` false when its
/// helper is known to lack `authkeys.v1`.
struct KeyRemovalStep {
    var key: DeviceKeyLine
    var name: String
    var supported: Bool
    var removeKey: ((String) async throws -> Int)?
}

/// A removed machine that may still hold this device's key.
struct PendingKeyRemoval: Codable, Equatable, Identifiable, Sendable {
    var id: HostID
    var machineName: String
    var key: DeviceKeyLine
    /// The encoded host record a retry connects with: address, user and key reference.
    var connection: Data?
    /// The helper cannot take the key off (its `authorized_keys` is a link, not a regular file
    /// or missing, or the helper is too old), so the user removes the line by hand. Not retried.
    var manual: Bool

    /// The record a retry connects with.
    var record: HostRecord? {
        connection.flatMap { try? JSONDecoder().decode(HostRecord.self, from: $0) }
    }
}

/// Key removals still to do, kept across launches. Without a file they live in memory only.
final class PendingKeyRemovalStore: Sendable {
    private let file: URL?
    private let entries: Mutex<[PendingKeyRemoval]>

    init(file: URL?) {
        self.file = file
        let data = file.flatMap { try? Data(contentsOf: $0) }
        let stored = data.flatMap { try? JSONDecoder().decode([PendingKeyRemoval].self, from: $0) }
        if let file, data != nil, stored == nil {
            // Unreadable, perhaps from a newer build: kept beside, never overwritten in place.
            let aside = file.appendingPathExtension("bad")
            try? FileManager.default.removeItem(at: aside)
            try? FileManager.default.moveItem(at: file, to: aside)
        }
        entries = Mutex(stored ?? [])
    }

    func all() -> [PendingKeyRemoval] { entries.withLock { $0 } }

    /// Adds the entry, or replaces the one for the same machine.
    func put(_ entry: PendingKeyRemoval) {
        update { values in
            if let index = values.firstIndex(where: { $0.id == entry.id }) {
                values[index] = entry
            } else {
                values.append(entry)
            }
        }
    }

    func remove(_ id: HostID) {
        update { $0.removeAll { $0.id == id } }
    }

    private func update(_ change: (inout [PendingKeyRemoval]) -> Void) {
        let data = entries.withLock { values -> Data? in
            change(&values)
            return try? JSONEncoder().encode(values)
        }
        guard let file, let data else { return }
        try? FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? data.write(to: file, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }
}

extension PairingCoordinator {
    /// Machines that may still hold this device's key.
    var pendingKeyRemovals: [PendingKeyRemoval] { keyRemovals.all() }

    /// Lists the key before anything is asked, so a crash leaves the machine listed. It is
    /// taken off only after the unpair, so the machine can still be reached to unpair.
    func recordKey(_ step: KeyRemovalStep, hostID: HostID, connection: Data?) {
        keyRemovals.put(PendingKeyRemoval(
            id: hostID, machineName: step.name, key: step.key, connection: connection, manual: !step.supported
        ))
        onChange()
    }

    /// Whether another machine in Togo, at the same address and user, signs in with `key`.
    func stillUses(_ key: DeviceKeyLine, at record: HostRecord, besides id: HostID, in records: [HostRecord]) -> Bool {
        records.contains { other in
            other.id != id && Self.sameEndpoint(other.profile.endpoint, record.profile.endpoint)
                && deviceKey(other)?.publicKey == key.publicKey
        }
    }

    /// Host names compare without case; an alias or address for the same machine does not match.
    static func sameEndpoint(_ a: HostEndpoint, _ b: HostEndpoint) -> Bool {
        a.host.lowercased() == b.host.lowercased() && a.port == b.port && a.user == b.user
    }

    /// Key entries whose machine is back in Togo signing in with the key go without asking.
    func dropReturnedKeys(current: [HostRecord]) {
        let ids = Set(current.map(\.id))
        for entry in keyRemovals.all() {
            let back = ids.contains(entry.id)
                || entry.record.map { stillUses(entry.key, at: $0, besides: entry.id, in: current) } == true
            guard back else { continue }
            keyRemovals.remove(entry.id)
            onChange()
        }
    }

    /// Key entries for machines with no unpair left, asked again.
    func retryLoneKeys(_ removeKey: (PendingKeyRemoval) async throws -> Int) async {
        let unpairing = Set(pendingUnpairs.compactMap(\.pairing.appHostID))
        for entry in keyRemovals.all() where !entry.manual && !unpairing.contains(entry.id) {
            await attemptKey(entry.id, removeKey)
        }
    }

    /// Drops a key entry the user gave up on or removed by hand. False when there is none.
    func forgetKeyRemoval(_ id: HostID) -> Bool {
        guard keyRemovals.all().contains(where: { $0.id == id }) else { return false }
        keyRemovals.remove(id)
        onChange()
        return true
    }

    /// Takes the key off the machine `id`, when it has an entry still to try.
    func attemptKey(_ id: HostID, _ removeKey: (PendingKeyRemoval) async throws -> Int) async {
        guard let entry = keyRemovals.all().first(where: { $0.id == id }), !entry.manual else { return }
        do {
            // Zero lines removed means the key was not there: nothing left to do either way.
            _ = try await removeKey(entry)
            keyRemovals.remove(entry.id)
        } catch HelperError.conflict("missing") {
            // No authorized_keys at all, so no line to remove.
            keyRemovals.remove(entry.id)
        } catch HelperError.conflict, HelperError.unsupported {
            markManual(entry)
        } catch HelperError.rpc(let error) where error.code == RPCErrorCode.invalidParams {
            // The helper does not take the key; asking again would not change that.
            markManual(entry)
        } catch {
            // Out of reach: the entry stays for the next retry.
            return
        }
        onChange()
    }

    private func markManual(_ entry: PendingKeyRemoval) {
        var manual = entry
        manual.manual = true
        keyRemovals.put(manual)
    }
}
