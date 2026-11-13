import CryptoKit
import Foundation
import Synchronization
import Testing
import TogoKit
import TogoPush
import UserNotifications
@testable import Togo

private let hostID = "h_mmg42klgyqzwneiskrelxms3j7"
private let requestID = "r_aaaaaaaaaa"
private let start = Date(timeIntervalSince1970: 1_793_707_200)

final class FakeRelay: AnswerSending {
    let sent = Mutex<[String]>([])
    let error = Mutex<(any Error)?>(nil)

    func send(requestId: String, decision: String, pairing: Pairing) async throws {
        if let error = error.withLock({ $0 }) { throw error }
        sent.withLock { $0.append("\(pairing.hostId) \(requestId) \(decision)") }
    }
}

@MainActor final class FakeLive: LiveAnswering {
    var live = true
    var ready = true
    var error: HelperError?
    var sent: [String] = []

    func answerIfConnected(_ pairing: Pairing, requestId: String, decision: Decision) async throws -> Bool {
        guard live, ready else { return false }
        if let error { throw error }
        sent.append("\(requestId) \(decision.wire)")
        return true
    }
}

@MainActor final class FakeNotifications: DeliveredNotifications {
    var shown: [DeliveredRequest] = []
    var removed: [Set<String>] = []
    var failures: [String] = []
    var offlineRemoved: [String] = []

    func removeOffline(hostId: String) async { offlineRemoved.append(hostId) }
    func delivered() async -> [DeliveredRequest] { shown }
    func removeDelivered(requestIds: Set<String>) async {
        removed.append(requestIds)
        shown.removeAll { requestIds.contains($0.requestId) }
    }
    func postSendFailure(threadID: String) async { failures.append(threadID) }
}

/// Captures what the relay sender posts and answers 202.
private final class MailboxStub: URLProtocol, @unchecked Sendable {
    static let bodies = Mutex<[Data]>([])

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        var body = request.httpBody ?? Data()
        if body.isEmpty, let stream = request.httpBodyStream {
            stream.open()
            var buffer = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable {
                let n = stream.read(&buffer, maxLength: buffer.count)
                if n <= 0 { break }
                body.append(buffer, count: n)
            }
            stream.close()
        }
        Self.bodies.withLock { $0.append(body) }
        let response = HTTPURLResponse(url: request.url!, statusCode: 202, httpVersion: nil, headerFields: nil)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data())
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@MainActor @Suite(.serialized)
struct NotificationActionHandlerTests {
    let directory = FileManager.default.temporaryDirectory.appending(path: "togo-actions-\(UUID().uuidString)")
    let hostKey = Curve25519.KeyAgreement.PrivateKey()
    let relay = FakeRelay()
    let live = FakeLive()
    let notifications = FakeNotifications()

    var pairings: PairingStore { PairingStore(directory: directory) }
    var pending: PendingStore { PendingStore(directory: directory, clock: { start }) }

    init() {
        pairings.put(Pairing(
            hostId: hostID, hostKey: Base64URL.encode(hostKey.publicKey.rawRepresentation),
            relayURL: URL(string: "https://relay.example")!, machineName: "studio", appHostID: nil, pairedAt: start,
            tokenHash: "a1b2c3", liveActivityCommand: false
        ))
        pending.upsert(PendingRequest(
            requestId: requestID, hostId: hostID, sessionId: "s_abcdefghij", bib: "04", agent: "claude", host: "studio",
            title: "Do you want to proceed?", subject: PushSubject(kind: "command", text: "make"),
            options: [
                PushOption(n: 1, label: "Yes", role: "approve"),
                PushOption(n: 2, label: "Yes, always", role: "approve_always"),
                PushOption(n: 3, label: "No", role: "deny"),
            ],
            receivedAt: start, answered: nil
        ))
        notifications.shown = [DeliveredRequest(requestId: requestID, hostId: hostID)]
    }

