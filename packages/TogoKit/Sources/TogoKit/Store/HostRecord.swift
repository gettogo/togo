import Foundation

/// A machine the user added, as stored and synced. Authentication inside
/// `profile` is a reference to a key or keychain item, never secret material.
public struct HostRecord: Codable, Sendable, Equatable, Identifiable {
    public var id: UUID
    public var profile: HostProfile
    public var symbol: String
    public var helperPath: String?
    public var noHelper: Bool
    /// Where to send a Wake-on-LAN packet, as the machine last reported it.
    public var wake: WakeTarget?
    /// The SSH host key fingerprints the machine presented, so every device can tell a second
    /// record for the same machine apart from a different machine with the same name.
    public var hostKeys: [String]?
    /// The id of the Togo helper on the machine, from pairing it for notifications.
    public var helperHostID: String?
    /// When the machine was added; nil on records saved before it was kept.
    public var addedAt: Date?
    /// Records for this same machine that were folded into this one. Each device moves what it
    /// alone keeps for them, such as its key choice, instead of treating them as removed.
    public var mergedIDs: [UUID]?

    public init(
        id: UUID, profile: HostProfile, symbol: String, helperPath: String? = nil, noHelper: Bool = false,
        wake: WakeTarget? = nil, hostKeys: [String]? = nil, helperHostID: String? = nil, addedAt: Date? = nil,
        mergedIDs: [UUID]? = nil
    ) {
        self.id = id
        self.profile = profile
        self.symbol = symbol
        self.helperPath = helperPath
        self.noHelper = noHelper
        self.wake = wake
        self.hostKeys = hostKeys
        self.helperHostID = helperHostID
        self.addedAt = addedAt
        self.mergedIDs = mergedIDs
    }
}

/// What a machine reports about the Togo helper.
public enum HelperPresence: Sendable, Equatable {
    case unknown
    case present(version: String)
    case missing
    case outdated(String)
    /// The helper speaks a newer protocol than this app, so the app is the side to update.
    case newer
}
