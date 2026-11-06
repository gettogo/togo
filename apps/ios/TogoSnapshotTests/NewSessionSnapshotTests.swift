import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct NewSessionSnapshotTests {
    private let services = FakeAppServices()
    private let recent = ["/Users/sam/src/app", "/Users/sam/src/site", "/Users/sam/notes"]

    private func model(
        host: HostID? = Samples.studio, machines: [MachineSection] = Samples.controlCenter.machines
    ) -> NewSessionModel {
        NewSessionModel(services: services, hostID: host, machines: machines)
    }

    private func sheet(_ model: NewSessionModel) -> some View {
        NewSessionSheet(model: model)
    }

    private func agentStep(_ agents: Load<[AgentInfo]>, host: HostID = Samples.studio) -> NewSessionModel {
        let model = model(host: host)
        model.step = .agent
        model.cwd = "/Users/sam/src/app"
        model.agents = agents
        return model
    }

    private var installed: [AgentInfo] {
        [
            AgentInfo(id: "claude", name: "Claude Code", installed: true),
            AgentInfo(id: "codex", name: "Codex", installed: false),
            AgentInfo(id: "omp", name: "OMP", installed: true),
        ]
    }

    @Test func machinesList() {
        assertScreen(sheet(model(host: nil)), named: "machines")
    }

    @Test func offlineMachine() {
        var machines = Samples.controlCenter.machines
        machines[0].helper = .missing
        assertScreen(sheet(model(host: nil, machines: machines)), named: "offline-and-no-helper")
    }

    @Test func recentLoading() {
        let model = model()
        model.recent = .loading
        assertScreen(sheet(model), named: "recent-loading")
    }

    @Test func recentEmpty() {
        let model = model()
        model.recent = .loaded([])
        assertScreen(sheet(model), named: "recent-empty")
    }

    @Test func recentError() {
        let model = model()
        model.recent = .failed("The Togo helper on the host did not answer in time.")
        assertScreen(sheet(model), named: "recent-error")
    }

    @Test func recentFolders() {
        let model = model()
        model.recent = .loaded(recent + ["/Users/sam/src/a-very-long-project-name/packages/feature-module/sources"])
        assertScreen(sheet(model), named: "recent")
    }

    @Test func browse() {
        let model = model()
        model.recent = .loaded(recent)
        model.browsing = .loaded(ListDirResult(path: "/Users/sam/src", entries: [
            DirEntry(name: "app", dir: true), DirEntry(name: "site", dir: true),
            DirEntry(name: ".cache", dir: true), DirEntry(name: "notes.txt", dir: false),
        ]))
        model.step = .browse
        assertScreen(sheet(model), named: "browse")
    }

    @Test func agentsLoading() {
        assertScreen(sheet(agentStep(.loading)), named: "agents-loading")
    }

    @Test func agentsFailed() {
        assertScreen(sheet(agentStep(.failed("The Togo helper on the host did not answer in time."))), named: "agents-error")
    }

    @Test func agentNotInstalled() {
        assertScreen(sheet(agentStep(.loaded(installed))), named: "agents")
    }

    @Test func creating() {
        let model = agentStep(.loaded(installed))
        model.agentID = "claude"
        model.creating = true
        assertScreen(sheet(model), named: "creating")
    }

    @Test func createError() async {
        let model = agentStep(.loaded(installed))
        services.failure = HelperError.rpc(RPCErrorObject(code: RPCErrorCode.invalidPath, message: "invalid_path"))
        _ = await model.create()
        assertScreen(sheet(model), named: "create-error")
    }

    @Test func noHelperMachine() {
        var machines = Samples.controlCenter.machines
        machines[0].helper = .missing
        assertScreen(sheet(model(machines: machines)), named: "no-helper")
    }

    @Test func longNamesAX5() {
        let model = agentStep(.loaded([AgentInfo(id: "claude", name: "Claude Code", installed: true)]))
        model.name = "refactor the payments module so retries survive restarts"
        assertScreen(sheet(model), named: "long-name")
    }
}
