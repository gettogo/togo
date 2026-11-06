import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct RestoreTests {
    private static let caps: Set<String> = [Capability.events, Capability.restore]
    private static let list = """
    {"sessions":[\
    {"id":"s_abcdefghij","bib":"01","name":"app","agent":"claude","command":["claude","--model","opus"],\
    "cwd":"/Users/sam/src/app","createdAt":"2026-11-03T10:00:00Z","endedAt":"2026-11-03T11:30:00Z","resumable":true},\
    {"id":"s_klmnopqrst","bib":"02","name":"site","agent":"codex","command":null,\
    "cwd":"/Users/sam/src/site","createdAt":"2026-11-03T10:05:00Z","endedAt":"2026-11-03T11:30:00Z","resumable":false}]}
    """

    private func restorable(_ id: String = "s_abcdefghij", resumable: Bool = true) -> RestorableSession {
        RestorableSession(
            id: id, bib: "01", name: "app", agent: "claude", command: ["claude"], cwd: "/Users/sam/src/app",
            createdAt: Samples.now, endedAt: Samples.now, resumable: resumable
        )
    }

    private func machine(
        caps: Set<String> = caps, reachability: Reachability = .online, restorable: [RestorableSession] = []
    ) -> MachineSection {
        MachineSection(
            id: Samples.studio, name: "Studio", symbol: .desktop, reachability: reachability,
            helper: .present(version: "0.4.0", live: true), sessions: [], lastSeen: Samples.now,
            capabilities: caps, restorable: restorable
        )
    }

    private func store(_ machine: MachineSection) -> AppStore {
        AppStore(services: FakeAppServices(), controlCenter: ControlCenterState(machines: [machine], loaded: true))
    }

    private func snapshot(_ reachability: Reachability, caps: Set<String> = caps) -> HostViewSnapshot {
        HostViewSnapshot(
            hostID: Samples.studio, reachability: reachability, helper: .present(version: "0.4.0", live: true),
            sessions: [], requests: [], at: Samples.now, capabilities: caps
        )
    }

    private func tools() -> FakeMachineTools {
        let tools = FakeMachineTools()
        tools.defaultCapabilities = Self.caps
        tools.answer("session.restorable", Self.list)
        return tools
    }

    private static func started(status: String = "idle") -> String {
        """
        {"id":"s_abcdefghij","bib":"01","name":"app","agent":"claude","cwd":"/Users/sam/src/app",\
        "createdAt":"2026-11-03T12:00:00Z","status":"\(status)","activity":null,"attachedClients":0,\
        "managed":true,"muted":false,"meta":{"model":null,"contextPercent":null,"costUsd":null},\
        "command":["claude"],"exit":null}
        """
    }

    @Test func loadsOncePerConnection() async {
        let store = store(machine())
        let tools = tools()
        await store.loadRestorable(tools: tools)
        #expect(store.controlCenter.machines[0].restorable.map(\.id) == ["s_abcdefghij", "s_klmnopqrst"])
        // More snapshots from the same connection do not ask again.
        store.apply(snapshot(.online))
        await store.loadRestorable(tools: tools)
        await store.loadRestorable(tools: tools)
        #expect(tools.requests.map(\.method) == ["session.restorable"])
        // The machine went away and came back: a new hello, so it asks once more.
        store.apply(snapshot(.offline(since: Samples.now, reason: "timeout")))
        store.apply(snapshot(.online))
        await store.loadRestorable(tools: tools)
        #expect(tools.requests.count == 2)
    }

    @Test func machineWithoutRestoreAsksNothing() async {
        let store = store(machine(caps: [Capability.events]))
        let tools = tools()
        await store.loadRestorable(tools: tools)
        #expect(tools.requests.isEmpty)
        #expect(store.restoreCandidates.isEmpty)
    }

    @Test func restoreMovesRow() async throws {
        let first = restorable()
        let store = store(machine(restorable: [first, restorable("s_klmnopqrst")]))
        let tools = tools()
        tools.answer("session.restore", Self.started())
        await store.restore(first, on: Samples.studio, resume: false, tools: tools)

        #expect(tools.requests == [FakeMachineTools.Request(
            method: "session.restore", params: ["id": .string("s_abcdefghij"), "resume": .bool(false)]
        )])
        let section = store.controlCenter.machines[0]
        #expect(section.restorable.map(\.id) == ["s_klmnopqrst"])
        let row = try #require(section.sessions.first)
        #expect(row.id == "s_abcdefghij")
        #expect(row.bib == "01")
        #expect(row.status == .idle)
        #expect(row.agentName == "Claude Code")
        #expect(store.notice == nil)
        #expect(store.restoring.isEmpty)
    }

    @Test func continueOnlyWhenResumable() async {
        #expect(RestoreActions.offered(for: restorable(resumable: true)) == [.restore, .restoreAndContinue])
        #expect(RestoreActions.offered(for: restorable(resumable: false)) == [.restore])
        #expect(RestoreActions.title(.restore) == "Restore")
        #expect(RestoreActions.title(.restoreAndContinue) == "Restore and continue")

        // A row the helper judged unsafe never sends the resume flag, whatever is asked.
        let plain = restorable(resumable: false)
        let store = store(machine(restorable: [plain]))
        let tools = tools()
        tools.answer("session.restore", Self.started())
        await store.restore(plain, on: Samples.studio, resume: true, tools: tools)
        #expect(tools.requests.first?.params["resume"] == .bool(false))

        let safe = restorable(resumable: true)
        let again = self.store(machine(restorable: [safe]))
        await again.restore(safe, on: Samples.studio, resume: true, tools: tools)
        #expect(tools.requests.last?.params["resume"] == .bool(true))
    }

    @Test func folderGoneSaysSo() async {
        let row = restorable()
        let store = store(machine(restorable: [row]))
        let tools = tools()
        tools.fail("session.restore", code: -32003, message: "invalid_path")
        await store.restore(row, on: Samples.studio, resume: false, tools: tools)
        #expect(store.notice == Notice(text: "The folder /Users/sam/src/app is gone on Studio.", isError: true))
        #expect(store.controlCenter.machines[0].restorable.map(\.id) == [row.id])
        #expect(store.controlCenter.machines[0].sessions.isEmpty)
    }

    @Test func otherRefusalsSayWhy() async {
        let row = restorable()
        let store = store(machine(restorable: [row]))
        let tools = tools()

        tools.fail("session.restore", code: -32602, message: "invalid params", data: #"{"reason":"not_installed"}"#)
        await store.restore(row, on: Samples.studio, resume: false, tools: tools)
        #expect(store.notice?.text == "Couldn't restore 01 app: Claude Code is not installed on Studio.")

        tools.fail("session.restore", code: -32006, message: "conflict", data: #"{"reason":"running"}"#)
        await store.restore(row, on: Samples.studio, resume: false, tools: tools)
        #expect(store.notice?.text == "Couldn't restore 01 app: The agent is still running.")

        tools.fail("session.restore", code: -32008, message: "refused", data: #"{"reason":"other"}"#)
        await store.restore(row, on: Samples.studio, resume: false, tools: tools)
        #expect(store.notice?.text == "Couldn't restore 01 app: The Togo helper on the host turned that down.")
        #expect(store.controlCenter.machines[0].restorable.count == 1)

        // Already restored or dismissed from another phone: the row goes away.
        tools.fail("session.restore", code: -32001, message: "not_found")
        await store.restore(row, on: Samples.studio, resume: false, tools: tools)
        #expect(store.notice?.text == "01 app can no longer be restored.")
        #expect(store.controlCenter.machines[0].restorable.isEmpty)
    }

    @Test func dismissRemoves() async {
        let first = restorable()
        let store = store(machine(restorable: [first, restorable("s_klmnopqrst")]))
        let tools = tools()
        await store.dismissRestorable(first, on: Samples.studio, tools: tools)
        #expect(tools.requests == [FakeMachineTools.Request(method: "session.dismiss", params: ["id": .string(first.id)])])
        #expect(store.controlCenter.machines[0].restorable.map(\.id) == ["s_klmnopqrst"])
    }

    @Test func failedDismissKeepsRow() async {
        let first = restorable()
        let store = store(machine(restorable: [first]))
        let tools = tools()
        tools.fail("session.dismiss", code: -32603)
        await store.dismissRestorable(first, on: Samples.studio, tools: tools)
        #expect(store.controlCenter.machines[0].restorable.count == 1)
        #expect(store.notice?.isError == true)
    }

    @Test func doubleTapRestoresOnce() async {
        let row = restorable()
        let store = store(machine(restorable: [row]))
        let tools = tools()
        tools.answer("session.restore", Self.started())
        async let first: Void = store.restore(row, on: Samples.studio, resume: false, tools: tools)
        async let second: Void = store.restore(row, on: Samples.studio, resume: false, tools: tools)
        _ = await (first, second)
        #expect(tools.requests.filter { $0.method == "session.restore" }.count == 1)
    }

    @Test func snapshotThatAlreadyListsTheSessionLeavesOneRow() async {
        let row = restorable()
        let store = store(machine(restorable: [row]))
        var live = Samples.row(bib: "01", name: "app", status: .idle, cwd: "/Users/sam/src/app")
        live.id = row.id
        var snap = snapshot(.online)
        snap.sessions = [live]
        store.apply(snap)
        let tools = tools()
        tools.answer("session.restore", Self.started())
        await store.restore(row, on: Samples.studio, resume: false, tools: tools)
        #expect(store.controlCenter.machines[0].sessions.map(\.id) == [row.id])
        #expect(store.controlCenter.machines[0].restorable.isEmpty)
    }

    @Test func machinesLoadIndependently() async {
        var other = machine(caps: [Capability.events])
        other.id = HostID()
        other.name = "Build box"
        let store = AppStore(services: FakeAppServices(), controlCenter: ControlCenterState(
            machines: [machine(), other], loaded: true
        ))
        let tools = tools()
        await store.loadRestorable(tools: tools)
        // Only the machine that offers restore is asked, and only it gets a list.
        #expect(store.controlCenter.machines.map(\.restorable.count) == [2, 0])
        #expect(tools.calledHosts == [Samples.studio])
    }

    @Test func fetchInFlightWhenMachineGoesAwayIsDropped() async {
        let store = store(machine())
        let tools = tools()
        let load = Task { await store.loadRestorable(tools: tools) }
        while !store.restoreLoaded.contains(Samples.studio) { await Task.yield() }
        store.apply(snapshot(.offline(since: Samples.now, reason: "timeout")))
        await load.value
        #expect(store.controlCenter.machines[0].restorable.isEmpty)
        #expect(!store.restoreLoaded.contains(Samples.studio))
    }

    @Test func successKeepsAnUnrelatedNotice() async {
        let row = restorable()
        let store = store(machine(restorable: [row]))
        store.notice = Notice(text: "Something else", isError: false)
        let tools = tools()
        tools.answer("session.restore", Self.started())
        await store.restore(row, on: Samples.studio, resume: false, tools: tools)
        #expect(store.notice?.text == "Something else")
    }
}
