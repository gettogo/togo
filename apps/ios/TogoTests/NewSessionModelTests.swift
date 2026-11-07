import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct NewSessionModelTests {
    private let services = FakeAppServices()

    private func model(
        hostID: HostID? = nil, machines: [MachineSection] = Samples.controlCenter.machines,
        store: AppStore? = nil
    ) -> NewSessionModel {
        NewSessionModel(
            services: services, hostID: hostID, machines: machines,
            open: { store?.open(hostID: $0, sessionID: $1) }
        )
    }

    private func readyToCreate(_ model: NewSessionModel, folder: String = "/Users/sam/src/app") async {
        await model.choose(model.machines[0])
        await model.choose(folder: folder)
    }

    @Test func skipsMachineStepWithOneHost() async {
        let one = Array(Samples.controlCenter.machines.prefix(1))
        let single = model(machines: one)
        #expect(single.step == .folder)
        #expect(single.hostID == Samples.studio)
        #expect(single.pushedSteps.isEmpty)
        await single.start()
        #expect(single.recent == .loaded(["/Users/sam/src/app", "/Users/sam/src/site", "/Users/sam/notes"]))

        let several = model()
        #expect(several.step == .machine)
        #expect(several.hostID == nil)
    }

    @Test func nameWithQuotesSentVerbatim() async {
        let model = model()
        await readyToCreate(model)
        model.name = #"a "b"; $(c)"#
        _ = await model.create()
        #expect(services.calls == [
            .create(Samples.studio, CreateSessionParams(agent: "claude", cwd: "/Users/sam/src/app", name: #"a "b"; $(c)"#)),
        ])
    }

    @Test func emptyNameOmitted() async {
        let model = model()
        await readyToCreate(model)
        model.name = "   "
        _ = await model.create()
        guard case .create(_, let params)? = services.calls.first else {
            Issue.record("no create call")
            return
        }
        #expect(params.name == nil)
    }

    @Test func badFolderShowsInline() async {
        let model = model()
        await readyToCreate(model, folder: "/gone")
        services.failure = HelperError.rpc(RPCErrorObject(code: RPCErrorCode.invalidPath, message: "invalid_path"))
        let row = await model.create()
        #expect(row == nil)
        #expect(model.createError == "That folder no longer exists.")
        #expect(!model.creating)
    }

    @Test func fullMachineShowsSessionLimit() async {
        var machines = Samples.controlCenter.machines
        machines[0].sessions = (1...99).map { Samples.row(bib: String(format: "%02d", $0), name: "s\($0)", cwd: "/") }
        let model = model(machines: machines)
        await readyToCreate(model)
        services.failure = HelperError.rpc(RPCErrorObject(code: RPCErrorCode.invalidParams, message: "invalid params"))
        _ = await model.create()
        #expect(model.createError == "Studio has 99 sessions. Close one first.")
    }

    @Test func createOpensWorkspace() async {
        let store = AppStore(services: services, controlCenter: Samples.controlCenter)
        let model = model(store: store)
        await readyToCreate(model)
        let row = await model.create()
        #expect(row != nil)
        #expect(store.workspaceOpen)
        #expect(store.workspace.selected == row.map { TabID(hostID: $0.hostID, sessionID: $0.id) })
        #expect(store.workspace.tabs.map(\.hostID) == [Samples.studio])
    }

    @Test func machineWithoutHelperStartsPlainSession() async {
        var machines = Samples.controlCenter.machines
        machines[0].helper = .missing
        let model = model(machines: machines)
        await model.choose(model.machines[0])
        #expect(model.recent == .idle)
        _ = await model.create()
        #expect(services.calls == [.create(Samples.studio, CreateSessionParams())])
    }

    @Test func offlineMachineIsUnavailableWithReason() {
        let model = model()
        #expect(model.machines.map(\.unavailable) == [nil, "Offline, asleep"])
    }

    @Test func installedAgentsComeFirstAndShellIsAlwaysOffered() async {
        let model = model()
        await model.choose(model.machines[0])
        model.agents = .loaded([
            AgentInfo(id: "codex", name: "Codex", installed: false),
            AgentInfo(id: "claude", name: "Claude Code", installed: true),
        ])
        #expect(model.offeredAgents.map(\.id) == ["claude", "shell", "codex"])
        #expect(model.selectedAgent?.id == "claude")
    }

    @Test func favoritesLeadAndMissingAgentsFoldAway() async {
        let library = AgentLibrary.inMemory()
        library.toggleFavorite("qwen")
        library.used("aider")
        let model = NewSessionModel(services: services, hostID: nil, machines: Samples.controlCenter.machines, library: library)
        await model.choose(model.machines[0])
        model.agents = .loaded([
            AgentInfo(id: "claude", name: "Claude Code", installed: true),
            AgentInfo(id: "aider", name: "Aider", installed: true),
            AgentInfo(id: "qwen", name: "Qwen Code", installed: true),
            AgentInfo(id: "gemini", name: "Gemini CLI", installed: false),
        ])
        #expect(model.installedAgents.map(\.id) == ["qwen", "aider", "claude", "shell"])
        #expect(model.missingAgents.map(\.id) == ["gemini"])
        #expect(model.selectedAgent?.id == "qwen")
        model.toggleFavorite("qwen")
        #expect(!model.isFavorite("qwen"))
        #expect(model.installedAgents.map(\.id) == ["aider", "claude", "qwen", "shell"])
    }

    @Test func browseAnswerAfterLeavingIsIgnored() async {
        let model = model()
        await model.choose(model.machines[0])
        services.listDirDelays["/slow"] = .milliseconds(150)
        let pending = Task { await model.browse("/slow") }
        await Task.yield()
        model.leaveBrowsing()
        await pending.value
        #expect(model.browsing == .idle)
    }

    @Test func latestBrowseRequestWinsWhenAnswersArriveOutOfOrder() async {
        let model = model()
        await model.choose(model.machines[0])
        services.listDirDelays["/first"] = .milliseconds(150)
        let first = Task { await model.browse("/first") }
        await Task.yield()
        await model.browse("/second")
        await first.value
        #expect(model.browsing == .loaded(ListDirResult(path: "/second", entries: [])))
    }

    @Test func stepBackDropsListingInFlight() async {
        let model = model()
        await model.choose(model.machines[0])
        services.listDirDelays["/slow"] = .milliseconds(150)
        let pending = Task { await model.browse("/slow") }
        await Task.yield()
        model.step = .machine
        await pending.value
        #expect(model.browsing == .idle)
    }

    @Test func retryBrowsesTheFolderThatFailed() async {
        let model = model()
        await model.choose(model.machines[0])
        services.failure = HelperError.closed
        await model.browse("/Users/sam/src/x")
        guard case .failed = model.browsing else {
            Issue.record("expected a failed listing")
            return
        }
        services.failure = nil
        await model.retryBrowse()
        #expect(model.browsing == .loaded(ListDirResult(path: "/Users/sam/src/x", entries: [])))
    }

    @Test func secondStartIsRefusedOnceTheFirstSucceeded() async {
        let model = model()
        await readyToCreate(model)
        #expect(await model.create() != nil)
        #expect(await model.create() == nil)
        #expect(services.calls.filter { if case .create = $0 { true } else { false } }.count == 1)
    }
}
