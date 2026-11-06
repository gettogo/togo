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

    func sealed(_ p: PushPayload) throws -> UNNotificationContent {
        let envelope = try Seal.seal(
            to: keys.agreementKey().publicKey, JSONEncoder().encode(p), label: Labels.push,
            aad: Data("\(Self.hostID)|\(try keys.deviceID())".utf8)
        )
        let content = UNMutableNotificationContent()
        content.userInfo = ["aps": ["mutable-content": 1], "c": envelope]
        return content
    }

    func stopped(_ kind: PushPayload.Kind, code: Int?, reason: String, secondsAgo: Int64 = 0) -> PushPayload {
        PushPayload(
            kind: kind, sessionId: "s_abcdefghij", bib: "01", agent: "claude", host: "studio",
            title: "Claude Code stopped", body: "Exit code 1", ts: 1_793_707_200_000 - secondsAgo * 1000,
            exit: PushExit(code: code, reason: reason, detail: nil)
        )
    }

    /// Run again needs the session it starts, so a stopped push carries its ids and nothing else.
    @Test func stoppedPushCarriesSessionForRunAgain() throws {
        pair(Self.hostID, name: "studio")
        let result = core.process(try sealed(stopped(.failed, code: 1, reason: "exit")))
        let content = result.content
        #expect(content.categoryIdentifier == "togo.ended")
        #expect(content.body == "Stopped with an error · Exited with code 1")
        #expect(content.targetContentIdentifier == "togo://session/01?host=studio")
        let togo = try #require(content.userInfo["togo"] as? [String: String])
        #expect(togo == ["hostId": Self.hostID, "sessionId": "s_abcdefghij"])
        #expect(!result.pendingChanged)

        let limited = core.process(try sealed(stopped(.failed, code: nil, reason: "usage_limit"))).content
        #expect(limited.categoryIdentifier == "togo.prompt", "a usage limit offers Reply, not Run again")
        #expect(limited.userInfo["togo"] as? [String: String] == ["hostId": Self.hostID, "sessionId": "s_abcdefghij"])
    }

    func offline(
        secondsAgo: Int64 = 0, active: Int? = 2, title: String = "attacker says hi", host: String? = "studio"
    ) -> PushPayload {
        PushPayload(
            kind: .offline, sessionId: nil, bib: nil, agent: nil, host: host,
            title: title, body: "free text the phone must not show", ts: 1_793_707_200_000 - secondsAgo * 1000,
            active: active
        )
    }

    @Test func offlineRendersHostAndCount() throws {
        pair(Self.hostID, name: "Studio Mac")
        let one = core.process(try sealed(offline(active: 1))).content
        #expect(one.title == "Studio Mac stopped answering")
        #expect(one.subtitle == "")
        #expect(one.body == "1 session was working.")
        #expect(one.categoryIdentifier == "togo.info")
        #expect(one.threadIdentifier == Self.hostID)
        #expect(one.targetContentIdentifier == nil)
        let two = core.process(try sealed(offline(secondsAgo: -10, active: 2))).content
        #expect(two.body == "2 sessions were working.")
        #expect(two.title == "Studio Mac stopped answering")
        let togo = try #require(two.userInfo["togo"] as? [String: String])
        #expect(togo == ["hostId": Self.hostID, "offline": "1"])
        let unnamed = core.process(try sealed(offline(secondsAgo: -20, active: 3, host: nil))).content
        #expect(unnamed.title == "Studio Mac stopped answering", "the paired record names the machine, not the payload")
    }

    @Test func offlineIsTimeSensitive() throws {
        pair(Self.hostID, name: "studio")
        let content = core.process(try sealed(offline(active: 2))).content
        #expect(content.interruptionLevel == .timeSensitive)
        let text = try #require(NotificationTextBuilder.build(offline(active: 2), hostId: Self.hostID, hostName: "studio"))
        #expect(text.interruption == .timeSensitive)
        #expect(!text.body.contains("free text"))
    }

    @Test func offlineSealedHoursAgoStillRenders() throws {
        pair(Self.hostID, name: "studio")
        let result = core.process(try sealed(offline(secondsAgo: 29 * 3600)))
        #expect(result.content.title == "studio stopped answering")
        #expect(result.content.body == "2 sessions were working.")
        #expect(result.content.interruptionLevel == .timeSensitive)
        #expect(result.content.categoryIdentifier == "togo.info")
        let newer = core.process(try sealed(offline(secondsAgo: 7 * 3600))).content
        #expect(newer.title == "studio stopped answering")
    }

    @Test func offlineOlderThanLimitFallsBack() throws {
        pair(Self.hostID, name: "studio")
        expectGeneric(core.process(try sealed(offline(secondsAgo: 31 * 3600))).content, "past 30 hours")
        var future = offline(active: 2)
        future.ts += 120_000
        expectGeneric(core.process(try sealed(future)).content, "claims to be from the future")
    }

    @Test func nonOfflineStillUses600s() throws {
        pair(Self.hostID, name: "studio")
        var done = PushPayload(kind: .done, sessionId: "s_abcdefghij", bib: "01", agent: "claude", title: "", body: "x", ts: 0)
        done.ts = 1_793_707_200_000 - 700_000
        expectGeneric(core.process(try sealed(done)).content, "700 s old")
        done.ts = 1_793_707_200_000 - 500_000
        #expect(core.process(try sealed(done)).content.categoryIdentifier == "togo.prompt")
        expectGeneric(core.process(try sealed(stopped(.failed, code: 1, reason: "exit", secondsAgo: 3600))).content, "stopped, an hour old")
    }

    @Test func offlineReplayRefused() throws {
        pair(Self.hostID, name: "studio")
        let first = offline(secondsAgo: 3600)
        #expect(core.process(try sealed(first)).content.title == "studio stopped answering")
        expectGeneric(core.process(try sealed(first)).content, "the same seal again")
        var older = first
        older.ts -= 1000
        expectGeneric(core.process(try sealed(older)).content, "an older seal")
        let refreshed = offline(secondsAgo: 60)
        #expect(core.process(try sealed(refreshed)).content.title == "studio stopped answering")
    }

    @Test func replayWithoutAnyContactStillRefused() throws {
        pair(Self.hostID, name: "studio")
        let will = offline(secondsAgo: 3600)
        #expect(core.process(try sealed(will)).content.title == "studio stopped answering")
        expectGeneric(core.process(try sealed(will)).content, "the same frame, nothing heard from the machine")
    }

    @Test func secondOutageAfterPushShows() throws {
        pair(Self.hostID, name: "studio")
        let will = offline(secondsAgo: 3600)
        #expect(core.process(try sealed(will)).content.title == "studio stopped answering")
        let done = PushPayload(kind: .done, sessionId: "s_abcdefghij", bib: "01", agent: "claude", title: "", body: "x", ts: 1_793_707_200_000 - 1000)
        #expect(core.process(try sealed(done)).clearsOfflineOf == Self.hostID)
        #expect(core.process(try sealed(will)).content.title == "studio stopped answering", "same seal, new outage")
    }

    @Test func secondOutageAfterReconnectShows() throws {
        pair(Self.hostID, name: "studio")
        let will = offline(secondsAgo: 3600)
        #expect(core.process(try sealed(will)).content.title == "studio stopped answering")
        pending.clearOfflineMark(hostId: Self.hostID)
        #expect(core.process(try sealed(will)).content.title == "studio stopped answering")
    }

    @Test func staleLaterPushDoesNotClearOffline() throws {
        pair(Self.hostID, name: "studio")
        var done = PushPayload(kind: .done, sessionId: "s_abcdefghij", bib: "01", agent: "claude", title: "", body: "x", ts: 0)
        done.ts = 1_793_707_200_000 - 3_600_000
        #expect(core.process(try sealed(done)).clearsOfflineOf == nil, "a push that is refused proves nothing about now")
    }

    @Test func laterPushClearsOffline() throws {
        pair(Self.hostID, name: "studio")
        let offlineResult = core.process(try sealed(offline(active: 2)))
        #expect(offlineResult.clearsOfflineOf == nil)
        let done = PushPayload(
            kind: .done, sessionId: "s_abcdefghij", bib: "01", agent: "claude", host: "studio",
            title: "", body: "Finished", ts: 1_793_707_200_000
        )
        #expect(core.process(try sealed(done)).clearsOfflineOf == Self.hostID)
        #expect(core.process(try sealed(payload())).clearsOfflineOf == Self.hostID)

        let delivered: [(identifier: String, userInfo: [AnyHashable: Any])] = [
            ("a", offlineResult.content.userInfo),
            ("b", ["togo": ["hostId": Self.otherHostID, "offline": "1"]]),
            ("c", ["togo": ["hostId": Self.hostID, "sessionId": "s_abcdefghij"]]),
        ]
        #expect(OfflineNotifications.identifiers(in: delivered, hostId: Self.hostID) == ["a"])
    }

    @Test func silentDoneStripsSound() throws {
        pair(Self.hostID, name: "studio")
        let silent = NotificationServiceCore(
            keys: keys, pairings: pairings, pending: pending, clock: { Self.now }, doneSilent: { true }
        )
        func loud(_ p: PushPayload) throws -> UNNotificationContent {
            let content = try sealed(p).mutableCopy() as! UNMutableNotificationContent
            content.sound = .default
            return content
        }
        let done = PushPayload(
            kind: .done, sessionId: "s_abcdefghij", bib: "01", agent: "claude", host: "studio",
            title: "", body: "Finished", ts: 1_793_707_200_000
        )
        let quiet = silent.process(try loud(done)).content
        #expect(quiet.sound == nil)
        #expect(quiet.interruptionLevel == .passive)
        #expect(quiet.body == "Finished")

        // Only done is quieted: a request, a stop and an outage keep their sound.
        let request = silent.process(try loud(payload())).content
        #expect(request.sound != nil)
        #expect(request.interruptionLevel == .timeSensitive)
        let failed = silent.process(try loud(stopped(.failed, code: 1, reason: "exit"))).content
        #expect(failed.sound != nil)
        #expect(silent.process(try loud(offline())).content.sound != nil)

        // Off by default: a done keeps its sound.
        let normal = NotificationServiceCore(
            keys: keys, pairings: pairings, pending: pending, clock: { Self.now }, doneSilent: { false }
        )
        let kept = normal.process(try loud(done)).content
        #expect(kept.sound != nil)
        #expect(kept.interruptionLevel == .active)
    }

    @Test func watchKeepsItsOwnDoneSilentChoice() throws {
        let suite = "togo-test-\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        #expect(!SharedSettings.readDoneSilent(defaults))
        SharedSettings.writeDoneSilent(true, to: defaults)
        #expect(SharedSettings.readDoneSilent(defaults))
        SharedSettings.writeDoneSilent(false, to: defaults)
        #expect(!SharedSettings.readDoneSilent(defaults))
        #expect(WatchLinkKey.doneSilent == "doneSilent")
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
