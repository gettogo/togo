import Foundation
import Testing
import TogoKit
import TogoPush
@testable import Togo

@MainActor private final class FakeWatchLink: Togo.WatchLink {
    var canReachWatchApp = true
    var sent: [[String: Any]] = []

    func updateApplicationContext(_ context: [String: Any]) throws {
        sent.append(context)
    }

    func last<T: Decodable>(_ key: String, as type: T.Type) throws -> T {
        let data = try #require(sent.last?[key] as? Data)
        return try JSONDecoder().decode(T.self, from: data)
    }
}

@MainActor private final class PairingHelper {
    var pairs: [PushPairParams] = []

    func pair(_ params: PushPairParams) async throws -> PushPairResult {
        pairs.append(params)
        return PushPairResult(hostId: "h_studio", hostKey: Base64URL.encode(Data(repeating: 7, count: 32)))
    }
}

@MainActor struct WatchBridgeTests {
    private let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    private let defaults = UserDefaults(suiteName: "watch-bridge-\(UUID().uuidString)")!
    private let link = FakeWatchLink()

    private func bridge() -> WatchBridge {
        WatchBridge(link: link, defaults: defaults, clock: { Samples.now })
    }

    private static let device = WatchDevice(
        deviceId: "w_abcdefghijklmnopqrstuvwxyz", name: "Sam's Watch",
        x25519: Base64URL.encode(Data(repeating: 1, count: 32)),
        approvalKey: Base64URL.encode(Data(repeating: 2, count: 32)), apnsToken: String(repeating: "ef", count: 32)
    )

    private static func request(_ id: String, answered: PendingRequest.Answered? = nil, age: TimeInterval = 30)
        -> PendingRequest {
        PendingRequest(
            requestId: id, hostId: "h_studio", sessionId: "s_1", bib: "04", agent: "claude", host: "studio",
            title: "Run tests?", subject: nil, options: [], receivedAt: Samples.now.addingTimeInterval(-age),
            answered: answered
        )
    }

    @Test func watchDeviceForwardedToCoordinator() async throws {
        let bridge = bridge()
        var changed = 0
        bridge.onDevicesChanged = { changed += 1 }
        bridge.received(device: try JSONEncoder().encode(Self.device))
        bridge.received(device: try JSONEncoder().encode(Self.device))
        #expect(bridge.devices == [Self.device])
        #expect(changed == 1)

        let registrar = PushRegistrar(permissions: FakePermissions(.authorized), registerForRemote: {})
        await registrar.refresh()
        registrar.didRegister(token: Data(repeating: 0xab, count: 32))
        let coordinator = PairingCoordinator(
            keys: InMemoryDeviceKeys(), store: PairingStore(directory: directory),
            unpairs: PendingUnpairStore(directory: directory), registrar: registrar, settings: { AppSettings() },
            liveTokens: { _ in (update: nil, start: nil) }, watch: { bridge.devices },
            hostedRelay: URL(string: "https://relay.example.test"), deviceName: "Sam's iPhone",
            clock: { Samples.now }
        )
        let helper = PairingHelper()
        await coordinator.machineOnline(hostID: Samples.studio, name: "Studio", capabilities: ["push.v1"]) {
            try await helper.pair($0)
        }
        let watch = try #require(helper.pairs.last)
        #expect(watch.platform == .watchos)
        #expect(watch.deviceId == Self.device.deviceId)
        #expect(watch.apnsToken == Self.device.apnsToken)

        // Kept for the next launch, before the Watch has a reason to report again.
        #expect(WatchBridge(link: link, defaults: defaults).devices == [Self.device])
    }

    @Test func phoneIdsAndEmptyTokensAreRefused() throws {
        let bridge = bridge()
        var phone = Self.device
        phone.deviceId = "d_abcdefghijklmnopqrstuvwxyz"
        var tokenless = Self.device
        tokenless.apnsToken = ""
        bridge.received(device: try JSONEncoder().encode(phone))
        bridge.received(device: try JSONEncoder().encode(tokenless))
        bridge.received(device: Data("{".utf8))
        #expect(bridge.devices.isEmpty)
    }

