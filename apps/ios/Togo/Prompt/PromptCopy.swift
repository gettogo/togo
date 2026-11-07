import Foundation

/// Every string the prompt bar shows. Keys share the `prompt.` prefix in the catalog.
enum PromptCopy {
    static let yes = String(localized: "prompt.yes", defaultValue: "Yes")
    static let no = String(localized: "prompt.no", defaultValue: "No")
    static let reply = String(localized: "prompt.reply", defaultValue: "Reply")
    static let send = String(localized: "prompt.send", defaultValue: "Send")
    static let cancel = String(localized: "prompt.cancel", defaultValue: "Cancel")
    static let done = String(localized: "prompt.done", defaultValue: "Done")
    static let showAll = String(localized: "prompt.showAll", defaultValue: "Show all")
    static let commandTitle = String(localized: "prompt.commandTitle", defaultValue: "Command")
    static let newSession = String(localized: "prompt.newSession", defaultValue: "New session")
    static let folderOrAgent = String(localized: "prompt.folderOrAgent", defaultValue: "Folder or agent")
    static let moreOptions = String(localized: "prompt.moreOptions", defaultValue: "More options")
    static let more = String(localized: "prompt.more", defaultValue: "More")
    static let dismiss = String(localized: "prompt.dismiss", defaultValue: "Dismiss")
    /// The request went away while the user wrote a reply.
    static let withdrawn = String(localized: "prompt.withdrawn", defaultValue: "No longer waiting for an answer")
    static let replyHint = String(localized: "prompt.replyHint", defaultValue: "Sends your words to the agent instead of yes or no.")

    /// "app · Claude Code on Studio", the context line after the bib.
    static func context(name: String, agent: String, machine: String) -> String {
        String(localized: "prompt.context", defaultValue: "\(name) · \(agent) on \(machine)")
    }

    /// What Yes and No answer, for VoiceOver: "Run npm test? npm test".
    static func answerHint(question: String, command: String) -> String {
        "\(question) \(command)"
    }

    /// "1 of 3".
    static func queue(position: Int, count: Int) -> String {
        String(localized: "prompt.queue", defaultValue: "\(position) of \(count)")
    }

    /// "04 app asks: Run npm test?", posted to VoiceOver when a request arrives.
    static func asks(bib: String, name: String, question: String) -> String {
        String(localized: "prompt.asks", defaultValue: "\(bib) \(name) asks: \(question)")
    }

    /// "Reply to app".
    static func replyTo(_ name: String) -> String {
        String(localized: "prompt.replyTo", defaultValue: "Reply to \(name)")
    }

    /// "Message api".
    static func message(_ name: String) -> String {
        String(localized: "prompt.message", defaultValue: "Message \(name)")
    }

    /// "Claude Code on Studio", a start suggestion's first line.
    static func agentOn(_ agent: String, machine: String) -> String {
        String(localized: "prompt.agentOn", defaultValue: "\(agent) on \(machine)")
    }

    /// VoiceOver's reading of a request's context: "04, app, Claude Code on Studio, 1 of 3".
    static func contextLabel(bib: String, name: String, agent: String, machine: String, queue: String?) -> String {
        [bib, name, agentOn(agent, machine: machine), queue].compactMap { $0 }.joined(separator: ", ")
    }
}