    /// A new handler with its own store instance, as after a relaunch or in another process.
    func handler(relay: (any AnswerSending)? = nil, live: Bool = false) -> NotificationActionHandler {
        NotificationActionHandler(dependencies: .init(
            pairings: pairings, pending: pending, relay: relay ?? self.relay, live: live ? self.live : nil,
            notifications: notifications
        ))
    }

    var userInfo: [AnyHashable: Any] {
        ["togo": ["requestId": requestID, "hostId": hostID, "sessionId": "s_abcdefghij", "options": []]]
    }

    /// A stopped push sealed by the helper, opened by the notification service as on a device,
    /// then the Run again action: the app opens the session and asks that machine to relaunch it.
    @Test func runAgainActionOpensAndRelaunches() async throws {
        let keys = InMemoryDeviceKeys()
        pairings.put(Pairing(
            hostId: hostID, hostKey: Base64URL.encode(hostKey.publicKey.rawRepresentation),
            relayURL: URL(string: "https://relay.example")!, machineName: "studio", appHostID: Samples.studio,
            pairedAt: start, tokenHash: "a1b2c3", liveActivityCommand: false
        ))
        let payload = PushPayload(
            kind: .failed, sessionId: "s_abcdefghij", bib: "01", agent: "claude", host: "studio",
            title: "Claude Code stopped", body: "Exit code 1", ts: Int64(start.timeIntervalSince1970 * 1000),
            exit: PushExit(code: 1, reason: "exit", detail: nil)
        )
        let envelope = try Seal.seal(
            to: keys.agreementKey().publicKey, JSONEncoder().encode(payload), label: Labels.push,
            aad: Data("\(hostID)|\(try keys.deviceID())".utf8)
        )
        let delivered = UNMutableNotificationContent()
        delivered.userInfo = ["aps": ["mutable-content": 1], "c": envelope]
        let shown = NotificationServiceCore(keys: keys, pairings: pairings, pending: pending, clock: { start })
            .process(delivered).content
        #expect(shown.categoryIdentifier == NotificationCategoryID.ended)

        let store = AppStore(services: FakeAppServices())
        let tools = FakeMachineTools()
        tools.defaultCapabilities = [Capability.launch]
        tools.answer("session.relaunch", """
            {"id":"s_abcdefghij","bib":"01","name":"Claude Code","agent":"claude","cwd":"/Users/sam/src/app",\
            "createdAt":"2026-11-03T12:00:00Z","status":"working","activity":null,"attachedClients":0,\
            "managed":true,"muted":false,"meta":{"model":null,"contextPercent":null,"costUsd":null},\
            "command":["claude"],"exit":null}
            """)
        var running: Task<Void, Never>?
        let h = NotificationActionHandler(dependencies: .init(
            pairings: pairings, pending: pending, relay: relay, live: nil, notifications: notifications,
            runAgain: { host, session in running = Task { await store.runAgain(hostID: host, sessionID: session, tools: tools) } }
        ))
        let link = await h.handle(
            actionIdentifier: NotificationActionID.runAgain, userInfo: shown.userInfo, target: shown.targetContentIdentifier
        )
        #expect(link == .session(bib: "01", host: "studio"))
        await running?.value
        #expect(tools.calledHosts == [Samples.studio])
        #expect(tools.requests == [FakeMachineTools.Request(method: "session.relaunch", params: ["id": .string("s_abcdefghij")])])
        #expect(relay.sent.withLock { $0 }.isEmpty)
        #expect(store.notice == nil)
    }

    @Test func approveUsesSSHWhenLive() async {
        let link = await handler(live: true).handle(actionIdentifier: "togo.approve", userInfo: userInfo, target: nil)
        #expect(link == nil)
        #expect(live.sent == ["\(requestID) approve"])
        #expect(relay.sent.withLock { $0 }.isEmpty)
        #expect(pending.request(requestID)?.answered == .approved)
        #expect(notifications.removed == [[requestID]])
    }

