import Foundation

/// One machine this device receives pushes from. Holds no private keys and no tokens: only
/// the 6-character `tokenHash` that tells whether the host has the current APNs token.
public struct Pairing: Codable, Sendable, Equatable, Identifiable {
    public var id: String { hostId }
    public var hostId: String
    /// The helper's X25519 public key, base64url without padding. Answers are sealed to it.
    public var hostKey: String
    public var relayURL: URL
    public var machineName: String
    /// The app's saved host this pairing belongs to; nil on the Watch.
    public var appHostID: UUID?
    public var pairedAt: Date
    public var tokenHash: String
    /// Whether the user allowed command text in Live Activity pushes for this machine.
    public var liveActivityCommand: Bool

    public init(
        hostId: String, hostKey: String, relayURL: URL, machineName: String, appHostID: UUID?, pairedAt: Date,
        tokenHash: String, liveActivityCommand: Bool
    ) {
        self.hostId = hostId
        self.hostKey = hostKey
        self.relayURL = relayURL
        self.machineName = machineName
        self.appHostID = appHostID
        self.pairedAt = pairedAt
        self.tokenHash = tokenHash
        self.liveActivityCommand = liveActivityCommand
    }
}

public final class PairingStore: Sendable {
    private let blob: SharedBlob<Pairing>

    /// Stores pairings in `directory`, normally `SharedGroup.storeDirectory(_:)`. Safe to
    /// open from the app and its extensions at once.
    public init(directory: URL) {
        blob = SharedBlob(directory: directory, name: "pairings")
    }

    public func all() -> [Pairing] { blob.read() }

    public func pairing(hostId: String) -> Pairing? { blob.read().first { $0.hostId == hostId } }

    /// Adds the pairing, or replaces the one with the same host id.
    public func put(_ pairing: Pairing) {
        blob.update { values in
            if let index = values.firstIndex(where: { $0.hostId == pairing.hostId }) {
                values[index] = pairing
            } else {
                values.append(pairing)
            }
        }
    }

    public func remove(hostId: String) {
        blob.update { $0.removeAll { $0.hostId == hostId } }
    }
}
