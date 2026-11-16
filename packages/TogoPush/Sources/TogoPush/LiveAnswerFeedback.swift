import Foundation

/// This device's delivery state, separate from what the helper says the agent is doing.
public enum LiveAnswerFeedback: String, Codable, Hashable, Sendable {
    case sending, sent, failed, answeredElsewhere
    case applied, expired, closed, unconfirmed, notApplied

    public static func resolve(_ request: PendingRequest?, local: Self?, now: Date) -> Self? {
        guard let request else { return local }
        if request.answered == .confirmed { return .applied }
        if let receipt = request.answerReceipt { return receipt.feedback }
        switch request.answered {
        case .sending: return request.claimAbandoned(at: now) ? nil : .sending
        case .approved, .denied, .sent: return .sent
        case .confirmed: return .applied
        case .elsewhere: return .answeredElsewhere
        case nil: return local == .failed ? .failed : nil
        }
    }

    public var blocksAnswer: Bool { self != .failed }
}
