import Foundation

/// `push.pair`. Keys are 32-byte public keys as base64url without padding; pairing an
/// existing `deviceId` replaces it.
public struct PushPairParams: Codable, Hashable, Sendable {
    public enum Environment: String, Codable, Sendable {
        case sandbox, production
    }

    public enum Platform: String, Codable, Sendable {
        case ios, watchos
    }

    public var deviceId: String
    public var name: String
    /// The device's X25519 key that seals pushes to it.
    public var x25519: String
    /// The device's Ed25519 key that signs answers sent through the relay.
    public var approvalKey: String
    /// Lowercase hex.
    public var apnsToken: String
    public var env: Environment
    public var relayUrl: String
    public var liveActivityToken: String?
    public var platform: Platform
    /// Lowercase hex push-to-start token for the Live Activity.
    public var liveActivityStartToken: String?
    /// The user's opt-in to command text in Live Activity pushes.
    public var liveActivityCommand: Bool
    /// The public key this device signs in with over SSH, type and base64 blob only, so the
    /// helper can take it off the machine when the device is revoked. Sent only to a helper
    /// with `authkeys.v1`; an older one turns down a field it does not know.
    public var sshKey: String?

    public init(
        deviceId: String, name: String, x25519: String, approvalKey: String, apnsToken: String,
        env: Environment, relayUrl: String, liveActivityToken: String? = nil,
        platform: Platform = .ios, liveActivityStartToken: String? = nil, liveActivityCommand: Bool = false,
        sshKey: String? = nil
    ) {
        self.deviceId = deviceId
        self.name = name
        self.x25519 = x25519
        self.approvalKey = approvalKey
        self.apnsToken = apnsToken
        self.env = env
        self.relayUrl = relayUrl
        self.liveActivityToken = liveActivityToken
        self.platform = platform
        self.liveActivityStartToken = liveActivityStartToken
        self.liveActivityCommand = liveActivityCommand
        self.sshKey = sshKey
    }

    private enum CodingKeys: String, CodingKey {
        case deviceId, name, x25519, approvalKey, apnsToken, env, relayUrl, liveActivityToken
        case platform, liveActivityStartToken, liveActivityCommand, sshKey
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        deviceId = try container.decode(String.self, forKey: .deviceId)
        name = try container.decode(String.self, forKey: .name)
        x25519 = try container.decode(String.self, forKey: .x25519)
        approvalKey = try container.decode(String.self, forKey: .approvalKey)
        apnsToken = try container.decode(String.self, forKey: .apnsToken)
        env = try container.decode(Environment.self, forKey: .env)
        relayUrl = try container.decode(String.self, forKey: .relayUrl)
        liveActivityToken = try container.decodeIfPresent(String.self, forKey: .liveActivityToken)
        platform = try container.decodeIfPresent(Platform.self, forKey: .platform) ?? .ios
        liveActivityStartToken = try container.decodeIfPresent(String.self, forKey: .liveActivityStartToken)
        liveActivityCommand = try container.decodeIfPresent(Bool.self, forKey: .liveActivityCommand) ?? false
        sshKey = try container.decodeIfPresent(String.self, forKey: .sshKey)
    }

    // platform and liveActivityCommand are written only when they differ from the defaults,
    // so an iPhone pairing reads exactly as it did before they existed.
    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(deviceId, forKey: .deviceId)
        try container.encode(name, forKey: .name)
        try container.encode(x25519, forKey: .x25519)
        try container.encode(approvalKey, forKey: .approvalKey)
        try container.encode(apnsToken, forKey: .apnsToken)
        try container.encode(env, forKey: .env)
        try container.encode(relayUrl, forKey: .relayUrl)
        try container.encodeIfPresent(liveActivityToken, forKey: .liveActivityToken)
        if platform != .ios { try container.encode(platform, forKey: .platform) }
        try container.encodeIfPresent(liveActivityStartToken, forKey: .liveActivityStartToken)
        if liveActivityCommand { try container.encode(true, forKey: .liveActivityCommand) }
        try container.encodeIfPresent(sshKey, forKey: .sshKey)
    }
}

public struct PushPairResult: Codable, Hashable, Sendable {
    public var hostId: String
    /// The helper's X25519 public key, which seals answers sent through the relay.
    public var hostKey: String

    public init(hostId: String, hostKey: String) {
        self.hostId = hostId
        self.hostKey = hostKey
    }
}

public struct PushUnpairParams: Codable, Hashable, Sendable {
    public var deviceId: String

    public init(deviceId: String) {
        self.deviceId = deviceId
    }
}
