import Foundation

public enum AnswerReceiptResult: String, Codable, Sendable {
    case applied, answered, expired, unconfirmed, failed
    case promptGone = "prompt_gone"

    public var feedback: LiveAnswerFeedback {
        switch self {
        case .applied: .applied
        case .answered: .answeredElsewhere
        case .expired: .expired
        case .promptGone: .closed
        case .unconfirmed: .unconfirmed
        case .failed: .notApplied
        }
    }

    public var isFailure: Bool { self != .applied && self != .answered }

    public var message: String? {
        switch self {
        case .applied, .answered: nil
        case .expired:
            String(localized: "answer.receipt.expired", defaultValue: "This request expired before your answer was applied. Open Togo to check the session.")
        case .promptGone:
            String(localized: "answer.receipt.gone", defaultValue: "The request is no longer waiting. Open Togo to check the session.")
        case .unconfirmed:
            String(localized: "answer.receipt.unconfirmed", defaultValue: "Your answer wasn't confirmed. Check the session before trying again.")
        case .failed:
            String(localized: "answer.receipt.failed", defaultValue: "Your answer couldn't be applied. Open Togo to check the session.")
        }
    }
}

/// A validated receipt from a push already opened with this host's pairing and device key.
public struct AnswerReceiptEvent: Sendable {
    public let hostId: String
    public let requestId: String
    public let result: AnswerReceiptResult
    public let ts: Int64

    public init?(payload: PushPayload, hostId: String, now: Date) {
        let age = now.timeIntervalSince1970 - Double(payload.ts) / 1000
        guard payload.v == 1, payload.kind == .answer, let id = payload.requestId, !id.isEmpty,
              let result = payload.result.flatMap(AnswerReceiptResult.init(rawValue:)),
              age <= NotificationServiceCore.maxAge, age >= -NotificationServiceCore.maxSkew else { return nil }
        self.hostId = hostId
        requestId = id
        self.result = result
        ts = payload.ts
    }
}

extension PendingStore {
    /// A relay 202 is delivery only. A receipt may have arrived before this write.
    public func markRelaySent(_ requestId: String) {
        update { values in
            guard let index = values.firstIndex(where: { $0.requestId == requestId }),
                  values[index].answered == .sending else { return }
            values[index].answered = .sent
            values[index].claimedAt = nil
        }
    }

    /// Ordered under the same file lock as claim and sender completion. No receipt opens a
    /// request again, and an actual applied result cannot be downgraded by a later replay.
    @discardableResult public func receive(_ event: AnswerReceiptEvent, now: Date) -> Bool {
        var changed = false
        update { values in
            if !values.contains(where: { $0.requestId == event.requestId }) {
                values.append(PendingRequest(requestId: event.requestId, hostId: event.hostId,
                    sessionId: nil, bib: nil, agent: nil, host: nil, title: "", subject: nil,
                    options: [], receivedAt: now, answered: .sent))
            }
            guard let index = values.firstIndex(where: { $0.requestId == event.requestId }),
                  values[index].hostId == event.hostId, values[index].answered != .confirmed else { return }
            let old = values[index]
            if event.result != .applied, let previous = old.answerReceiptTS, event.ts < previous { return }
            if old.answerReceipt == event.result, old.answerReceiptTS == event.ts { return }
            values[index].answerReceipt = event.result
            values[index].answerReceiptTS = event.ts
            values[index].claimedAt = nil
            switch event.result {
            case .applied: values[index].answered = .confirmed
            case .answered, .expired, .promptGone: values[index].answered = .elsewhere
            case .unconfirmed, .failed: values[index].answered = .sent
            }
            changed = true
        }
        return changed
    }
}
