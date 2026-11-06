import Foundation

// launch.v1 agent checks, notify.v1, authkeys.v1, hostinfo.v1 and awake.v1. Field names
// follow protocol/v1/schema.json exactly.

/// `agents.check` result: whether a program is on the host's login-shell PATH.
public struct AgentCheck: Codable, Hashable, Sendable {
    public var installed: Bool
    /// The absolute path found; nil when `installed` is false.
    public var path: String?

    public init(installed: Bool, path: String? = nil) {
        self.installed = installed
        self.path = path
    }

    private enum CodingKeys: String, CodingKey { case installed, path }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        installed = try container.decode(Bool.self, forKey: .installed)
        path = try container.decodeIfPresent(String.self, forKey: .path)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(installed, forKey: .installed)
        try container.encode(path, forKey: .path)
    }
}

struct AgentsCheckParams: Codable, Hashable, Sendable {
    var program: String
}

// MARK: - Notify

/// Per-machine notification settings. `idleSeconds` is the quiet time before a "done";
/// `remindMinutes` is the delay before the one reminder for a request, 0 for none.
public struct NotifySettings: Codable, Hashable, Sendable {
    public var idleSeconds: Int
    public var muted: Bool
    public var remindMinutes: Int

    public init(idleSeconds: Int, muted: Bool, remindMinutes: Int) {
        self.idleSeconds = idleSeconds
        self.muted = muted
        self.remindMinutes = remindMinutes
    }
}

/// The fields to change; a nil field keeps its stored value.
public struct NotifyPatch: Codable, Hashable, Sendable {
    public var idleSeconds: Int?
    public var muted: Bool?
    public var remindMinutes: Int?

    public init(idleSeconds: Int? = nil, muted: Bool? = nil, remindMinutes: Int? = nil) {
        self.idleSeconds = idleSeconds
        self.muted = muted
        self.remindMinutes = remindMinutes
    }
}

// MARK: - Authorized keys and host info

struct AuthkeysRemoveParams: Codable, Hashable, Sendable {
    var publicKey: String
}

struct AuthkeysRemoveResult: Codable, Hashable, Sendable {
    var removed: Int
}

public struct HostInterface: Codable, Hashable, Sendable {
    public var name: String
    /// Lowercase colon-separated hex.
    public var mac: String
    public var ipv4: [String]

    public init(name: String, mac: String, ipv4: [String]) {
        self.name = name
        self.mac = mac
        self.ipv4 = ipv4
    }
}

/// What a phone needs to wake this machine with a magic packet.
public struct HostInfo: Codable, Hashable, Sendable {
    public var interfaces: [HostInterface]
    /// Whether the machine reports wake on LAN as enabled; nil when it cannot tell.
    public var wakeOnLan: Bool?

    public init(interfaces: [HostInterface], wakeOnLan: Bool? = nil) {
        self.interfaces = interfaces
        self.wakeOnLan = wakeOnLan
    }

    private enum CodingKeys: String, CodingKey { case interfaces, wakeOnLan }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        interfaces = try container.decode([HostInterface].self, forKey: .interfaces)
        wakeOnLan = try container.decodeIfPresent(Bool.self, forKey: .wakeOnLan)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(interfaces, forKey: .interfaces)
        try container.encode(wakeOnLan, forKey: .wakeOnLan)
    }
}

// MARK: - Keep awake

public enum AwakeMode: WireEnum {
    case off, plugged, battery
    case unknown(String)

    public static let known: [AwakeMode] = [.off, .plugged, .battery]

    public var rawValue: String {
        switch self {
        case .off: "off"
        case .plugged: "plugged"
        case .battery: "battery"
        case .unknown(let value): value
        }
    }
}

public enum AwakeReason: WireEnum {
    case unsupported, off, idle, active, grace, onBattery, needsAdminSetup, batteryFloor, thermal, failed
    case unknown(String)

    public static let known: [AwakeReason] = [
        .unsupported, .off, .idle, .active, .grace, .onBattery, .needsAdminSetup, .batteryFloor, .thermal, .failed,
    ]

    public var rawValue: String {
        switch self {
        case .unsupported: "unsupported"
        case .off: "off"
        case .idle: "idle"
        case .active: "active"
        case .grace: "grace"
        case .onBattery: "on_battery"
        case .needsAdminSetup: "needs_admin_setup"
        case .batteryFloor: "battery_floor"
        case .thermal: "thermal"
        case .failed: "failed"
        case .unknown(let value): value
        }
    }
}

/// Whether the helper is keeping the machine awake, and why or why not.
public struct AwakeStatus: Codable, Hashable, Sendable {
    public var mode: AwakeMode
    /// A sleep block is in place now.
    public var held: Bool
    public var reason: AwakeReason
    /// Why, when `reason` is `failed`.
    public var detail: String?
    /// `ac`, `battery` or another source the helper reports.
    public var power: String
    public var batteryPercent: Int?
    public var batteryFloor: Int
    public var platformSupported: Bool
    /// Battery mode needs `togo awake battery` run on the machine first.
    public var needsAdmin: Bool

    public init(
        mode: AwakeMode, held: Bool, reason: AwakeReason, detail: String? = nil, power: String,
        batteryPercent: Int? = nil, batteryFloor: Int, platformSupported: Bool, needsAdmin: Bool
    ) {
        self.mode = mode
        self.held = held
        self.reason = reason
        self.detail = detail
        self.power = power
        self.batteryPercent = batteryPercent
        self.batteryFloor = batteryFloor
        self.platformSupported = platformSupported
        self.needsAdmin = needsAdmin
    }
}

struct AwakeSetParams: Codable, Hashable, Sendable {
    var mode: AwakeMode?
    var batteryFloor: Int?
}
