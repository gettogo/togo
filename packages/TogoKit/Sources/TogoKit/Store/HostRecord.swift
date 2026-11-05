import Foundation

/// A machine the user added, as stored and synced. Authentication inside
/// `profile` is a reference to a key or keychain item, never secret material.
public struct HostRecord: Codable, Sendable, Equatable, Identifiable {
    public var id: UUID
    public var profile: HostProfile
    public var symbol: String
    public var helperPath: String?
    public var noHelper: Bool

    public init(id: UUID, profile: HostProfile, symbol: String, helperPath: String? = nil, noHelper: Bool = false) {
        self.id = id
        self.profile = profile
        self.symbol = symbol
        self.helperPath = helperPath
        self.noHelper = noHelper
    }
}

/// What a machine reports about the Togo helper.
public enum HelperPresence: Sendable, Equatable {
    case unknown
    case present(version: String)
    case missing
    case outdated(String)
}
