import Foundation
import OSLog
import TogoKit
import TogoPush
import TogoTerminal

/// Words the user typed for an agent: an answer to a request that takes text, or a message
/// for an agent waiting at its prompt.
enum Reply: Equatable, Sendable {
    case answer(host: HostID, requestID: String, text: String)
    case prompt(host: HostID, session: String, text: String)

    var host: HostID {
        switch self {
        case .answer(let host, _, _), .prompt(let host, _, _): host
        }
    }

    var text: String {
        switch self {
        case .answer(_, _, let text), .prompt(_, _, let text): text
        }
    }
}

extension Reply {
    /// A reply typed on the Watch. A request is named by the helper's host id, which a pairing
    /// maps to this phone's machine; a session by this phone's machine id, or by the host id
    /// when it came from a notification on the Watch.
    init?(_ message: WatchReplyMessage, pairings: PairingStore) {
        func machine(_ id: String) -> HostID? {
            UUID(uuidString: id) ?? pairings.pairing(hostId: id)?.appHostID
        }
        switch message.target {
        case .answer(let hostId, let requestId):
            guard let host = pairings.pairing(hostId: hostId)?.appHostID else { return nil }
            self = .answer(host: host, requestID: requestId, text: message.text)
        case .prompt(let machineId, let sessionId):
            guard let host = machine(machineId) else { return nil }
            self = .prompt(host: host, session: sessionId, text: message.text)
        }
    }
}

/// What may be typed into an agent. CRLF becomes a line feed; a lone carriage return or a tab
/// becomes a space, so no tab or carriage return is ever sent and words stay apart; then every
/// other control character but the line feed goes (the terminal's paste rule, so no escape
/// sequence can form), then trailing white space.
enum ReplyText {
    /// The helper's limits, in Unicode scalars.
    static let promptLimit = 8000
    static let answerLimit = 2000

    static func clean(_ text: String) -> String {
        var scalars = String.UnicodeScalarView()
        let input = Array(text.unicodeScalars)
        for (i, scalar) in input.enumerated() {
            switch scalar {
            case "\r" where i + 1 < input.count && input[i + 1] == "\n": continue
            case "\r", "\t": scalars.append(" ")
            default: scalars.append(scalar)
            }
        }
        var cleaned = PasteText.sanitize(String(scalars))
        while cleaned.last?.isWhitespace == true { cleaned.removeLast() }
        return cleaned
    }

    /// An answer is one line: the helper takes no line break in one.
    static func answer(_ text: String) -> String {
        clean(text).replacingOccurrences(of: "\n", with: " ")
    }
}

enum ReplyCopy {
    static func sent(_ name: String) -> String {
        String(localized: "reply.sent", defaultValue: "Sent to \(name)")
    }

    static func ended(_ name: String) -> String {
        String(localized: "reply.ended", defaultValue: "Not sent: \(name) has ended.")
    }

    static func asking(_ name: String) -> String {
        String(localized: "reply.asking", defaultValue: "Not sent: \(name) is asking something. Open it to answer.")
    }

    static func noAgent(_ name: String) -> String {
        String(localized: "reply.noAgent", defaultValue: "Not sent: \(name) is not running an agent.")
    }

    static func working(_ name: String) -> String {
        String(localized: "reply.working", defaultValue: "Not sent: \(name) is working. Wait until it's done.")
    }

    static func closed(_ name: String) -> String {
        String(localized: "reply.closed", defaultValue: "Not sent: \(name) is no longer asking.")
    }

    static func failed(_ name: String, reason: String) -> String {
        String(localized: "reply.failed", defaultValue: "Couldn't send to \(name): \(reason)")
    }

    static let empty = String(localized: "reply.empty", defaultValue: "There's nothing to send.")
    static let tooLong = String(localized: "reply.tooLong", defaultValue: "Not sent: the message is too long.")
    static let unreadable = String(localized: "reply.unreadable", defaultValue: "Not sent: the message could not be read.")
    static let unknownMachine = String(
        localized: "reply.unknownMachine", defaultValue: "Not sent: this machine is no longer in Togo."
    )
    static let notReached = String(
        localized: "reply.notReached", defaultValue: "Not sent: the machine could not be reached."
    )
    static let theAgent = String(localized: "reply.theAgent", defaultValue: "the agent")
}

/// Sends a reply over the machine's own helper connection: a text answer through
/// `approval.answer`, a message through `session.sendText` with Enter after it. Free text never
/// goes through the relay.
///
/// A quiet sender (for the Watch) uses tools that never ask the user anything: when the
/// machine cannot be reached that way it returns `needsPhone`.
@MainActor final class ReplySender {
    private let tools: any MachineTools
    private let quiet: Bool
    private let label: @MainActor (Reply) -> String

