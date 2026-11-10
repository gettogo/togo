import CryptoKit
import Foundation
import Observation
import TogoPush
import WidgetKit

/// The Watch's requests and sessions.
///
/// Requests come from the Watch's own silent pushes and live in the Watch's `PendingStore`,
/// which its complication reads too. The phone's context only ever closes requests, and only
/// the ones it names as closed: the phone can miss a request the Watch got. An answered or
/// closed request stays in the store as answered until it ages out, so a late context or a
/// replayed push cannot bring it back.
@MainActor @Observable final class WatchStore {
    enum Outcome: Equatable, Sendable {
        case sending, approved, denied, failed
        /// The phone has not given this Watch a pairing for the request's machine yet.
        case notSetUp
    }

    /// What a request's screen shows. Only `open`, `failed` and `notSetUp` offer Approve and Deny.
    enum RequestState: Equatable, Sendable {
        case open, sending, failed
        /// This Watch cannot answer for the machine until the phone sends its pairing.
        case notSetUp
        /// The relay took this screen's answer.
        case approved, denied
        /// Answered on this Watch some other way, such as a notification action.
        case answered
        /// Answered on the phone or at the desk, or closed by the machine.
        case answeredElsewhere
        /// Past the ten minutes the helper accepts an answer in.
        case expired

        var isActionable: Bool { self == .open || self == .failed || self == .notSetUp }
    }

    struct Dependencies {
        var pending: PendingStore
        var pairings: PairingStore
        /// A `RelayAnswerSender` given the same `pending` store.
        var sender: any AnswerSending
        var clock: @Sendable () -> Date = { Date() }
        var reloadComplication: () -> Void = {
            WidgetCenter.shared.reloadTimelines(ofKind: WatchStore.complicationKind)
        }
    }

    nonisolated static let complicationKind = "togo.watch.waiting"
    /// How old the phone's context may get before the sessions say when they are from.
    nonisolated static let freshContext: TimeInterval = 300

    /// Requests waiting on the user, oldest first, including one being sent.
    private(set) var requests: [PendingRequest] = []
    private(set) var sessions: [WatchSession] = []
    private(set) var sessionsAsOf: Date?
    private(set) var outcomes: [String: Outcome] = [:]
    /// Bumped when a tap is refused, so the screen shows why at once.
    private(set) var revision = 0
    /// Open requests the phone named as taking a typed answer.
    private(set) var textRequestIds: Set<String> = []
    /// Sends a reply through the phone, which alone reaches the machine with it. Set by the
    /// app delegate once the phone link exists.
    @ObservationIgnored var sendReply: @MainActor (WatchReplyMessage) async -> ReplyOutcome = { _ in .needsPhone }
    /// Hands Run again to the phone, which starts the session over its own connection.
    @ObservationIgnored var sendRunAgain: @MainActor (WatchRunAgainMessage) async -> ReplyOutcome = { _ in .needsPhone }
    /// A reply screen to show over everything, opened from a notice about a reply that did not go.
    var openReply: ReplyRoute?
    /// One reply screen per request or session, so typed words outlast leaving the screen.
    @ObservationIgnored private var replies: [WatchReplyMessage.Target: WatchReplyModel] = [:]

    @ObservationIgnored private let deps: Dependencies

    init(dependencies: Dependencies) {
        deps = dependencies
        releaseAbandonedClaims()
        reload()
    }

    /// Only this app claims requests on the Watch, so at launch nothing is being sent: a claim
    /// still marked sending was left by an app stopped mid-send, and would keep its request on
    /// "Sending" and refuse every tap until it aged out. It is given back.
    private func releaseAbandonedClaims() {
        for request in deps.pending.all() where request.answered == .sending {
            giveBack(request)
        }
    }

    /// Undoes a claim. A record that `claim` made for an id the Watch never got a push for holds
    /// nothing to show, so it goes rather than open as a blank request.
    private func giveBack(_ request: PendingRequest) {
        if Self.isPlaceholder(request) {
            deps.pending.remove(request.requestId)
        } else {
            deps.pending.release(request.requestId)
        }
    }

    private static func isPlaceholder(_ request: PendingRequest) -> Bool {
        request.title.isEmpty && request.options.isEmpty && request.sessionId == nil && request.bib == nil
            && request.agent == nil && request.host == nil
    }

    func outcome(_ requestId: String) -> Outcome? { outcomes[requestId] }

    /// A request by id, still there after it was answered so its screen can say so.
    func request(_ requestId: String) -> PendingRequest? {
        requests.first { $0.requestId == requestId } ?? deps.pending.request(requestId)
    }

    /// Where a request stands now. The expiry is checked against the clock each time.
    func state(of requestId: String) -> RequestState {
        state(of: requestId, at: deps.clock())
    }

