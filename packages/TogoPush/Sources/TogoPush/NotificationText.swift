import Foundation

/// What a notification shows for one push, built only from structured fields.
public struct NotificationText: Equatable, Sendable {
    public enum Interruption: Sendable {
        case active, timeSensitive, passive
    }

    public var title: String
    public var subtitle: String
    public var body: String
    public var category: String
    public var threadID: String
    public var interruption: Interruption

    public init(
        title: String, subtitle: String, body: String, category: String, threadID: String,
        interruption: Interruption
    ) {
        self.title = title
        self.subtitle = subtitle
        self.body = body
        self.category = category
        self.threadID = threadID
        self.interruption = interruption
    }
}

public enum NotificationCategoryID {
    public static let request = "togo.request"
    public static let requestDenyOnly = "togo.request.deny"
    public static let info = "togo.info"
    /// A session whose agent stopped and can be run again.
    public static let ended = "togo.ended"
    /// The agent waits at its prompt for words: a done, or a stop on a usage limit, where the
    /// agent is still alive. Offers Reply.
    public static let prompt = "togo.prompt"
    /// Requests that take a typed answer: the request's buttons and Reply.
    public static let requestReply = "togo.request.reply"
    public static let requestDenyOnlyReply = "togo.request.deny.reply"
    /// A request that takes a typed answer and offers no option a button can pick: Reply and Open.
    public static let requestText = "togo.request.text"
    /// A single question's own options, one button each. Registered per request, as
    /// `choicePrefix` + request id, since the buttons carry the option labels.
    public static let choicePrefix = "togo.choice."

    public static func choice(_ requestId: String) -> String { choicePrefix + requestId }
}

public enum NotificationActionID {
    public static let approve = "togo.approve"
    public static let deny = "togo.deny"
    public static let open = "togo.open"
    /// Opens the app and runs the session's command again in place.
    public static let runAgain = "togo.run-again"
    /// Types the user's words into the agent, over the machine's own connection only.
    public static let reply = "togo.reply"
    /// Picks option n of a question: `optionPrefix` + n.
    public static let optionPrefix = "togo.option."

    public static func option(_ n: Int) -> String { optionPrefix + String(n) }

    /// The option number an action picks; nil for any other action.
    public static func optionNumber(_ identifier: String) -> Int? {
        guard identifier.hasPrefix(optionPrefix), let n = Int(identifier.dropFirst(optionPrefix.count)), n > 0,
              identifier == option(n)
        else { return nil }
        return n
    }
}

/// How an agent's name reads in a notification.
public enum AgentName {
    /// The helper registry's names (TogoKit `AgentCatalog` holds the same list).
    private static let names = [
        "claude": "Claude Code", "omp": "OMP", "codex": "Codex", "opencode": "OpenCode",
        "copilot": "Copilot CLI", "cursor": "Cursor CLI",
        "aider": "Aider", "amp": "Amp", "auggie": "Auggie", "cline": "Cline", "crush": "Crush",
        "droid": "Droid", "goose": "Goose", "kilo": "Kilo Code", "kimi": "Kimi Code", "kiro": "Kiro CLI",
        "openhands": "OpenHands", "pi": "Pi", "vibe": "Mistral Vibe",
    ]

    /// A custom program has no agent name: "" so the session's own name stands for it.
    public static func display(_ agent: String) -> String {
        if let known = names[agent.lowercased()] { return known }
        if agent.lowercased() == "custom" { return "" }
        let cleaned = PromptText.clean(agent, maxLines: 1)
        return cleaned.prefix(1).uppercased() + cleaned.dropFirst()
    }
}

