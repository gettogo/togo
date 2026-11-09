import Foundation
import Testing
import TogoKit
import TogoPush
@testable import Togo

@MainActor private final class RunAgainWatchLink: Togo.WatchLink {
    var canReachWatchApp = true
    func updateApplicationContext(_ context: [String: Any]) throws {}
}

/// A machine the phone cannot reach without asking the user something.
@MainActor private final class NoQuietChannel: MachineTools {
    struct Closed: Error {}
    var calls = 0

    func call<T: Sendable>(_ host: UUID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T {
        calls += 1
        throw Closed()
    }
}

/// Run again tapped on a notification shown on the Watch: the phone checks the session as its
/// own Run again would, then starts it over a channel that opens without asking.
@MainActor @Suite(.serialized)
struct WatchRunAgainTests {
    private static let launchCaps: Set<String> = [Capability.events, Capability.launch, Capability.lifecycle]
    private let hostID = "h_mmg42klgyqzwneiskrelxms3j7"
    private let directory = FileManager.default.temporaryDirectory.appending(path: "togo-run-\(UUID().uuidString)")
    private let tools = FakeMachineTools()
    private let store = AppStore(services: FakeAppServices())

    init() {
        tools.defaultCapabilities = Self.launchCaps
        var failed = Samples.row(bib: "01", name: "app", status: .failed, cwd: "/Users/sam/src/app")
        failed.id = "s_abcdefghij"
        failed.exit = SessionExit(code: 1, reason: .exit)
        var waiting = Samples.row(bib: "02", name: "api", status: .failed, cwd: "/Users/sam/src/api")
        waiting.id = "s_bbbbbbbbbb"
        waiting.exit = SessionExit(code: nil, reason: .error)
        store.apply(HostViewSnapshot(
            hostID: Samples.studio, reachability: .online, helper: .present(version: "0.5.0", live: true),
            sessions: [failed, waiting], requests: [], at: Samples.now, capabilities: Self.launchCaps
        ))
        PairingStore(directory: directory).put(Pairing(
            hostId: hostID, hostKey: "key", relayURL: URL(string: "https://relay.example")!, machineName: "studio",
            appHostID: Samples.studio, pairedAt: Samples.now, tokenHash: "a1b2c3", liveActivityCommand: false
        ))
    }

    private func bridge(_ tools: any MachineTools) -> WatchBridge {
        let bridge = WatchBridge(link: RunAgainWatchLink(), defaults: UserDefaults(suiteName: "run-\(UUID().uuidString)")!)
        bridge.onRunAgain = WatchRunAgainGate.handler(store: store, pairings: PairingStore(directory: directory), tools: tools)
        return bridge
    }

    private func message(_ session: String, machine: String? = nil, id: String = "n-1") -> [String: Any] {
        WatchRunAgainMessage(id: id, machineId: machine ?? hostID, sessionId: session).message
    }

    @Test func watchRunAgainRelaunchesOnce() async throws {
        tools.answer("session.relaunch", Self.relaunched(id: "s_abcdefghij"))
        let bridge = bridge(tools)
        #expect(ReplyOutcome(message: await bridge.handle(message: message("s_abcdefghij"))) == .sent)
        #expect(tools.requests == [FakeMachineTools.Request(method: "session.relaunch", params: ["id": .string("s_abcdefghij")])])
        #expect(store.controlCenter.machines[0].sessions.first { $0.id == "s_abcdefghij" }?.status == .working)
        // The Watch retries the same tap: the phone starts nothing twice.
        #expect(ReplyOutcome(message: await bridge.handle(message: message("s_abcdefghij"))) == .sent)
        #expect(tools.requests.count == 1)
    }

    /// An agent still alive at its prompt, a session or machine the phone does not know: the
    /// Watch hears why, and nothing reaches the machine.
    @Test func watchRunAgainRefusals() async {
        let bridge = bridge(tools)
        func send(_ m: [String: Any]) async -> ReplyOutcome { ReplyOutcome(message: await bridge.handle(message: m)) }
        #expect(await send(message("s_bbbbbbbbbb"))
            == .refused("Couldn't run 02 Claude Code again: The agent is still running."))
        #expect(await send(message("s_zzzzzzzzzz")) == .refused("Couldn't run it again: the session is gone."))
        #expect(await send(message("s_abcdefghij", machine: UUID().uuidString))
            == .refused("Couldn't run it again: this machine is no longer in Togo."))
        #expect(await send(message("s_abcdefghij", machine: "h_aaaaaaaaaaaaaaaaaaaaaaaaaa"))
            == .refused("Couldn't run it again: this machine is no longer in Togo."))
        var extra = message("s_abcdefghij")
        extra["text"] = "rm -rf ~"
        #expect(await send(extra) == .refused("Not sent: the message could not be read."))
        #expect(tools.requests.isEmpty)
    }

    /// The phone never asks the user anything for the Watch; a channel that needs asking means
    /// the phone is needed.
    @Test func watchRunAgainNeedsPhoneWhenChannelClosed() async {
        let closed = NoQuietChannel()
        let bridge = bridge(closed)
        #expect(ReplyOutcome(message: await bridge.handle(message: message("s_abcdefghij"))) == .needsPhone)
        #expect(closed.calls == 1)
        #expect(store.controlCenter.machines[0].sessions.first { $0.id == "s_abcdefghij" }?.status == .failed)
    }

    @Test func watchRunAgainSaysWhyTheHelperRefused() async {
        tools.fail("session.relaunch", code: -32006, message: "conflict", data: #"{"reason":"running"}"#)
        let bridge = bridge(tools)
        #expect(ReplyOutcome(message: await bridge.handle(message: message("s_abcdefghij")))
            == .refused("Couldn't run 01 Claude Code again: The agent is still running."))
    }

    private static func relaunched(id: String) -> String {
        """
        {"id":"\(id)","bib":"01","name":"app","agent":"claude","cwd":"/Users/sam/src/app",\
        "createdAt":"2026-11-03T12:00:00Z","status":"working","activity":null,"attachedClients":0,\
        "managed":true,"muted":false,"meta":{"model":null,"contextPercent":null,"costUsd":null},\
        "command":["claude"],"exit":null}
        """
    }
}
