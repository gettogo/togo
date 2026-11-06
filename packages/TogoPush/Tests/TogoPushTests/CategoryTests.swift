import CryptoKit
import Foundation
import Testing
import UserNotifications
@testable import TogoPush

/// Which notifications offer Reply, and what it asks of the user.
@Suite(.serialized)
struct CategoryTests {
    static let now = Date(timeIntervalSince1970: 1_793_707_200)
    static let hostID = "h_mmg42klgyqzwneiskrelxms3j7"

    let keys = InMemoryDeviceKeys()
    let directory = FileManager.default.temporaryDirectory.appending(path: "togo-category-tests-\(UUID().uuidString)")

    var core: NotificationServiceCore {
        NotificationServiceCore(
            keys: keys, pairings: PairingStore(directory: directory),
            pending: PendingStore(directory: directory, clock: { Self.now }), clock: { Self.now }, doneSilent: { false }
        )
    }

    init() {
        PairingStore(directory: directory).put(Pairing(
            hostId: Self.hostID, hostKey: Base64URL.encode(Curve25519.KeyAgreement.PrivateKey().publicKey.rawRepresentation),
            relayURL: URL(string: "https://relay.example")!, machineName: "studio", appHostID: nil, pairedAt: Self.now,
            tokenHash: "a1b2c3", liveActivityCommand: false
        ))
    }

    func sealed(_ payload: PushPayload) throws -> UNNotificationContent {
        let envelope = try Seal.seal(
            to: keys.agreementKey().publicKey, JSONEncoder().encode(payload), label: Labels.push,
            aad: Data("\(Self.hostID)|\(try keys.deviceID())".utf8)
        )
        let content = UNMutableNotificationContent()
        content.userInfo = ["aps": ["mutable-content": 1], "c": envelope]
        return content
    }

    func push(
        _ kind: PushPayload.Kind, reason: String? = nil, code: Int? = nil, allowText: Bool? = nil,
        options: [PushOption] = [PushOption(n: 1, label: "Yes", role: "approve"), PushOption(n: 2, label: "No", role: "deny")]
    ) -> PushPayload {
        var p = PushPayload(
            kind: kind, sessionId: "s_abcdefghij", bib: "01", agent: "claude", host: "studio", title: "t", body: "b",
            requestId: kind == .needsYou ? "r_aaaaaaaaaa" : nil,
            options: kind == .needsYou ? options : nil,
            ts: 1_793_707_200_000, exit: reason.map { PushExit(code: code, reason: $0, detail: nil) }
        )
        p.allowText = allowText
        return p
    }

    func actions(_ category: String) throws -> [String] {
        try #require(NotificationCategorySet.make(approveWithoutUnlock: false).first { $0.identifier == category })
            .actions.map(\.identifier)
    }

    func category(_ payload: PushPayload) throws -> String {
        try #require(NotificationTextBuilder.build(payload, hostId: Self.hostID, hostName: "studio")).category
    }

    /// Reply goes only where the agent waits for words: a done, a stop on a usage limit (the
    /// helper reports it as failed, and the agent is still alive), and a request whose push says
    /// it takes a typed answer. Every other stop runs again; other requests keep their buttons.
    @Test func replyActionOnlyWhereTextAllowed() throws {
        #expect(try actions("togo.prompt") == ["togo.reply"])
        for other in ["togo.request", "togo.request.deny", "togo.info", "togo.ended"] {
            #expect(try !actions(other).contains("togo.reply"), "\(other)")
        }
        #expect(try actions("togo.ended") == ["togo.run-again"])
        #expect(try actions("togo.request.reply") == ["togo.approve", "togo.deny", "togo.reply"])
        #expect(try actions("togo.request.deny.reply") == ["togo.deny", "togo.reply"])
        #expect(try actions("togo.request.text") == ["togo.reply", "togo.open"])

        #expect(try category(push(.needsYou, allowText: true)) == "togo.request.reply")
        #expect(try category(push(.needsYou, allowText: false)) == "togo.request")
        #expect(try category(push(.needsYou)) == "togo.request", "older helpers say nothing")
        let denyOnly = [PushOption(n: 1, label: "No", role: "deny")]
        #expect(try category(push(.needsYou, allowText: true, options: denyOnly)) == "togo.request.deny.reply")
        #expect(try category(push(.needsYou, allowText: true, options: [])) == "togo.request.text")
        #expect(try category(push(.needsYou, options: [])) == "togo.info")

        // The answer names its request; the reply keeps every id the handler needs.
        let request = core.process(try sealed(push(.needsYou, allowText: true))).content
        #expect(request.categoryIdentifier == "togo.request.reply")
        let info = try #require(request.userInfo["togo"] as? [String: Any])
        #expect(info["requestId"] as? String == "r_aaaaaaaaaa")
        #expect(info["hostId"] as? String == Self.hostID)
        #expect(info["sessionId"] as? String == "s_abcdefghij")

        #expect(try category(push(.done)) == "togo.prompt")
        #expect(try category(push(.failed, reason: "usage_limit")) == "togo.prompt")
        #expect(try category(push(.ended, reason: "usage_limit")) == "togo.info", "the helper refuses an ended agent")
        #expect(try category(push(.failed, reason: "exit", code: 1)) == "togo.ended")
        #expect(try category(push(.ended, reason: "interrupted", code: 130)) == "togo.ended")
        #expect(try category(push(.needsYou)) == "togo.request")
        #expect(try category(push(.job)) == "togo.info")
        #expect(try category(push(.notify)) == "togo.info")

        // The reply names its session by ids only; no text of the push rides along.
        for payload in [push(.done), push(.failed, reason: "usage_limit")] {
            let content = core.process(try sealed(payload)).content
            #expect(content.categoryIdentifier == "togo.prompt")
            #expect(content.userInfo["togo"] as? [String: String] == ["hostId": Self.hostID, "sessionId": "s_abcdefghij"])
        }
        var sessionless = push(.done)
        sessionless.sessionId = nil
        #expect(core.process(try sealed(sessionless)).content.categoryIdentifier == "togo.info", "nothing to reply to")
    }

