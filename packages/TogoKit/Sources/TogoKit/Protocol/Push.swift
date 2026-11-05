import Foundation

/// `push.pair`. Keys are 32-byte public keys as base64url without padding; pairing an
/// existing `deviceId` replaces it.
public struct PushPairParams: Codable, Hashable, Sendable {
    public enum Environment: String, Codable, Sendable {
        case sandbox, production
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

    public init(
        deviceId: String, name: String, x25519: String, approvalKey: String, apnsToken: String,
        env: Environment, relayUrl: String, liveActivityToken: String? = nil
    ) {
        self.deviceId = deviceId
        self.name = name
        self.x25519 = x25519
        self.approvalKey = approvalKey
        self.apnsToken = apnsToken
        self.env = env
        self.relayUrl = relayUrl
        self.liveActivityToken = liveActivityToken
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
