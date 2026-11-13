import ActivityKit
import AppIntents
import Foundation
import TogoPush
import WidgetKit

/// Answers a request from the Live Activity's buttons, the same one-tap way a notification
/// does: approve or deny, signed with this device's key and sealed through the machine's relay
/// mailbox. The pending store claims the request first, so a second tap, or the notification's
/// own button, sends nothing more. Anything else is answered in the app.
enum LiveAnswer {
    enum Failure: Error {
        case unavailable
        /// Approving without unlocking was turned off after the card was drawn.
        case needsUnlock
    }

    static func send(_ decision: String, hostId: String, requestId: String) async throws {
        guard let directory = SharedGroup.storeDirectory(SharedGroup.ios),
              let pairing = PairingStore(directory: directory).pairing(hostId: hostId)
        else {
            await refresh(.failed, hostId: hostId, requestId: requestId)
            throw Failure.unavailable
        }
        // The waiting widget counts open requests: an answer here takes one off at once,
        // rather than when the widget's next entry is due, up to ten minutes later.
        let pending = PendingStore(directory: directory, clock: { Date() }) {
            WidgetCenter.shared.reloadTimelines(ofKind: WidgetKinds.waiting)
        }
        let sender = RelayAnswerSender(keys: KeychainDeviceKeys(accessGroup: nil), pending: pending, clock: { Date() })
        try await perform(decision, hostId: hostId, requestId: requestId, pending: pending) {
            #if DEBUG
            if SharedSettings.defaults?.bool(forKey: "ui.demo.liveAnswers") == true { return }
            #endif
            try await sender.send(requestId: requestId, decision: decision, pairing: pairing)
        } refresh: { feedback in
            await refresh(feedback, hostId: hostId, requestId: requestId)
        }
    }

    static func perform(
        _ decision: String, hostId: String, requestId: String, pending: PendingStore,
        send: @Sendable () async throws -> Void,
        refresh: @Sendable (LiveAnswerFeedback) async -> Void
    ) async throws {
        if let request = pending.request(requestId), request.hostId != hostId { throw Failure.unavailable }
        guard pending.claim(requestId, hostId: hostId) else {
            if let feedback = LiveAnswerFeedback.resolve(pending.request(requestId), local: nil, now: Date()) {
                await refresh(feedback)
            }
            return
        }
        await refresh(.sending)
        do {
            try await send()
        } catch {
            pending.release(requestId)
            await refresh(.failed)
            throw error
        }
        pending.mark(requestId, decision == "deny" ? .denied : .approved)
        await refresh(.sent)
    }

    /// Mutate only the activity still showing this request. A late result must not replace
    /// another request the helper has already moved to. The delivery field forces a render
    /// even though the helper's status and timer have not changed.
    private static func refresh(_ feedback: LiveAnswerFeedback, hostId: String, requestId: String) async {
        for activity in Activity<TogoActivityAttributes>.activities where activity.attributes.hostId == hostId {
            let content = activity.content
            var state = content.state
            guard state.status == "needs_you", state.requestId == requestId else { continue }
            state.answerFeedback = feedback
            await activity.update(ActivityContent(
                state: state, staleDate: content.staleDate, relevanceScore: content.relevanceScore
            ))
        }
    }
}

/// Approve, for when approving needs the device unlocked (the default).
struct ApproveRequestIntent: LiveActivityIntent {
    static let title = LocalizedStringResource("live.intent.approve", defaultValue: "Approve")
    static let isDiscoverable = false
    static let authenticationPolicy: IntentAuthenticationPolicy = .requiresAuthentication

    @Parameter(title: LocalizedStringResource("live.intent.machine", defaultValue: "Machine")) var hostId: String
    @Parameter(title: LocalizedStringResource("live.intent.request", defaultValue: "Request")) var requestId: String

    init() {}

    init(hostId: String, requestId: String) {
        self.hostId = hostId
        self.requestId = requestId
    }

    func perform() async throws -> some IntentResult {
        try await LiveAnswer.send("approve", hostId: hostId, requestId: requestId)
        return .result()
    }
}

/// Approve, for a user who turned on approving without unlocking.
struct ApproveUnlockedRequestIntent: LiveActivityIntent {
    static let title = LocalizedStringResource("live.intent.approve", defaultValue: "Approve")
    static let isDiscoverable = false
    static let authenticationPolicy: IntentAuthenticationPolicy = .alwaysAllowed

    @Parameter(title: LocalizedStringResource("live.intent.machine", defaultValue: "Machine")) var hostId: String
    @Parameter(title: LocalizedStringResource("live.intent.request", defaultValue: "Request")) var requestId: String

    init() {}

    init(hostId: String, requestId: String) {
        self.hostId = hostId
        self.requestId = requestId
    }

    func perform() async throws -> some IntentResult {
        // A card drawn before the setting was turned off still carries this intent: the
        // setting as it is now decides, so nothing is approved without unlocking against it.
        guard SharedSettings.defaults?.bool(forKey: SharedSettings.approveWithoutUnlock) == true else {
            throw LiveAnswer.Failure.needsUnlock
        }
        try await LiveAnswer.send("approve", hostId: hostId, requestId: requestId)
        return .result()
    }
}

/// Deny only stops the agent, so it never needs the device unlocked.
struct DenyRequestIntent: LiveActivityIntent {
    static let title = LocalizedStringResource("live.intent.deny", defaultValue: "Deny")
    static let isDiscoverable = false
    static let authenticationPolicy: IntentAuthenticationPolicy = .alwaysAllowed

    @Parameter(title: LocalizedStringResource("live.intent.machine", defaultValue: "Machine")) var hostId: String
    @Parameter(title: LocalizedStringResource("live.intent.request", defaultValue: "Request")) var requestId: String

    init() {}

    init(hostId: String, requestId: String) {
        self.hostId = hostId
        self.requestId = requestId
    }

    func perform() async throws -> some IntentResult {
        try await LiveAnswer.send("deny", hostId: hostId, requestId: requestId)
        return .result()
    }
}
