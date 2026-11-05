import Foundation

/// A machine the user removed whose helper may still push to this device. It stays here,
/// out of `PairingStore`, until the helper confirms it forgot every device id, so its pushes
/// open as nothing and the app keeps asking the helper to stop.
public struct PendingUnpair: Codable, Sendable, Equatable, Identifiable {
    public var id: String { pairing.hostId }
    public var pairing: Pairing
    /// Device ids the helper has not yet confirmed forgetting.
    public var deviceIds: [String]
    /// How the app reaches the machine, encoded by the app: address, user and key reference.
    /// Never a password or a private key.
    public var connection: Data?

    public init(pairing: Pairing, deviceIds: [String], connection: Data?) {
        self.pairing = pairing
        self.deviceIds = deviceIds
        self.connection = connection
    }
}

public final class PendingUnpairStore: Sendable {
    private let blob: SharedBlob<PendingUnpair>

    public init(directory: URL) {
        blob = SharedBlob(directory: directory, name: "pending-unpair")
    }

    public func all() -> [PendingUnpair] { blob.read() }

    /// Adds the entry, or replaces the one for the same host id.
    public func put(_ entry: PendingUnpair) {
        blob.update { values in
            if let index = values.firstIndex(where: { $0.id == entry.id }) {
                values[index] = entry
            } else {
                values.append(entry)
            }
        }
    }

    public func remove(hostId: String) {
        blob.update { $0.removeAll { $0.id == hostId } }
    }

    /// Records the entry first, then drops the pairing, so a crash in between leaves the
    /// machine still listed for unpairing rather than forgotten.
    public func retire(_ entry: PendingUnpair, from pairings: PairingStore) {
        put(entry)
        pairings.remove(hostId: entry.pairing.hostId)
    }
}
