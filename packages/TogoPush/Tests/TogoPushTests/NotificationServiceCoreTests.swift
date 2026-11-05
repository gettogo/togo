import CryptoKit
import Foundation
import Synchronization
import Testing
import UserNotifications
@testable import TogoPush

@Suite(.serialized)
struct NotificationServiceCoreTests {
    static let now = Date(timeIntervalSince1970: 1_793_707_200)
    static let hostID = "h_mmg42klgyqzwneiskrelxms3j7"
    static let otherHostID = "h_bbbbbbbbbbbbbbbbbbbbbbbbbb"

    let keys = InMemoryDeviceKeys()
    let directory = FileManager.default.temporaryDirectory.appending(path: "togo-nse-tests-\(UUID().uuidString)")

    var pairings: PairingStore { PairingStore(directory: directory) }
    var pending: PendingStore { PendingStore(directory: directory, clock: { Self.now }) }
    var core: NotificationServiceCore {
        NotificationServiceCore(keys: keys, pairings: pairings, pending: pending, clock: { Self.now })
    }

    func pair(_ hostID: String, name: String) {
        pairings.put(Pairing(
            hostId: hostID, hostKey: Base64URL.encode(Curve25519.KeyAgreement.PrivateKey().publicKey.rawRepresentation),
            relayURL: URL(string: "https://relay.example")!, machineName: name, appHostID: nil, pairedAt: Self.now,
            tokenHash: "a1b2c3", liveActivityCommand: false
        ))
    }

    func payload(ts: Int64 = 1_793_707_200_000) -> PushPayload {
        PushPayload(
            kind: .needsYou, sessionId: "s_abcdefghij", bib: "04", agent: "claude", host: "studio",
            title: "Do you want to proceed?", body: "", requestId: "r_aaaaaaaaaa",
            options: [PushOption(n: 1, label: "Yes", role: "approve"), PushOption(n: 2, label: "No", role: "deny")],
            subject: PushSubject(kind: "command", text: "rm -rf ./build && make"), ts: ts
        )
    }

    /// What the relay delivers: the generic alert and the envelope sealed by `hostID`'s helper.
    func generic(
        sealedFor hostID: String = hostID, aadDevice: String? = nil, ts: Int64 = 1_793_707_200_000
    ) throws -> UNNotificationContent {
        let device = try aadDevice ?? keys.deviceID()
        let envelope = try Seal.seal(
            to: keys.agreementKey().publicKey, JSONEncoder().encode(payload(ts: ts)), label: Labels.push,
            aad: Data("\(hostID)|\(device)".utf8)
        )
        let content = UNMutableNotificationContent()
        content.title = "Togo"
        content.body = "Needs you"
        content.userInfo = ["aps": ["mutable-content": 1], "c": envelope]
        return content
    }

    func expectGeneric(_ content: UNNotificationContent, _ comment: Comment) {
        #expect(Seen(content) == Seen.generic, comment)
    }

    @Test func opensAndRenders() throws {
        pair(Self.otherHostID, name: "laptop")
        pair(Self.hostID, name: "studio")
        let result = core.process(try generic())
        let content = result.content
        #expect(content.title == "04 Claude Code on studio")
        #expect(content.subtitle == "Do you want to proceed?")
        #expect(content.body == "rm -rf ./build && make")
        #expect(content.categoryIdentifier == "togo.request")
        #expect(content.threadIdentifier == "s_abcdefghij")
        #expect(content.interruptionLevel == .timeSensitive)
        #expect(content.targetContentIdentifier == "togo://session/04?host=studio")
        #expect(result.pendingChanged)
        let stored = try #require(pending.request("r_aaaaaaaaaa"))
        #expect(stored.hostId == Self.hostID)
        #expect(stored.subject?.text == "rm -rf ./build && make")
        #expect(stored.receivedAt == Self.now)
    }

    @Test func userInfoHoldsNoCommandText() throws {
        pair(Self.hostID, name: "studio")
        let content = core.process(try generic()).content
        #expect(content.userInfo["c"] == nil, "the envelope is not kept")
        let togo = try #require(content.userInfo["togo"] as? [String: Any])
        #expect(Set(togo.keys) == ["requestId", "hostId", "sessionId", "options"])
        #expect(togo["requestId"] as? String == "r_aaaaaaaaaa")
        #expect(togo["hostId"] as? String == Self.hostID)
        let dump = String(describing: content.userInfo)
        #expect(!dump.contains("rm -rf"))
        #expect(!dump.contains("Do you want"))
    }

    @Test func openFailureKeepsGenericContent() throws {
        pair(Self.hostID, name: "studio")
        expectGeneric(core.process(try generic(aadDevice: "d_someoneelse")).content, "aad names another device")
        let tampered = try generic().mutableCopy() as! UNMutableNotificationContent
        tampered.userInfo["c"] = "AQ" + String(repeating: "A", count: 90)
        expectGeneric(core.process(tampered).content, "garbage envelope")
        let missing = try generic().mutableCopy() as! UNMutableNotificationContent
        missing.userInfo = [:]
        expectGeneric(core.process(missing).content, "no envelope")
        try keys.reset()
        expectGeneric(core.process(try generic(aadDevice: "d_old")).content, "key replaced after reinstall")
        #expect(pending.all().isEmpty)
    }

    @Test func missingPairingKeepsGeneric() throws {
        pair(Self.otherHostID, name: "laptop")
        let result = core.process(try generic())
        expectGeneric(result.content, "sealed by a machine this device is not paired with")
        #expect(!result.pendingChanged)
        #expect(pending.all().isEmpty)
    }

