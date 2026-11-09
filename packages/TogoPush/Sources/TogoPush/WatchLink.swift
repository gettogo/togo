import Foundation

/// A place in the Watch app that a complication, a control or a notification opens:
/// `togo://waiting` (the oldest waiting request), `togo://waiting/<requestId>` (that one) and
/// `togo://sessions`. The phone's own links (`togo://session/…`, `togo://new`) use other hosts.
public enum WatchLink: Equatable, Hashable, Sendable {
    case waiting
    case request(String)
    case sessions

    public static let scheme = "togo"

    public var url: URL {
        var parts = URLComponents()
        parts.scheme = Self.scheme
        switch self {
        case .waiting:
            parts.host = "waiting"
        case .request(let id):
            parts.host = "waiting"
            if Self.isRequestID(id) { parts.path = "/" + id }
        case .sessions:
            parts.host = "sessions"
        }
        return parts.url ?? URL(string: "togo://waiting")!
    }

    /// Nil for anything that is not one of the Watch's links, including a request id that is
    /// not a plain identifier.
    public init?(url: URL) {
        guard url.scheme?.lowercased() == Self.scheme else { return nil }
        let path = url.pathComponents.filter { $0 != "/" }
        switch url.host()?.lowercased() {
        case "waiting":
            if path.isEmpty {
                self = .waiting
            } else if path.count == 1, Self.isRequestID(path[0]) {
                self = .request(path[0])
            } else {
                return nil
            }
        case "sessions":
            guard path.isEmpty else { return nil }
            self = .sessions
        default:
            return nil
        }
    }

    /// Request ids are short ASCII identifiers that start with a letter or digit; nothing else
    /// is taken from a link.
    public static func isRequestID(_ id: String) -> Bool {
        guard (1...128).contains(id.count), let first = id.first, first.isASCII, first.isLetter || first.isNumber
        else { return false }
        return id.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber || "-_.".contains($0)) }
    }
}

/// A link left by the Watch's control for the app to open, kept in the Watch's app group so it
/// reaches the app whichever process ran the control. It is taken once, and only while fresh.
public enum WatchLinkInbox {
    static let key = "watch.link.pending"
    /// How long a left link stays worth opening.
    public static let maxAge: TimeInterval = 60
    /// Posted in-process after a link is left, so a running app takes it at once.
    public static let posted = Notification.Name("app.togo.watch.linkPosted")

    public static func post(_ link: WatchLink, to defaults: UserDefaults?, now: Date = Date()) {
        defaults?.set(["url": link.url.absoluteString, "at": now.timeIntervalSince1970], forKey: key)
    }

    public static func take(from defaults: UserDefaults?, now: Date = Date()) -> WatchLink? {
        guard let defaults, let entry = defaults.dictionary(forKey: key) else { return nil }
        defaults.removeObject(forKey: key)
        guard let string = entry["url"] as? String, let url = URL(string: string),
              let at = entry["at"] as? Double
        else { return nil }
        let age = now.timeIntervalSince1970 - at
        guard age >= -5, age <= maxAge else { return nil }
        return WatchLink(url: url)
    }
}
