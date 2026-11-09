import AppIntents
import Foundation
import TogoPush

/// Answers a request from the Live Activity's buttons, the same one-tap way a notification
/// does: approve or deny, signed with this device's key and sealed through the machine's relay
/// mailbox. The pending store claims the request first, so a second tap, or the notification's
/// own button, sends nothing more. Anything else is answered in the app.
enum LiveAnswer {
    enum Failure: Error {
        case unavailable
    }

    static func send(_ decision: String, hostId: String, requestId: String) async throws {
        guard let directory = SharedGroup.storeDirectory(SharedGroup.ios),
              let pairing = PairingStore(directory: directory).pairing(hostId: hostId)
        else { throw Failure.unavailable }
        let pending = PendingStore(directory: directory, clock: { Date() })
        guard pending.claim(requestId, hostId: hostId) else { return }
        #if DEBUG
        // The app's recorded demo (`-ui-demo-live`) has no relay: it reads the answer from the store.
        if SharedSettings.defaults?.bool(forKey: "ui.demo.liveAnswers") == true {
            pending.mark(requestId, decision == "deny" ? .denied : .approved)
            return
        }
        #endif
        let sender = RelayAnswerSender(keys: KeychainDeviceKeys(accessGroup: nil), pending: pending, clock: { Date() })
        do {
            try await sender.send(requestId: requestId, decision: decision, pairing: pairing)
        } catch {
            // Nothing reached the machine: the request can be answered again.
            pending.release(requestId)
            throw error
        }
        pending.mark(requestId, decision == "deny" ? .denied : .approved)
    }
}

/// Approve, for when approving needs the device unlocked (the default).
struct ApproveRequestIntent: AppIntent {
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
struct ApproveUnlockedRequestIntent: AppIntent {
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
        try await LiveAnswer.send("approve", hostId: hostId, requestId: requestId)
        return .result()
    }
}

/// Deny only stops the agent, so it never needs the device unlocked.
struct DenyRequestIntent: AppIntent {
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