    @Test func removedMachineAwaitingUnpairKeepsGeneric() throws {
        pair(Self.hostID, name: "studio")
        let pairing = try #require(pairings.pairing(hostId: Self.hostID))
        PendingUnpairStore(directory: directory).retire(
            PendingUnpair(pairing: pairing, deviceIds: ["d_phone"], connection: nil), from: pairings
        )
        let result = core.process(try generic())
        expectGeneric(result.content, "sealed by a machine the user removed")
        #expect(result.content.categoryIdentifier.isEmpty, "no lock-screen actions")
        #expect(pending.all().isEmpty)
        #expect(PendingUnpairStore(directory: directory).all().map(\.id) == [Self.hostID])
    }

    @Test func replayedPushIsNotActionable() throws {
        pair(Self.hostID, name: "studio")
        let old = core.process(try generic(ts: 1_793_707_200_000 - 601_000))
        expectGeneric(old.content, "older than ten minutes")
        #expect(!old.pendingChanged)
        expectGeneric(core.process(try generic(ts: 1_793_707_200_000 + 61_000)).content, "from the future")
        #expect(pending.all().isEmpty)
        #expect(core.process(try generic(ts: 1_793_707_200_000 - 599_000)).content.categoryIdentifier == "togo.request")
    }

    @Test func replayOfAnsweredRequestStaysAnswered() throws {
        pair(Self.hostID, name: "studio")
        _ = core.process(try generic())
        pending.mark("r_aaaaaaaaaa", .approved)
        let replay = core.process(try generic(ts: 1_793_707_200_000 - 5_000))
        #expect(pending.request("r_aaaaaaaaaa")?.answered == .approved)
        #expect(replay.content.categoryIdentifier == "togo.info", "an answered request offers no actions")
        #expect(replay.content.userInfo["togo"] == nil)
    }

    @Test func keysUnavailableKeepsGeneric() throws {
        pair(Self.hostID, name: "studio")
        let locked = NotificationServiceCore(
            keys: LockedKeys(), pairings: pairings, pending: pending, clock: { Self.now }
        )
        expectGeneric(locked.process(try generic()).content, "before first unlock")
    }

    @Test func serviceExtensionTimeWillExpireDeliversGeneric() throws {
        let delivered = Mutex<[Seen]>([])
        let gate = DeliveryGate { content in delivered.withLock { $0.append(Seen(content)) } }
        gate.expire()
        pair(Self.hostID, name: "studio")
        gate.finish(core.process(try generic()).content)
        let all = delivered.withLock { $0 }
        #expect(all.count == 1, "the system takes one content only")
        #expect(all.first == Seen.generic, "expired before the open finished")
    }

    @Test func finishedDeliveryIsKept() throws {
        let delivered = Mutex<[Seen]>([])
        let gate = DeliveryGate { content in delivered.withLock { $0.append(Seen(content)) } }
        pair(Self.hostID, name: "studio")
        gate.finish(core.process(try generic()).content)
        gate.expire()
        #expect(delivered.withLock { $0.map(\.title) } == ["04 Claude Code"])
    }

    @Test func clearOpensButBuildsNothing() throws {
        pair(Self.hostID, name: "studio")
        let clear = PushPayload(kind: .clear, title: "", body: "", requestId: "r_aaaaaaaaaa", ts: 1)
        let envelope = try Seal.seal(
            to: keys.agreementKey().publicKey, JSONEncoder().encode(clear), label: Labels.push,
            aad: Data("\(Self.hostID)|\(try keys.deviceID())".utf8)
        )
        let opened = try #require(core.open(userInfo: ["c": envelope]))
        #expect(opened.payload.kind == .clear)
        #expect(opened.pairing.hostId == Self.hostID)
    }

    @Test func approveNeedsUnlockUnlessTurnedOff() throws {
        func actions(_ id: String, without: Bool) throws -> [UNNotificationAction] {
            try #require(NotificationCategorySet.make(approveWithoutUnlock: without).first { $0.identifier == id }).actions
        }
        let locked = try actions("togo.request", without: false)
        #expect(locked.map(\.identifier) == ["togo.approve", "togo.deny", "togo.open"])
        #expect(locked[0].options == .authenticationRequired)
        #expect(locked[1].options == .destructive)
        #expect(locked[2].options == .foreground)
        #expect(try actions("togo.request", without: true)[0].options == [])
        #expect(try actions("togo.request.deny", without: false).map(\.identifier) == ["togo.deny", "togo.open"])
        #expect(try actions("togo.info", without: false).isEmpty)
    }
}

/// The parts of a delivered content the tests look at.
private struct Seen: Equatable, Sendable {
    static let generic = Seen(title: "Togo", subtitle: "", body: "A session needs you", category: "", infoKeys: [])

    var title, subtitle, body, category: String
    var infoKeys: Set<String>

    init(title: String, subtitle: String, body: String, category: String, infoKeys: Set<String>) {
        (self.title, self.subtitle, self.body, self.category, self.infoKeys) = (title, subtitle, body, category, infoKeys)
    }

    init(_ content: UNNotificationContent) {
        self.init(
            title: content.title, subtitle: content.subtitle, body: content.body, category: content.categoryIdentifier,
            infoKeys: Set(content.userInfo.keys.compactMap { $0 as? String })
        )
    }
}

private struct LockedKeys: DeviceKeyStore {
    func deviceID() throws -> String { throw DeviceKeyError.keychain(-25308) }
    func agreementKey() throws -> Curve25519.KeyAgreement.PrivateKey { throw DeviceKeyError.keychain(-25308) }
    func signingKey() throws -> Curve25519.Signing.PrivateKey { throw DeviceKeyError.keychain(-25308) }
    func reset() throws {}
}
