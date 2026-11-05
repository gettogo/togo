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
}

public enum NotificationActionID {
    public static let approve = "togo.approve"
    public static let deny = "togo.deny"
    public static let open = "togo.open"
}

/// How an agent's name reads in a notification.
public enum AgentName {
    private static let names = ["claude": "Claude Code", "codex": "Codex", "omp": "OMP", "opencode": "OpenCode"]

    public static func display(_ agent: String) -> String {
        if let known = names[agent.lowercased()] { return known }
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
            var title = header(p)
            if multipleHosts, let host = (p.host ?? hostName).map({ PromptText.clean($0, maxLines: 1) }),
               !host.isEmpty {
                let header = title
                title = String(localized: "notif.onHost", defaultValue: "\(header) on \(host)")
            }
            return NotificationText(
                title: title, subtitle: PromptText.clean(p.title, maxLines: 1), body: subjectText(p),
                category: category(p.options ?? []), threadID: thread, interruption: .timeSensitive
            )
        case .done:
            return NotificationText(
                title: header(p), subtitle: String(localized: "notif.done", defaultValue: "Done"), body: PromptText.clean(p.body), category: NotificationCategoryID.info,
                threadID: thread, interruption: .active
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

    /// Lock-screen actions only when the options let the helper pick the answer by role.
    private static func category(_ options: [PushOption]) -> String {
        let roles = Set(options.map(\.role))
        switch (roles.contains("approve"), roles.contains("deny")) {
        case (true, true): return NotificationCategoryID.request
        case (false, true): return NotificationCategoryID.requestDenyOnly
        default: return NotificationCategoryID.info
        }
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
