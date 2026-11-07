import Foundation

/// Something an agent is waiting on, as the bar shows it.
struct PromptRequest: Identifiable, Equatable, Sendable {
    var id: String
    var sessionID: String
    var bib: String
    var sessionName: String
    var agentName: String
    var machineName: String
    /// The request as a question, such as "Run npm test?".
    var question: String
    /// The command the agent wants to run, when there is one.
    var command: String?
    /// Whether the agent takes words in answer. When it does not, the bar offers no Reply:
    /// typing into a prompt that is still open would pick one of its options.
    var allowsReply = true
    /// Whether Yes and No mean something here. A question that only takes words has neither.
    var asksYesOrNo = true
    /// The agent's options beyond its one yes and one no, such as "Yes, and don't ask again".
    var more: [PromptOption] = []
    /// Where the answer stands.
    var state: PromptRequestState = .open

    /// Whether an answer can be sent now.
    var answerable: Bool {
        switch state {
        case .open, .failed: true
        case .sending, .done, .withdrawn: false
        }
    }
}

/// One of the agent's own options, by the number it answers to.
struct PromptOption: Identifiable, Equatable, Hashable, Sendable {
    var id: Int
    var label: String
}

enum PromptRequestState: Equatable, Sendable {
    case open
    /// An answer is on its way.
    case sending
    /// The last answer did not arrive; the request is open again.
    case failed(String)
    /// Answered, shown for a moment before it leaves.
    case done(PromptOutcome)
    /// The agent stopped asking, or it was answered somewhere else, while the user wrote a
    /// reply. The reply line stays, with its words, until the user puts it away.
    case withdrawn
}

enum PromptOutcome: Equatable, Sendable {
    case approved, denied, answeredElsewhere, expired

    var label: String {
        switch self {
        case .approved: ControlCenterCopy.approved
        case .denied: ControlCenterCopy.denied
        case .answeredElsewhere: ControlCenterCopy.answeredElsewhere
        case .expired: ControlCenterCopy.expired
        }
    }
}

enum PromptAnswer: Equatable, Sendable {
    case yes, no
    /// One of the agent's further options, by its number.
    case option(Int)
    case reply(String)
}