    func state(of requestId: String, at now: Date) -> RequestState {
        switch outcomes[requestId] {
        case .approved: return .approved
        case .denied: return .denied
        case .sending: return .sending
        case .failed, .notSetUp, nil: break
        }
        guard let request = deps.pending.request(requestId) else { return .answeredElsewhere }
        switch request.answered {
        case .approved, .denied: return .answered
        case .elsewhere: return .answeredElsewhere
        case .sending: return .sending
        case nil: break
        }
        if Self.expired(request, at: now) { return .expired }
        switch outcomes[requestId] {
        case .failed: return .failed
        case .notSetUp: return .notSetUp
        default: return .open
        }
    }

    /// Why the last answer to a request did not go, for the notice after a notification action.
    func failureNotice(_ requestId: String) -> String? {
        switch outcomes[requestId] {
        case .failed: String(localized: "notif.sendFailed", defaultValue: "Could not send. Open Togo to answer.")
        case .notSetUp: WatchCopy.notSetUp(machine: deps.pending.request(requestId)?.host)
        default: nil
        }
    }

    private static func expired(_ request: PendingRequest, at now: Date) -> Bool {
        now.timeIntervalSince(request.receivedAt) >= RequestLifetime.seconds
    }

    /// The time the sessions are from, when that is more than five minutes ago.
    var asOf: Date? { asOf(at: deps.clock()) }

    /// The phone has sent at least one machine's pairing.
    var isPaired: Bool { !deps.pairings.all().isEmpty }

    func asOf(at now: Date) -> Date? {
        guard let sessionsAsOf, now.timeIntervalSince(sessionsAsOf) > Self.freshContext else { return nil }
        return sessionsAsOf
    }

    /// Approve is offered only when the agent's options include one the helper can pick as
    /// approve. Anything else, such as "always allow", needs the app.
    static func canApprove(_ request: PendingRequest) -> Bool {
        request.options.contains { $0.role == "approve" }
    }

    /// Reads the store again, after another process may have changed it.
    func reload() {
        let now = deps.clock()
        requests = deps.pending.all()
            .filter {
                ($0.answered == nil || $0.answered == .sending)
                    && now.timeIntervalSince($0.receivedAt) < RequestLifetime.seconds
            }
            .sorted { $0.receivedAt < $1.receivedAt }
    }

    /// A push the Watch opened. Only requests and their clears change anything here.
    func ingest(_ push: PushPayload, hostId: String) {
        guard let requestId = push.requestId else { return }
        switch push.kind {
        case .needsYou:
            let age = deps.clock().timeIntervalSince1970 - Double(push.ts) / 1000
            guard age <= NotificationServiceCore.maxAge, age >= -NotificationServiceCore.maxSkew else { return }
            deps.pending.upsert(PendingRequest(
                requestId: requestId, hostId: hostId, sessionId: push.sessionId, bib: push.bib, agent: push.agent,
                host: push.host, title: PromptText.clean(push.title, maxLines: 2), subject: push.subject,
                // From when the machine sent it: watchOS can hold a silent push for minutes.
                options: push.options ?? [],
                receivedAt: min(deps.clock(), Date(timeIntervalSince1970: Double(push.ts) / 1000)), answered: nil,
                allowText: push.allowText == true ? true : nil
            ))
        case .clear:
            close(requestId)
        case .done, .job, .notify, .ended, .failed, .offline:
            return
        }
        changed()
    }

    /// The phone's latest context. One older than the context already applied is ignored.
    /// Only the requests it names as closed are closed here.
    func apply(context: WatchContext) {
        if let sessionsAsOf, context.at < sessionsAsOf { return }
        sessions = context.sessions
        sessionsAsOf = context.at
        textRequestIds = Set(context.textRequestIds ?? [])
        for requestId in context.closedRequestIds { close(requestId) }
        changed()
    }

    /// Reply is offered on a request the phone named as taking text, or whose own push said so,
    /// while it is still open.
    func canReply(_ requestId: String) -> Bool {
        let takesText = textRequestIds.contains(requestId) || deps.pending.request(requestId)?.allowText == true
        return takesText && state(of: requestId).isActionable
    }

    /// A Reply typed on a notification shown here. Only these words are sent; a draft in the
    /// target's reply screen is never sent with them and never changed by them. When they do
    /// not go they become the draft, unless there is one already. Returns the notice to show,
    /// or nil when they went. The id comes from the notification and the words, so the same
    /// reply delivered twice is typed once.
    func replyFromNotification(target: WatchReplyMessage.Target, text: String, notification: String) async -> String? {
        let digest = SHA256.hash(data: Data("\(notification)\n\(text)".utf8))
        let id = digest.prefix(16).map { String(format: "%02x", $0) }.joined()
        let notice: String? = switch await sendReply(WatchReplyMessage(id: id, target: target, text: text)) {
        case .sent: nil
        case .needsPhone: WatchCopy.needsPhone
        case .refused(let sentence), .failed(let sentence): sentence
        }
        if let notice {
            let model = replyModel(target)
            if model.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { model.keep(text, problem: notice) }
        }
        return notice
    }