    @Test func retiredTransportUsesRelayDespiteCachedOnline() async {
        live.ready = false
        let h = handler(live: true)
        #expect(await h.answer(hostId: hostID, requestId: requestID, decision: "approve") == .sent)
        #expect(relay.sent.withLock { $0 } == ["\(hostID) \(requestID) approve"])
        #expect(live.sent.isEmpty)
    }

    @Test func uncertainLiveFailureDoesNotAlsoSendToRelay() async {
        live.error = .timeout(method: "approval.answer")
        let h = handler(live: true)
        #expect(await h.answer(hostId: hostID, requestId: requestID, decision: "approve") == .failed)
        #expect(relay.sent.withLock { $0 }.isEmpty)
        #expect(notifications.failures.count == 1)
    }

    @Test func approveUsesRelayWhenNot() async {
        live.live = false
        let h = NotificationActionHandler(dependencies: .init(
            pairings: pairings, pending: pending, relay: relay, live: live, notifications: notifications
        ))
        await h.handle(actionIdentifier: "togo.approve", userInfo: userInfo, target: nil)
        #expect(relay.sent.withLock { $0 } == ["\(hostID) \(requestID) approve"])
        #expect(live.sent.isEmpty)
        #expect(pending.request(requestID)?.answered == .approved)
        #expect(notifications.shown.isEmpty)
    }

    @Test func denyUsesRelayWhenNotLive() async {
        await handler().handle(actionIdentifier: "togo.deny", userInfo: userInfo, target: nil)
        #expect(relay.sent.withLock { $0 } == ["\(hostID) \(requestID) deny"])
        #expect(pending.request(requestID)?.answered == .denied)
    }