    /// An error stop with no exit code leaves the agent alive at its prompt, as a usage limit
    /// does: the helper takes words for it and refuses to start it again.
    @Test func errorStopOffersReplyNotRunAgain() throws {
        #expect(try category(push(.failed, reason: "error")) == "togo.prompt")
        #expect(LifecycleWords.agentWaiting(status: "failed", reason: "error", code: nil))
        #expect(LifecycleWords.agentWaiting(status: "failed", reason: "usage_limit", code: nil))
        #expect(!LifecycleWords.agentWaiting(status: "failed", reason: "error", code: 1), "the process is gone")
        #expect(!LifecycleWords.agentWaiting(status: "ended", reason: "error", code: nil))
        #expect(!LifecycleWords.agentWaiting(status: "failed", reason: "exit", code: nil))
        #expect(!LifecycleWords.agentWaiting(status: "failed", reason: nil, code: nil))
        #expect(!LifecycleWords.canRunAgain(status: "failed", reason: "error", code: nil))
        let content = core.process(try sealed(push(.failed, reason: "error"))).content
        #expect(content.categoryIdentifier == "togo.prompt")
        #expect(content.userInfo["togo"] as? [String: String] == ["hostId": Self.hostID, "sessionId": "s_abcdefghij"])
    }

    /// Once the wrapper reports the code, the agent is gone and runs again.
    @Test func exitedErrorWithCodeRunsAgain() throws {
        #expect(try category(push(.failed, reason: "error", code: 1)) == "togo.ended")
        #expect(try category(push(.failed, reason: "exit", code: 2)) == "togo.ended")
        #expect(LifecycleWords.canRunAgain(status: "failed", reason: "error", code: 1))
        #expect(LifecycleWords.canRunAgain(status: "ended", reason: "interrupted", code: 130))
        #expect(!LifecycleWords.canRunAgain(status: "working", reason: nil, code: nil))
    }

    /// Run again needs the session it starts; a stop without one offers nothing.
    @Test func stopWithoutSessionOffersNoRunAgain() throws {
        var sessionless = push(.failed, reason: "exit", code: 1)
        sessionless.sessionId = nil
        #expect(try category(sessionless) == "togo.info")
        sessionless.sessionId = ""
        #expect(try category(sessionless) == "togo.info")
    }

    /// Reply types into an agent, so it needs the device unlocked and opens the app to say
    /// whether it went.
    @Test func replyForegroundAndAuth() throws {
        for without in [false, true] {
            let prompt = try #require(NotificationCategorySet.make(approveWithoutUnlock: without)
                .first { $0.identifier == "togo.prompt" })
            let reply = try #require(prompt.actions.first as? UNTextInputNotificationAction)
            #expect(reply.identifier == NotificationActionID.reply)
            #expect(reply.title == "Reply")
            #expect(reply.textInputButtonTitle == "Send")
            #expect(reply.textInputPlaceholder == "Message")
            #expect(reply.options == [.foreground, .authenticationRequired])
        }
    }
}
