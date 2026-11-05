import Foundation

/// Hello capabilities the app checks before using a method family.
public enum Capability {
    public static let events = "events.v1"
    public static let approvals = "approvals.v1"
    public static let mute = "mute.v1"
    public static let upload = "upload.v1"
    public static let git = "git.v1"
    /// `git.stage`, `git.unstage`, `git.discard`, `git.commit` and `git.diff` with a commit.
    public static let gitWrite = "gitwrite.v1"
    public static let ports = "ports.v1"
    public static let push = "push.v1"
    public static let capture = "capture.v1"
}

/// `fs.upload`: writes `data` as a file named `name` for the session.
///
/// `data` travels as base64url without padding, the protocol-wide rule for bytes.
/// At most `maxBytes` raw bytes fit, which keeps the encoded frame under 1 MiB.
public struct UploadParams: Codable, Hashable, Sendable {
    public static let maxBytes = 700_000

    public var sessionId: String
    public var name: String
    public var data: Data

    public init(sessionId: String, name: String, data: Data) {
        self.sessionId = sessionId
        self.name = name
        self.data = data
    }

    private enum CodingKeys: String, CodingKey {
        case sessionId, name, data
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        sessionId = try container.decode(String.self, forKey: .sessionId)
        name = try container.decode(String.self, forKey: .name)
        let text = try container.decode(String.self, forKey: .data)
        guard let data = Base64URL.decode(text) else {
            throw DecodingError.dataCorruptedError(forKey: .data, in: container, debugDescription: "not base64url")
        }
        self.data = data
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(sessionId, forKey: .sessionId)
        try container.encode(name, forKey: .name)
        try container.encode(Base64URL.encode(data), forKey: .data)
    }
}

public struct UploadResult: Codable, Hashable, Sendable {
    /// Where the helper wrote the file, for pasting into the agent's prompt.
    public var path: String

    public init(path: String) {
        self.path = path
    }
}

enum Base64URL {
    static func encode(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    static func decode(_ text: String) -> Data? {
        var standard = text.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        standard += String(repeating: "=", count: (4 - standard.count % 4) % 4)
        return Data(base64Encoded: standard)
    }
}