    @Test func pairingsSentBack() throws {
        let bridge = bridge()
        let pairing = Pairing(
            hostId: "h_studio", hostKey: "key", relayURL: URL(string: "https://relay.example")!, machineName: "Studio",
            appHostID: Samples.studio, pairedAt: Samples.now, tokenHash: "abc123", liveActivityCommand: false
        )
        bridge.sendPairings([pairing])

        let sent = try link.last(WatchLinkKey.pairings, as: [Pairing].self)
        #expect(sent.map(\.hostId) == ["h_studio"])
        #expect(sent.map(\.hostKey) == ["key"])
        #expect(sent.map(\.relayURL) == [URL(string: "https://relay.example")!])
        #expect(sent.allSatisfy { $0.appHostID == nil && $0.tokenHash.isEmpty })

        // A later context keeps the pairings beside it; the system keeps only the latest dictionary.
        bridge.publish(sessions: [], requests: [])
        #expect(link.sent.last?[WatchLinkKey.pairings] != nil)
        #expect(link.sent.last?[WatchLinkKey.context] != nil)
    }

    @Test func doneSilentReachesTheWatch() throws {
        let bridge = bridge()
        bridge.sendDoneSilent(true)
        #expect(link.sent.last?[WatchLinkKey.doneSilent] as? Bool == true)
        // Kept beside later contexts, and not sent again while unchanged.
        bridge.publish(sessions: [], requests: [])
        #expect(link.sent.last?[WatchLinkKey.doneSilent] as? Bool == true)
        let count = link.sent.count
        bridge.sendDoneSilent(true)
        #expect(link.sent.count == count)
        bridge.sendDoneSilent(false)
        #expect(link.sent.last?[WatchLinkKey.doneSilent] as? Bool == false)
    }

    @Test func nothingSentUntilTheWatchAppCanHearIt() throws {
        link.canReachWatchApp = false
        let bridge = bridge()
        bridge.publish(sessions: [], requests: [Self.request("r_done", answered: .denied)])
        #expect(link.sent.isEmpty)
        link.canReachWatchApp = true
        bridge.flush()
        #expect(try link.last(WatchLinkKey.context, as: WatchContext.self).closedRequestIds == ["r_done"])
    }

    /// The phone may never have seen a request the Watch holds, so it names only what it knows
    /// is closed: answered, expired or cleared.
    @Test func contextCarriesOnlyClosedRequests() throws {
        let bridge = bridge()
        let session = WatchSession(hostName: "Studio", bib: "04", agentName: "Claude", status: "needs_you", activity: nil)
        bridge.publish(sessions: [session], requests: [
            Self.request("r_open"), Self.request("r_sending", answered: .sending),
            Self.request("r_approved", answered: .approved), Self.request("r_elsewhere", answered: .elsewhere),
            Self.request("r_expired", age: 700),
        ])
        let context = try link.last(WatchLinkKey.context, as: WatchContext.self)
        #expect(context.closedRequestIds == ["r_approved", "r_elsewhere", "r_expired"])
        #expect(context.sessions == [session])
        #expect(context.at == Samples.now)

        bridge.cleared("r_open")
        #expect(try link.last(WatchLinkKey.context, as: WatchContext.self).closedRequestIds
            == ["r_approved", "r_elsewhere", "r_expired", "r_open"])
    }

    @Test func sessionRowsUseCleanActivity() {
        let row = SessionRow(
            id: "s_1", hostID: Samples.studio, bib: "04", name: "api", agent: "claude", agentName: "Claude",
            status: .needsYou, activity: "\u{2502} Running tests  \u{2502}", cwd: "~", muted: false, managed: true,
            meta: .init(), statusSince: Samples.now
        )
        let machine = MachineSection(
            id: Samples.studio, name: "Studio", symbol: .mini, reachability: .online,
            helper: .present(version: "1", live: true), sessions: [row]
        )
        #expect(WatchSession.rows([machine]) == [WatchSession(
            hostName: "Studio", bib: "04", agentName: "Claude", status: "needs_you", activity: "Running tests",
            machineId: Samples.studio.uuidString, sessionId: "s_1"
        )])
    }

    @Test func clearedRequestStaysClosed() throws {
        let bridge = bridge()
        var stored = [Self.request("r_a"), Self.request("r_b", answered: .approved)]
        bridge.currentRequests = { stored }
        // The last publish predates both requests, as when the app wakes for a clear push.
        bridge.publish(sessions: [], requests: [])
        // The clear removes the request from the phone's store, but the Watch still hears it closed.
        stored.removeAll { $0.requestId == "r_a" }
        bridge.cleared("r_a")
        #expect(try link.last(WatchLinkKey.context, as: WatchContext.self).closedRequestIds == ["r_a", "r_b"])
        bridge.publish(sessions: [], requests: [])
        #expect(try link.last(WatchLinkKey.context, as: WatchContext.self).closedRequestIds == ["r_a"])
    }
}