    /// Run again tapped on a phone notification shown on the Watch. The phone starts the
    /// session; nil when it did, else the notice to show. The id comes from the notification,
    /// so a second delivery of the same tap starts nothing twice.
    func runAgainFromNotification(hostId: String, sessionId: String, notification: String) async -> String? {
        let digest = SHA256.hash(data: Data("runAgain\n\(notification)".utf8))
        let id = digest.prefix(16).map { String(format: "%02x", $0) }.joined()
        return switch await sendRunAgain(WatchRunAgainMessage(id: id, machineId: hostId, sessionId: sessionId)) {
        case .sent: nil
        case .needsPhone: WatchCopy.runAgainNeedsPhone
        case .refused(let sentence), .failed(let sentence): sentence
        }
    }

    /// The reply screen's model for a request or session, the same one each time.
    func replyModel(_ target: WatchReplyMessage.Target) -> WatchReplyModel {
        if let model = replies[target] { return model }
        let model = WatchReplyModel(target: target) { [weak self] message in
            await self?.sendReply(message) ?? .needsPhone
        }
        replies[target] = model
        return model
    }

    /// Approve or deny from the Watch's own screens. Approve needs an approve option.
    func answer(_ requestId: String, _ decision: String) async {
        guard let request = deps.pending.request(requestId), state(of: requestId).isActionable else {
            revision += 1
            return
        }
        if decision == "approve", !Self.canApprove(request) { return }
        await send(hostId: request.hostId, requestId: requestId, decision: decision)
    }

    /// Approve or deny from a phone notification shown on the Watch. The notification's
    /// category already offered only the actions its options allow. Returns the notice to show
    /// when the answer did not go, or nil when it went or another answer is on its way.
    /// `deliveredAt` is the notification's date: a request this Watch no longer holds, pruned
    /// after its ten minutes or never pushed here, is refused once its notice is that old.
    @discardableResult
    func answerFromNotification(
        hostId: String, requestId: String, decision: String, deliveredAt: Date? = nil
    ) async -> String? {
        if deps.pending.request(requestId) == nil, let deliveredAt,
           deps.clock().timeIntervalSince(deliveredAt) >= RequestLifetime.seconds {
            return WatchCopy.expired
        }
        await send(hostId: hostId, requestId: requestId, decision: decision)
        if let notice = failureNotice(requestId) { return notice }
        // Nothing went: the desk or the phone answered first, or the window closed. Say so
        // rather than leave the tap looking like it worked.
        switch state(of: requestId) {
        case .answeredElsewhere: return WatchCopy.answeredElsewhere
        case .expired: return WatchCopy.expired
        default: return nil
        }
    }

    /// Sends one answer through the relay. The shared store's claim makes it the only one,
    /// across the app and a notification action alike; a failed send gives the claim back.
    private func send(hostId: String, requestId: String, decision: String) async {
        // A request this Watch knows is refused once it is past its ten minutes: the helper
        // would drop the answer, and the screen must not say it went through.
        if let known = deps.pending.request(requestId), known.answered == nil,
           Self.expired(known, at: deps.clock()) {
            // An earlier failure no longer applies: the request ran out, which is what to say.
            outcomes[requestId] = nil
            return
        }
        guard decision == "approve" || decision == "deny" else { return }
        guard let pairing = deps.pairings.pairing(hostId: hostId) else {
            outcomes[requestId] = .notSetUp
            revision += 1
            return
        }
        guard deps.pending.claim(requestId, hostId: hostId) else { return }
        outcomes[requestId] = .sending
        do {
            try await deps.sender.send(requestId: requestId, decision: decision, pairing: pairing)
        } catch {
            if let claimed = deps.pending.request(requestId), claimed.answered == .sending { giveBack(claimed) }
            outcomes[requestId] = .failed
            reload()
            return
        }
        deps.pending.mark(requestId, decision == "approve" ? .approved : .denied)
        outcomes[requestId] = decision == "approve" ? .approved : .denied
        changed()
    }

    /// Marks a request answered elsewhere, or keeps a clear for one not seen yet, so its push
    /// arriving late cannot open it.
    private func close(_ requestId: String) {
        switch deps.pending.request(requestId)?.answered {
        case .approved, .denied, .elsewhere:
            return
        case nil, .sending:
            _ = deps.pending.claim(requestId, hostId: "")
            deps.pending.mark(requestId, .elsewhere)
        }
    }

    private func changed() {
        deps.pending.prune(olderThan: RequestLifetime.seconds)
        reload()
        deps.reloadComplication()
    }
}
