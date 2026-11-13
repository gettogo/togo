import Foundation

/// Hello capabilities the app checks before using a method family.
public enum Capability {
    public static let events = "events.v1"
    public static let approvals = "approvals.v1"
    public static let mute = "mute.v1"
    public static let upload = "upload.v1"
    /// `fs.image`.
    public static let image = "image.v1"
    public static let git = "git.v1"
    /// `git.stage`, `git.unstage`, `git.discard`, `git.commit` and `git.diff` with a commit.
    public static let gitWrite = "gitwrite.v1"
    /// `git.files`, and `git.log` with `graph` and commit parents.
    public static let gitFiles = "gitfiles.v1"
    public static let ports = "ports.v1"
    public static let push = "push.v1"
    public static let liveContext = "livecontext.v1"
    public static let capture = "capture.v1"
    public static let launch = "launch.v1"
    public static let lifecycle = "lifecycle.v1"
    public static let restore = "restore.v1"
    public static let notify = "notify.v1"
    public static let reply = "reply.v1"
    public static let authkeys = "authkeys.v1"
    /// `push.pair` takes `doneSilent`.
    public static let doneSilent = "donesilent.v1"
    public static let hostInfo = "hostinfo.v1"
    public static let awake = "awake.v1"
    /// The `browser.open` event and `browser.openOnMac`.
    public static let browser = "browser.v1"
    /// `fs.forgetRecentDir`.
    public static let recent = "recent.v1"
    /// The harness.* methods, the `harness` notification and `Session.mission`.
    public static let harness = "harness.v1"
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

/// `fs.image`: a picture the chat shows, from the session's uploads or its folder.
public struct FsImageParams: Codable, Hashable, Sendable {
    public var sessionId: String
    public var path: String

    public init(sessionId: String, path: String) {
        self.sessionId = sessionId
        self.path = path
    }
}

public struct FsImageResult: Codable, Hashable, Sendable {
    public var mediaType: String
    public var data: Data
    /// The file's size on disk.
    public var bytes: Int
    /// The picture was shrunk to fit.
    public var scaled: Bool

    public init(mediaType: String, data: Data, bytes: Int, scaled: Bool) {
        self.mediaType = mediaType
        self.data = data
        self.bytes = bytes
        self.scaled = scaled
    }

    private enum CodingKeys: String, CodingKey { case mediaType, data, bytes, scaled }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        mediaType = try c.decode(String.self, forKey: .mediaType)
        guard let data = Base64URL.decode(try c.decode(String.self, forKey: .data)) else {
            throw DecodingError.dataCorruptedError(forKey: .data, in: c, debugDescription: "not base64url")
        }
        self.data = data
        bytes = try c.decode(Int.self, forKey: .bytes)
        scaled = try c.decode(Bool.self, forKey: .scaled)
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(mediaType, forKey: .mediaType)
        try c.encode(Base64URL.encode(data), forKey: .data)
        try c.encode(bytes, forKey: .bytes)
        try c.encode(scaled, forKey: .scaled)
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
