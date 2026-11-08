import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct IntegrationsModelTests {
    private let host = Samples.studio

    private func row(_ id: String, _ state: String, setup: String = "hooks", installed: Bool = true, extra: String = "") -> String {
        #"{"id":"\#(id)","name":"\#(id.capitalized)","installed":\#(installed),"setup":"\#(setup)","state":"\#(state)"\#(extra)}"#
    }

    private func status(_ rows: [String]) -> String {
        #"{"integrations":[\#(rows.joined(separator: ","))]}"#
    }

    private func tools() -> FakeMachineTools {
        let tools = FakeMachineTools()
        tools.defaultCapabilities = [Capability.integrations]
        return tools
    }

    @Test func rowsSplitIntoSetupAndNothingNeeded() async {
        let tools = tools()
        tools.answer("integrations.status", status([
            row("claude", "ready", extra: #","hooks":"installed","statusLine":"chained""#),
            row("droid", "needsSetup", extra: #","hooks":"absent""#),
            row("vibe", "notSupported", setup: "pending"),
            row("kimi", "notNeeded", setup: "none"),
            row("aider", "notInstalled", setup: "none", installed: false),
        ]))
        let model = IntegrationsModel(host: host, tools: tools)
        await model.load()
        #expect(model.phase == .loaded)
        #expect(model.setupRows.map(\.id) == ["claude", "droid"])
        #expect(model.notNeededRows.map(\.id) == ["vibe", "kimi"])
        #expect(model.needing.map(\.id) == ["droid"])
        #expect(!model.showsSetUpAll)
    }

    @Test func setUpAllShowsWhenMoreThanOneNeedsIt() async {
        let tools = tools()
        tools.answer("integrations.status", status([row("droid", "needsSetup"), row("codex", "outdated"), row("claude", "ready")]))
        let model = IntegrationsModel(host: host, tools: tools)
        await model.load()
        #expect(model.showsSetUpAll)
        tools.answer("integrations.install", #"{"results":[],"integrations":[\#(row("droid", "ready")),\#(row("codex", "ready")),\#(row("claude", "ready"))]}"#)
        await model.setUpAll()
        let sent = tools.requests.filter { $0.method == "integrations.install" }.map(\.params)
        #expect(sent == [["agents": .array([.string("droid"), .string("codex")])]])
        #expect(!model.showsSetUpAll && model.needing.isEmpty)
    }

    @Test func setUpTurnsTheRowReady() async {
        let tools = tools()
        let model = IntegrationsModel(host: host, tools: tools, reports: [
            IntegrationReport(id: "droid", name: "Droid", installed: true, setup: .hooks, state: .needsSetup),
        ])
        tools.answer("integrations.install", #"{"results":[{"id":"droid","state":"installed"}],"integrations":[\#(row("droid", "ready"))]}"#)
        await model.setUp("droid")
        #expect(model.setupRows.first?.state == .ready)
        #expect(model.failures.isEmpty && model.busy.isEmpty)
    }

    @Test func aRefusedHarnessShowsWhyInPlace() async {
        let tools = tools()
        let model = IntegrationsModel(host: host, tools: tools, reports: [
            IntegrationReport(id: "droid", name: "Droid", installed: true, setup: .hooks, state: .needsSetup),
        ])
        tools.answer("integrations.install", #"{"results":[{"id":"droid","state":"refused","error":"a Togo hook entry was changed by hand\nmore"}],"integrations":[\#(row("droid", "needsSetup"))]}"#)
        await model.setUp("droid")
        #expect(model.failures["droid"] == "a Togo hook entry was changed by hand")
    }

    @Test func aFailedCallSaysSoOnTheRow() async {
        let tools = tools()
        let model = IntegrationsModel(host: host, tools: tools, reports: [
            IntegrationReport(id: "droid", name: "Droid", installed: true, setup: .hooks, state: .needsSetup),
        ])
        tools.fail("integrations.install", code: -32603)
        await model.setUp("droid")
        #expect(model.failures["droid"] == "Couldn't set up Droid. Try again.")
        #expect(model.setupRows.first?.state == .needsSetup)
    }

    @Test func uninstallSendsTheOneHarness() async {
        let tools = tools()
        let model = IntegrationsModel(host: host, tools: tools, reports: [
            IntegrationReport(id: "codex", name: "Codex", installed: true, setup: .hooks, state: .ready, hooks: "installed"),
        ])
        tools.answer("integrations.uninstall", #"{"results":[{"id":"codex","state":"removed"}],"integrations":[\#(row("codex", "needsSetup"))]}"#)
        await model.uninstall("codex")
        #expect(tools.requests.map(\.method) == ["integrations.uninstall"])
        #expect(model.needing.map(\.id) == ["codex"])
    }

    @Test func anOlderHelperIsUnsupported() async {
        let tools = FakeMachineTools()
        let model = IntegrationsModel(host: host, tools: tools)
        await model.load()
        #expect(model.phase == .unsupported)
    }

    @Test func aFailedFirstReadCanBeRetried() async {
        let tools = tools()
        tools.fail("integrations.status", code: -32603)
        let model = IntegrationsModel(host: host, tools: tools)
        await model.load()
        #expect(model.phase == .failed)
        tools.answer("integrations.status", status([row("claude", "ready")]))
        await model.load()
        #expect(model.phase == .loaded)
    }

    @Test func stateWords() {
        #expect(IntegrationsCopy.word(.ready) == "Ready")
        #expect(IntegrationsCopy.word(.needsSetup) == "Needs setup")
        #expect(IntegrationsCopy.word(.unknown("x")) == "Needs setup")
        #expect(IntegrationsCopy.word(.outdated) == "Outdated")
        #expect(IntegrationsCopy.word(.notNeeded) == "Not needed")
        #expect(IntegrationsModel.needsAction(.unknown("x")))
        #expect(!IntegrationsModel.needsAction(.notSupported))
        let chained = IntegrationReport(id: "claude", name: "Claude Code", installed: true, setup: .hooks, state: .ready, statusLine: "chained")
        #expect(IntegrationsCopy.detail(chained) == "Your own status line still runs")
    }
}