public enum NotificationTextBuilder {
    /// Builds a notification from the structured fields of a push; nil for `clear`, which
    /// removes notifications instead of posting one. Every string passes `PromptText.clean`.
    ///
    /// - Parameters:
    ///   - hostId: the machine that sealed the push; the thread when there is no session.
    ///   - hostName: the machine's name when the push does not carry one.
    ///   - multipleHosts: whether the device is paired with more than one machine, so a
    ///     request names the machine it came from.
    public static func build(
        _ p: PushPayload, hostId: String = "", hostName: String? = nil, multipleHosts: Bool = false
    ) -> NotificationText? {
        let thread = p.sessionId ?? hostId
        switch p.kind {
        case .clear:
            return nil
        case .needsYou:
            var title = titled(p)
            if multipleHosts, let host = (p.host ?? hostName).map({ PromptText.clean($0, maxLines: 1) }),
               !host.isEmpty {
                let header = title
                title = String(localized: "notif.onHost", defaultValue: "\(header) on \(host)")
            }
            let category = category(p.options ?? [], allowText: p.allowText == true, requestId: p.requestId)
            var body = subjectText(p)
            if category == NotificationCategoryID.info, body.isEmpty {
                // No button can answer it (a question with several tabs, or a prompt not on
                // screen yet): say where it is answered instead of offering buttons that fail.
                body = openToAnswer
            }
            return NotificationText(
                title: title, subtitle: PromptText.clean(p.title, maxLines: 1), body: body,
                category: category, threadID: thread, interruption: .timeSensitive
            )
        case .done:
            return NotificationText(
                title: titled(p), subtitle: String(localized: "notif.done", defaultValue: "Done"), body: PromptText.clean(p.body),
                category: replyCategory(p), threadID: thread, interruption: .active
            )
        case .ended, .failed:
            let status = LifecycleWords.status(status: p.kind.rawValue, reason: p.exit?.reason, code: p.exit?.code)
                ?? LifecycleWords.ended
            let detail = p.exit.flatMap { LifecycleWords.detail(reason: $0.reason, code: $0.code) }
            return NotificationText(
                title: titled(p), subtitle: "", body: [status, detail].compactMap { $0 }.joined(separator: " · "),
                category: stoppedCategory(p),
                threadID: thread, interruption: .active
            )
        case .offline:
            // The machine's name comes from the pairing and the count from `active`; the
            // payload's own title and body are never shown.
            let name = PromptText.clean(hostName ?? p.host ?? "", maxLines: 1)
            let host = name.isEmpty ? String(localized: "notif.offline.machine", defaultValue: "A machine") : name
            return NotificationText(
                title: String(localized: "notif.offline.title", defaultValue: "\(host) stopped answering"),
                subtitle: "", body: offlineBody(active: p.active),
                category: NotificationCategoryID.info, threadID: hostId.isEmpty ? thread : hostId,
                interruption: .timeSensitive
            )
        case .job:
            return NotificationText(
                title: header(p), subtitle: PromptText.clean(p.title, maxLines: 1), body: jobOutcome(p.body),
                category: NotificationCategoryID.info, threadID: thread, interruption: .active
            )
        case .notify:
            let title = PromptText.clean(p.title, maxLines: 1)
            return NotificationText(
                title: title.isEmpty ? header(p) : title, subtitle: "", body: PromptText.clean(p.body),
                category: NotificationCategoryID.info, threadID: thread, interruption: .active
            )
        }
    }

    /// How many sessions were working when the machine went quiet.
    public static func offlineBody(active: Int?) -> String {
        switch active ?? 0 {
        case 1: String(localized: "notif.offline.one", defaultValue: "1 session was working.")
        case let n where n > 1: String(localized: "notif.offline.many", defaultValue: "\(n) sessions were working.")
        default: String(localized: "notif.offline.some", defaultValue: "Sessions were working.")
        }
    }

    /// The body of a request no notification button can answer.
    public static let openToAnswer = String(localized: "notif.openToAnswer", defaultValue: "Open to answer")

    /// `"<bib> <agent> · <session>"`: the header with the session's name when the push has one
    /// that says more than the agent's name.
    private static func titled(_ p: PushPayload) -> String {
        let head = header(p)
        let name = p.sessionName.map { PromptText.clean($0, maxLines: 1) } ?? ""
        guard !name.isEmpty, name != p.agent.map(AgentName.display) else { return head }
        return "\(head) · \(name)"
    }

    /// `"<bib> <agent>"`, either part when the other is missing, else "Togo".
    private static func header(_ p: PushPayload) -> String {
        let parts = [
            p.bib.map { PromptText.clean($0, maxLines: 1) },
            p.agent.map(AgentName.display),
        ].compactMap { $0 }.filter { !$0.isEmpty }
        return parts.isEmpty ? "Togo" : parts.joined(separator: " ")
    }

    /// The subject when there is one: a command or file as it is, a URL as host and path.
    private static func subjectText(_ p: PushPayload) -> String {
        guard let subject = p.subject, subject.isValid else { return PromptText.clean(p.body) }
        if subject.kind == "url", let url = URLComponents(string: subject.text), let host = url.host, !host.isEmpty {
            return PromptText.clean(host + url.path)
        }
        return PromptText.clean(subject.text)
    }

    /// Reply needs the session it types into; without one there is nothing to offer.
    private static func replyCategory(_ p: PushPayload) -> String {
        guard let session = p.sessionId, !session.isEmpty else { return NotificationCategoryID.info }
        return NotificationCategoryID.prompt
    }

