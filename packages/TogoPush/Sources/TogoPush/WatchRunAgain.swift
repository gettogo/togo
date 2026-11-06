import Foundation

/// Run again tapped on a phone notification shown on the Watch, sent to the phone with
/// `sendMessage` as `{"type":"runAgain", ...}`. The phone starts the session's command again
/// over its own connection to the machine and answers with a `ReplyOutcome`. The session is
/// named by its machine: the helper's host id from the notification, or the phone's machine id.
///
/// `id` is kept across retries of the same tap, so the phone starts the command at most once.
/// Reading a message checks every field and refuses any other key that names a target or text.
public struct WatchRunAgainMessage: Equatable, Sendable {
    public static let type = "runAgain"

    public var id: String
    public var machineId: String
    public var sessionId: String

    public init(id: String = UUID().uuidString, machineId: String, sessionId: String) {
        self.id = id
        self.machineId = machineId
        self.sessionId = sessionId
    }

    public var message: [String: Any] {
        ["type": Self.type, "id": id, "machineId": machineId, "sessionId": sessionId]
    }

    /// Nil unless the message is a run again with a well-formed id, machine and session and
    /// nothing else.
    public init?(message: [String: Any]) {
        guard message["type"] as? String == Self.type,
              Set(message.keys).isSubset(of: ["type", "id", "machineId", "sessionId"]),
              let id = message["id"] as? String, WatchReplyMessage.isMessageID(id),
              let machine = message["machineId"] as? String,
              WatchReplyMessage.isHostID(machine) || UUID(uuidString: machine) != nil,
              let session = message["sessionId"] as? String, WatchReplyMessage.isSessionID(session)
        else { return nil }
        self.id = id
        machineId = machine
        sessionId = session
    }
}