    init(tools: any MachineTools, quiet: Bool = false, label: @escaping @MainActor (Reply) -> String) {
        self.tools = tools
        self.quiet = quiet
        self.label = label
    }

    func send(_ r: Reply) async -> ReplyOutcome {
        let name = label(r)
        do {
            switch r {
            case .answer(let host, let requestID, let text):
                let line = ReplyText.answer(text)
                if let refusal = Self.check(line, limit: ReplyText.answerLimit) { return refusal }
                try await tools.call(host) { try await $0.answer(requestId: requestID, decision: .text(line)) }
            case .prompt(let host, let session, let text):
                let body = ReplyText.clean(text)
                if let refusal = Self.check(body, limit: ReplyText.promptLimit) { return refusal }
                try await tools.call(host) { try await $0.sendText(id: session, text: body, submit: true) }
            }
            return .sent
        } catch HelperError.conflict(let reason) {
            if case .answer = r { return .refused(ReplyCopy.closed(name)) }
            return .refused(Self.refusal(reason, name: name))
        } catch let error as HelperError {
            let dropped = switch error {
            case .closed, .timeout, .notReady: true
            default: false
            }
            if quiet, dropped { return .needsPhone }
            return .failed(ReplyCopy.failed(name, reason: ServiceErrors.message(error)))
        } catch let error as LiveServiceError {
            return .failed(ReplyCopy.failed(name, reason: ServiceErrors.message(error)))
        } catch {
            // The channel could not open: without asking, only the phone can.
            return quiet ? .needsPhone : .failed(ReplyCopy.failed(name, reason: ServiceErrors.message(error)))
        }
    }

    private static func check(_ text: String, limit: Int) -> ReplyOutcome? {
        if text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return .refused(ReplyCopy.empty) }
        if text.unicodeScalars.count > limit { return .refused(ReplyCopy.tooLong) }
        return nil
    }

    private static func refusal(_ reason: String, name: String) -> String {
        switch reason {
        case "needs_you": ReplyCopy.asking(name)
        case "shell": ReplyCopy.noAgent(name)
        default: ReplyCopy.ended(name)
        }
    }
}

extension AppStore {
    fileprivate static let replyLog = Logger(subsystem: "app.togo", category: "reply")

    /// How a reply names its session: "01 Claude Code", else the request's bib and agent.
    func replyLabel(_ reply: Reply) -> String {
        func row(_ session: String?) -> SessionRow? {
            guard let session else { return nil }
            return controlCenter.machines.first { $0.id == reply.host }?.sessions.first { $0.id == session }
        }
        switch reply {
        case .prompt(_, let session, _):
            if let row = row(session) { return SessionActions.label(row) }
        case .answer(let host, let requestID, _):
            if let card = controlCenter.requests.first(where: { $0.id == requestID && $0.hostID == host }) {
                if let row = row(card.sessionID) { return SessionActions.label(row) }
                return [card.bib, card.agentName].compactMap { $0 }.joined(separator: " ")
            }
        }
        return ReplyCopy.theAgent
    }

    /// Sends a reply typed in a notification and says how it went. Words that were not typed
    /// wait in the session's compose bar, which opens with them.
    @discardableResult
    func deliverReply(_ reply: Reply, session: String? = nil, via sender: ReplySender) async -> ReplyOutcome {
        let outcome = await sender.send(reply)
        switch outcome {
        case .sent:
            notice = Notice(text: ReplyCopy.sent(replyLabel(reply)), isError: false)
        case .refused(let text), .failed(let text):
            notice = Notice(text: text, isError: true)
            keepDraft(reply, session: session)
        case .needsPhone:
            notice = Notice(text: ReplyCopy.notReached, isError: true)
            keepDraft(reply, session: session)
        }
        return outcome
    }

    /// The draft kept for a tab's compose bar, taken once.
    func takeReplyDraft(_ id: TabID) -> String? {
        replyDrafts.removeValue(forKey: id)
    }

    private func keepDraft(_ reply: Reply, session: String?) {
        guard !ReplyText.clean(reply.text).isEmpty else { return }
        let target: String? = switch reply {
        case .prompt(_, let session, _): session
        case .answer(let host, let requestID, _):
            session ?? controlCenter.requests.first { $0.id == requestID && $0.hostID == host }?.sessionID
        }
        guard let target else {
            // A request with no session has no compose bar; the notice says what happened.
            Self.replyLog.notice("Reply to a request with no session kept no draft")
            return
        }
        let id = TabID(hostID: reply.host, sessionID: target)
        replyDrafts[id] = ReplyText.clean(reply.text)
        composing = id
    }
}