    /// Lock-screen buttons only when the options let the helper pick the answer by role; Reply
    /// beside them when the request takes a typed answer. A question's own options get one
    /// button each (see `NotificationCategorySet.choices`).
    private static func category(_ options: [PushOption], allowText: Bool, requestId: String?) -> String {
        let roles = Set(options.map(\.role))
        switch (roles.contains("approve"), roles.contains("deny"), allowText) {
        case (true, true, false): return NotificationCategoryID.request
        case (true, true, true): return NotificationCategoryID.requestReply
        case (false, true, false): return NotificationCategoryID.requestDenyOnly
        case (false, true, true): return NotificationCategoryID.requestDenyOnlyReply
        default:
            if let requestId, NotificationCategorySet.offersChoices(options) {
                return NotificationCategoryID.choice(requestId)
            }
            return allowText ? NotificationCategoryID.requestText : NotificationCategoryID.info
        }
    }

    /// A stop runs again, unless the agent is still alive at its prompt (a usage limit or an
    /// error it reported), which gets Reply. Both need the session they act on.
    private static func stoppedCategory(_ p: PushPayload) -> String {
        let status = p.kind.rawValue, reason = p.exit?.reason, code = p.exit?.code
        if LifecycleWords.agentWaiting(status: status, reason: reason, code: code) { return replyCategory(p) }
        guard LifecycleWords.canRunAgain(status: status, reason: reason, code: code),
              let session = p.sessionId, !session.isEmpty
        else { return NotificationCategoryID.info }
        return NotificationCategoryID.ended
    }

    /// The helper sends the job's status, or its message when it has one.
    private static func jobOutcome(_ body: String) -> String {
        switch body {
        case "ok": String(localized: "notif.job.finished", defaultValue: "Finished")
        case "failed": String(localized: "notif.job.failed", defaultValue: "Failed")
        default: PromptText.clean(body)
        }
    }
}

/// How a session whose agent stopped reads, in the app, its notifications, the Live Activity and
/// on the Watch. Only the status and the exit reason and code choose the words; the helper's own
/// text is never shown.
public enum LifecycleWords {
    public static let ended = String(localized: "lifecycle.ended", defaultValue: "Ended")
    public static let error = String(localized: "lifecycle.error", defaultValue: "Stopped with an error")
    public static let usageLimit = String(localized: "lifecycle.usageLimit", defaultValue: "Hit a usage limit")
    public static let restart = String(localized: "lifecycle.restart", defaultValue: "Ended by restart")

    /// The words for `ended` and `failed`; nil for any other status. `reason` and `code` come
    /// from the session's exit when the helper reported one. A reason a newer helper adds
    /// reads as the status alone.
    public static func status(status: String, reason: String?, code: Int?) -> String? {
        guard status == "ended" || status == "failed" else { return nil }
        switch reason {
        case "usage_limit": return usageLimit
        case "restart": return restart
        case "error": return error
        case "interrupted": return ended
        case "exit": return (code ?? 0) == 0 ? ended : error
        default: return status == "failed" ? error : ended
        }
    }

    /// The exit reason words a helper sends. `exit` is left out: it says nothing the status
    /// does not, and the exit code that qualifies it is not carried everywhere.
    private static let knownReasons: Set<String> = ["interrupted", "error", "usage_limit", "restart"]

    /// `reason` when it is one of the words above, else nil, so an unknown or empty word reads
    /// as the plain status instead of reaching the screen.
    public static func knownReason(_ reason: String?) -> String? {
        reason.flatMap { knownReasons.contains($0) ? $0 : nil }
    }

    /// "Exited with code 1" for an agent that exited on its own with a code other than 0. Ctrl-C and
    /// a usage limit say all there is in the status.
    public static func detail(reason: String?, code: Int?) -> String? {
        guard let code, code != 0, reason == "exit" || reason == "error" else { return nil }
        return String(localized: "lifecycle.exitCode", defaultValue: "Exited with code \(code)")
    }

    /// Whether a stopped agent is still alive at its prompt, waiting for words. The helper
    /// records a usage limit or an error the agent reported as failed with no exit code; the
    /// code arrives only once the process is gone. It takes a reply then, and refuses to start
    /// the agent again.
    public static func agentWaiting(status: String, reason: String?, code: Int?) -> Bool {
        status == "failed" && code == nil && (reason == "error" || reason == "usage_limit")
    }

    /// Whether a stopped agent can be started again: it ended or failed and is not still alive
    /// at its prompt. A usage limit never runs again.
    public static func canRunAgain(status: String, reason: String?, code: Int?) -> Bool {
        (status == "ended" || status == "failed") && reason != "usage_limit"
            && !agentWaiting(status: status, reason: reason, code: code)
    }
}
