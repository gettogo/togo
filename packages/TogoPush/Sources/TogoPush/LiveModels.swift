import Foundation

public enum SharedGroup {
    /// The app group shared by the iPhone app and its extensions.
    public static let ios = "group.app.togo"
    /// The app group shared by the Watch app and its extensions.
    public static let watch = "group.app.togo.watch"

    /// Where `PairingStore` and `PendingStore` keep their files inside an app group container;
    /// nil when the process lacks the group entitlement.
    public static func storeDirectory(_ group: String) -> URL? {
        FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)?
            .appending(path: "Library/Application Support/Push", directoryHint: .isDirectory)
    }
}

/// WidgetKit kinds the app and its extensions reload by name.
public enum WidgetKinds {
    /// The Home and Lock Screen widget that counts waiting requests.
    public static let waiting = "togo.waiting"
}

#if canImport(ActivityKit) && os(iOS)
import ActivityKit

/// One Live Activity per machine. The helper updates it through the relay; `command` is only
/// set when the user opted in to command text for that machine.
public struct TogoActivityAttributes: ActivityAttributes {
    public struct ContentState: Codable, Hashable, Sendable {
        public var bib: String?
        public var agent: String
        /// `needs_you`, `working`, `done`, `ended`, `failed` or `offline`.
        public var status: String
        public var waiting: Int
        /// Unix seconds when the current status began.
        public var since: Int
        public var requestId: String?
        public var command: String?
        /// Exit reason word (`exit`, `interrupted`, `error` or `usage_limit`); only set when
        /// `status` is `ended` or `failed`.
        public var reason: String?
        /// The session, step and request context, sealed for this device (see `LiveContext`).
        /// Absent from older helpers and relays.
        public var ctx: String?

        public init(
            bib: String?, agent: String, status: String, waiting: Int, since: Int, requestId: String?,
            command: String?, reason: String? = nil, ctx: String? = nil
        ) {
            self.bib = bib
            self.agent = agent
            self.status = status
            self.waiting = waiting
            self.since = since
            self.requestId = requestId
            self.command = command
            self.reason = reason
            self.ctx = ctx
        }

        /// A field a pushed state leaves out takes a default instead of failing the decode:
        /// ActivityKit drops a state it cannot decode without a word, so one missing field
        /// would freeze the activity.
        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            bib = try? c.decodeIfPresent(String.self, forKey: .bib)
            agent = (try? c.decodeIfPresent(String.self, forKey: .agent)) ?? "shell"
            status = (try? c.decodeIfPresent(String.self, forKey: .status)) ?? "working"
            waiting = (try? c.decodeIfPresent(Int.self, forKey: .waiting)) ?? 0
            since = (try? c.decodeIfPresent(Int.self, forKey: .since)) ?? Int(Date().timeIntervalSince1970)
            requestId = try? c.decodeIfPresent(String.self, forKey: .requestId)
            command = try? c.decodeIfPresent(String.self, forKey: .command)
            reason = try? c.decodeIfPresent(String.self, forKey: .reason)
            ctx = try? c.decodeIfPresent(String.self, forKey: .ctx)
        }
    }

    public var hostId: String

    public init(hostId: String) {
        self.hostId = hostId
    }
}
#endif