    @Test func approveTwiceSendsOnce() async {
        let h = handler()
        #expect(await h.answer(hostId: hostID, requestId: requestID, decision: "approve") == .sent)
        #expect(await h.answer(hostId: hostID, requestId: requestID, decision: "approve") == .repeated)
        #expect(await h.answer(hostId: hostID, requestId: requestID, decision: "deny") == .repeated)
        #expect(await handler().answer(hostId: hostID, requestId: requestID, decision: "approve") == .repeated,
                "a relaunch, minutes later, still sends nothing")
        #expect(relay.sent.withLock { $0 }.count == 1)
        #expect(notifications.failures.isEmpty)
    }

    /// Two handlers on one store directory stand in for the app and another process tapping
    /// the same request at once.
    @Test func tapsInTwoProcessesSendOnce() async {
        let first = handler()
        let second = handler()
        async let a = first.answer(hostId: hostID, requestId: requestID, decision: "approve")
        async let b = second.answer(hostId: hostID, requestId: requestID, decision: "approve")
        let outcomes = await [a, b]
        #expect(outcomes.filter { $0 == .sent }.count == 1)
        #expect(outcomes.filter { $0 == .repeated }.count == 1)
        #expect(relay.sent.withLock { $0 }.count == 1)
    }

    @Test func postFailurePostsLocalNotice() async {
        relay.error.withLock { $0 = AnswerError.relay(503) }
        let h = handler()
        await h.handle(actionIdentifier: "togo.approve", userInfo: userInfo, target: nil)
        #expect(notifications.failures == ["s_abcdefghij"])
        #expect(pending.request(requestID)?.answered == nil)
        #expect(notifications.shown.count == 1, "the request stays on the lock screen")
        relay.error.withLock { $0 = nil }
        #expect(await h.answer(hostId: hostID, requestId: requestID, decision: "approve") == .sent, "a retry sends")
    }

    @Test func unknownMachinePostsLocalNotice() async {
        #expect(await handler().answer(hostId: "h_bbbbbbbbbbbbbbbbbbbbbbbbbb", requestId: requestID, decision: "approve")
            == .failed)
        #expect(notifications.failures == ["h_bbbbbbbbbbbbbbbbbbbbbbbbbb"])
    }

    @Test func openReturnsSessionLink() async {
        let link = await handler().handle(
            actionIdentifier: "togo.open", userInfo: userInfo, target: "togo://session/04?host=studio"
        )
        #expect(link == .session(bib: "04", host: "studio"))
        #expect(relay.sent.withLock { $0 }.isEmpty)
    }

    /// The real relay sender: the posted envelope opens with the host's key and carries an
    /// answer signed by this device over the approval bytes.
    @Test func relayAnswerIsSignedAndSealed() async throws {
        MailboxStub.bodies.withLock { $0 = [] }
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [MailboxStub.self]
        let keys = InMemoryDeviceKeys()
        let sender = RelayAnswerSender(
            keys: keys, session: URLSession(configuration: config), pending: pending, clock: { start }
        )
        #expect(await handler(relay: sender).answer(hostId: hostID, requestId: requestID, decision: "approve") == .sent)

        let body = try #require(MailboxStub.bodies.withLock { $0.first })
        let envelope = try #require(try JSONDecoder().decode([String: String].self, from: body)["ciphertext"])
        let plaintext = try Seal.open(envelope, with: hostKey, label: Labels.answer, aad: Data(hostID.utf8))
        let answer = try JSONDecoder().decode(AnswerPlaintext.self, from: plaintext)
        #expect(answer.requestId == requestID)
        #expect(answer.decision == "approve")
        #expect(answer.deviceId == (try keys.deviceID()))
        let ts = Int64(start.timeIntervalSince1970 * 1000)
        #expect(answer.ts == ts)
        let message = Signing.approvalBytes(hostID: hostID, requestID: requestID, decision: "approve", ts: ts)
        let sig = try #require(Base64URL.decode(answer.sig))
        #expect(try keys.signingKey().publicKey.isValidSignature(sig, for: message))
    }

    @Test func optionAnswersSendButNeverApproveAlways() async throws {
        MailboxStub.bodies.withLock { $0 = [] }
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [MailboxStub.self]
        let sender = RelayAnswerSender(
            keys: InMemoryDeviceKeys(), session: URLSession(configuration: config), pending: pending, clock: { start }
        )
        let h = handler(relay: sender)
        #expect(await h.answer(hostId: hostID, requestId: requestID, decision: "option:2") == .failed)
        #expect(MailboxStub.bodies.withLock { $0 }.isEmpty, "approve_always never leaves the device")
        #expect(notifications.failures.count == 1)
        #expect(await h.answer(hostId: hostID, requestId: requestID, decision: "option:9") == .failed)
        #expect(await h.answer(hostId: hostID, requestId: requestID, decision: "option:1") == .sent)
        #expect(MailboxStub.bodies.withLock { $0 }.count == 1)
        let viaSSH = handler(live: true)
        #expect(await viaSSH.answer(hostId: hostID, requestId: requestID, decision: "option:2") == .failed)
        #expect(await viaSSH.answer(hostId: hostID, requestId: requestID, decision: "option:1") == .repeated)
        #expect(live.sent.isEmpty)
    }

    @Test func backgroundReceiptFollowsSharedStoreWrite() async throws {
        let keys = InMemoryDeviceKeys()
        let id = "r_bbbbbbbbbb"
        var savedBeforeReceipt = false
        let clear = ClearHandler(dependencies: .init(
            core: NotificationServiceCore(keys: keys, pairings: pairings, pending: pending, clock: { start }),
            pending: pending, notifications: notifications, reloadWidgets: {},
            received: { host, receivedID in
                let saved = pending.request(receivedID)
                savedBeforeReceipt = host == hostID && receivedID == id && saved?.title == "Approve?"
                    && saved?.options.map(\.role) == ["approve", "deny"] && saved?.answered == nil
            }
        ))
        let payload = PushPayload(kind: .needsYou, title: "Approve?", body: "", requestId: id,
                                  options: [PushOption(n: 1, label: "Yes", role: "approve"),
                                            PushOption(n: 2, label: "No", role: "deny")],
                                  ts: Int64(Date().timeIntervalSince1970 * 1000))
        let envelope = try Seal.seal(to: keys.agreementKey().publicKey, JSONEncoder().encode(payload),
                                     label: Labels.push, aad: Data("\(hostID)|\(try keys.deviceID())".utf8))
        #expect(await clear.didReceiveRemoteNotification(["c": envelope]))
        #expect(savedBeforeReceipt)
        #expect(pending.request(id)?.answered == nil)
    }

    @Test func clearRemovesDelivered() async throws {
        let keys = InMemoryDeviceKeys()
        var widgetReloads = 0
        var watchCleared: [String] = []
        let clear = ClearHandler(dependencies: .init(
            core: NotificationServiceCore(keys: keys, pairings: pairings, pending: pending, clock: { start }),
            pending: pending, notifications: notifications,
            reloadWidgets: { widgetReloads += 1 }, cleared: { watchCleared.append($0) }
        ))
        let payload = PushPayload(kind: .clear, title: "", body: "", requestId: requestID, ts: 1)
        let envelope = try Seal.seal(
            to: keys.agreementKey().publicKey, JSONEncoder().encode(payload), label: Labels.push,
            aad: Data("\(hostID)|\(try keys.deviceID())".utf8)
        )
        #expect(await clear.didReceiveRemoteNotification(["c": "AQ"]) == false)
        #expect(await clear.didReceiveRemoteNotification(["aps": ["content-available": 1], "c": envelope]))
        #expect(notifications.shown.isEmpty)
        #expect(pending.request(requestID) == nil)
        #expect(widgetReloads == 1)
        #expect(watchCleared == [requestID])
    }

    @Test func laterPushFromTheMachineRemovesItsOfflineAlert() async throws {
        let keys = InMemoryDeviceKeys()
        let clear = ClearHandler(dependencies: .init(
            core: NotificationServiceCore(keys: keys, pairings: pairings, pending: pending, clock: { start }),
            pending: pending, notifications: notifications, reloadWidgets: {}
        ))
        func envelope(_ payload: PushPayload) throws -> [AnyHashable: Any] {
            ["aps": ["content-available": 1], "c": try Seal.seal(
                to: keys.agreementKey().publicKey, JSONEncoder().encode(payload), label: Labels.push,
                aad: Data("\(hostID)|\(try keys.deviceID())".utf8)
            )]
        }
        _ = await clear.didReceiveRemoteNotification(try envelope(PushPayload(kind: .offline, title: "", body: "", ts: 1, active: 2)))
        #expect(notifications.offlineRemoved.isEmpty, "the offline alert itself stays")
        _ = await clear.didReceiveRemoteNotification(try envelope(PushPayload(kind: .done, title: "", body: "", ts: 1)))
        #expect(notifications.offlineRemoved == [hostID])
        #expect(pending.claimOffline(hostId: hostID, ts: 5) == true)
        await clear.didEnterForeground(open: [], reported: [hostID])
        #expect(notifications.offlineRemoved == [hostID, hostID])
        #expect(pending.claimOffline(hostId: hostID, ts: 5) == true, "a reconnect forgets the shown offline push")
    }

    @Test func foregroundDropsWhatTheMachineNoLongerLists() async {
        let other = DeliveredRequest(requestId: "r_bbbbbbbbbb", hostId: "h_bbbbbbbbbbbbbbbbbbbbbbbbbb")
        notifications.shown.append(other)
        let clear = ClearHandler(dependencies: .init(
            core: NotificationServiceCore(keys: InMemoryDeviceKeys(), pairings: pairings, pending: pending, clock: { start }),
            pending: pending, notifications: notifications, reloadWidgets: {}
        ))
        await clear.didEnterForeground(open: [], reported: [hostID])
        #expect(notifications.shown == [other], "a machine that has not reported keeps its notifications")
        #expect(pending.request(requestID) == nil)
    }
}
