import Foundation
import TogoPush

/// What the Watch's first page shows, chosen from the requests waiting.
enum WatchHome {
    enum Page: Equatable {
        /// The oldest waiting request fills the screen; `position` counts from 1.
        case request(id: String, position: Int, count: Int)
        case allClear
    }

    /// One roster line: mono bib, session name, status word.
    struct Line: Equatable {
        /// The session's identity, stable while its words change.
        var id: String
        var bib: String
        var name: String
        var status: String
        var needsYou: Bool
    }

    /// The first page: the oldest request while any waits, otherwise "All clear".
    /// `requests` are the waiting ones, oldest first.
    static func page(for requests: [PendingRequest]) -> Page {
        guard let first = requests.first else { return .allClear }
        return .request(id: first.requestId, position: 1, count: requests.count)
    }

    /// How long an answered request stays on the page, so its result and haptic land before
    /// the next request takes its place.
    static let holdAfterAnswer: Duration = .milliseconds(1200)
    /// How long a request's buttons wait after it shows, so a second tap meant for the one
    /// before cannot answer it.
    static let armDelay: Duration = .milliseconds(500)

    /// The page while `holding` an answered request: it stays until the hold ends.
    static func page(for requests: [PendingRequest], holding: String?) -> Page {
        if let holding, !requests.contains(where: { $0.requestId == holding }) {
            return .request(id: holding, position: 1, count: 1)
        }
        return page(for: requests)
    }

    /// Whether the page should hold `previous` as the queue moves on: only when it was
    /// answered here and something else took its place.
    static func holds(previous: String?, next: String?, answeredHere: Bool) -> Bool {
        guard let previous, previous != next else { return false }
        return answeredHere
    }

    /// "1 of 3" when several wait; nothing for one.
    static func counter(position: Int, count: Int) -> String? {
        count > 1 ? WatchCopy.position(position, of: count) : nil
    }

    /// "$ npm test" for a command, the bare subject for anything else, nil without one.
    static func commandText(_ request: PendingRequest) -> String? {
        guard let subject = request.subject, subject.isValid else { return nil }
        let text = PromptText.clean(subject.text)
        guard !text.isEmpty else { return nil }
        return subject.kind == "command" ? "$ " + text : text
    }

    /// A session's identity across updates: its machine and session ids where the phone sent
    /// them, otherwise its machine name and bib.
    static func key(_ session: WatchSession) -> String {
        "\(session.machineId ?? session.hostName)/\(session.sessionId ?? session.bib)"
    }

    /// One line per session, those that need you first, at most `limit` lines.
    static func lines(_ sessions: [WatchSession], limit: Int? = nil) -> [Line] {
        let all = sessions.map { session in
            Line(
                id: key(session),
                bib: PromptText.clean(session.bib, maxLines: 1),
                name: PromptText.clean(session.agentName, maxLines: 1),
                status: WatchCopy.status(session),
                needsYou: session.status == "needs_you"
            )
        }
        let ordered = all.filter(\.needsYou) + all.filter { !$0.needsYou }
        guard let limit else { return ordered }
        return Array(ordered.prefix(limit))
    }
}
