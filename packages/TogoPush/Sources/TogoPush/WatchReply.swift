import Foundation

/// A reply typed on the Watch, sent to the phone with `sendMessage` as
/// `{"type":"reply", ...}`. Only the phone sends it on, over its own connection to the machine;
/// it never goes through the relay. An answer names its request and the helper's host id; a
/// prompt names the session and its machine, by the phone's machine id or the helper's host id.
///
/// `id` is made on the Watch and kept across retries of the same words, so the phone types
/// them at most once. Reading a message checks every field: the phone acts on none it cannot
/// read exactly.
public struct WatchReplyMessage: Equatable, Sendable {
    public static let type = "reply"
    /// More than this many bytes of UTF-8 is refused before any cleaning.
    public static let maxTextBytes = 32 * 1024

    public enum Target: Hashable, Sendable {
        case answer(hostId: String, requestId: String)
        case prompt(machineId: String, sessionId: String)
    }

    public var id: String
    public var target: Target
    public var text: String

    public init(id: String = UUID().uuidString, target: Target, text: String) {
        self.id = id
        self.target = target
        self.text = text
    }

    public var message: [String: Any] {
        var message: [String: Any] = ["type": Self.type, "id": id, "text": text]
        switch target {
        case .answer(let hostId, let requestId):
            message["hostId"] = hostId
            message["requestId"] = requestId
        case .prompt(let machineId, let sessionId):
            message["machineId"] = machineId
            message["sessionId"] = sessionId
        }
        return message
    }

    /// Nil unless the message is a reply with well-formed ids, exactly one target and text of
    /// a sane size. A message from a Watch that predates ids gets a fresh one.
    public init?(message: [String: Any]) {
        guard message["type"] as? String == Self.type, let text = message["text"] as? String,
              text.utf8.count <= Self.maxTextBytes
        else { return nil }
        let id: String
        switch message["id"] {
        case nil: id = UUID().uuidString
        case let given as String where Self.isMessageID(given): id = given
        default: return nil
        }
        let hostId = message["hostId"], requestId = message["requestId"]
        let machineId = message["machineId"], sessionId = message["sessionId"]
        switch (hostId, requestId, machineId, sessionId) {
        case (let host as String, let request as String, nil, nil):
            guard Self.isHostID(host), Self.isRequestID(request) else { return nil }
            target = .answer(hostId: host, requestId: request)
        case (nil, nil, let machine as String, let session as String):
            guard Self.isHostID(machine) || UUID(uuidString: machine) != nil, Self.isSessionID(session) else { return nil }
            target = .prompt(machineId: machine, sessionId: session)
        default:
            return nil
        }
        self.id = id
        self.text = text
    }

    /// `h_` and 26 of a-z, 2-7.
    public static func isHostID(_ s: String) -> Bool { matches(s, prefix: "h_", count: 26) }
    /// `r_` and 10 of a-z, 2-7.
    public static func isRequestID(_ s: String) -> Bool { matches(s, prefix: "r_", count: 10) }
    /// A managed session: `s_` and 10 of a-z, 2-7. Plain shells take no reply.
    public static func isSessionID(_ s: String) -> Bool { matches(s, prefix: "s_", count: 10) }

    static func isMessageID(_ s: String) -> Bool {
        (1...64).contains(s.utf8.count) && s.unicodeScalars.allSatisfy {
            ("a"..."z").contains($0) || ("A"..."Z").contains($0) || ("0"..."9").contains($0) || $0 == "-" || $0 == "_"
        }
    }

    private static func matches(_ s: String, prefix: String, count: Int) -> Bool {
        guard s.hasPrefix(prefix) else { return false }
        let rest = s.unicodeScalars.dropFirst(prefix.unicodeScalars.count)
        return rest.count == count && rest.allSatisfy { ("a"..."z").contains($0) || ("2"..."7").contains($0) }
    }
}
