import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct NewSessionRefreshTests {
    private func machine(_ reach: Reachability = .online, caps: Set<String> = []) -> MachineSection {
        MachineSection(id: Samples.studio, name: "Studio", symbol: .desktop, reachability: reach,
                       helper: .present(version: "1", live: true), sessions: [], capabilities: caps)
    }

    private func preset() -> AgentPreset {
        AgentPreset(id: UUID(), name: "Custom launch", agent: "claude", args: ["--dangerously-skip-permissions"])
    }

    @Test func machineAvailabilityAndCapabilitiesRefreshWithoutReplacingTheDraft() {
        var rows = [machine(.connecting)]
        let model = NewSessionModel(services: FakeAppServices(), hostID: Samples.studio,
                                    machines: rows, machinesNow: { rows })
        model.name = "my draft"
        #expect(model.machine?.unavailable != nil)
        #expect(!model.flagsAvailable)
        rows[0].reachability = .online
        rows[0].capabilities = [Capability.launch]
        #expect(model.machine?.unavailable == nil)
        #expect(model.flagsAvailable)
        #expect(model.name == "my draft")
        rows[0].helper = .missing
        #expect(model.helperMissing)
    }

    @Test func requestedPresetWaitsForItsLibraryAndMachineCapability() async {
        var rows = [machine()]
        let library = AgentLibrary.inMemory()
        let preset = preset()
        let model = NewSessionModel(services: FakeAppServices(), hostID: Samples.studio,
                                    machines: rows, machinesNow: { rows }, library: library)
        model.apply(NewSessionPrefill(hostID: Samples.studio, folder: "/tmp/project", presetID: preset.id))
        await model.start()
        #expect(model.selectedPreset == nil)
        await library.save(preset)
        await model.refreshPresetEligibility()
        #expect(model.selectedPreset == nil)
        rows[0].capabilities = [Capability.launch]
        await model.refreshPresetEligibility()
        #expect(model.selectedPreset?.id == preset.id)
        #expect(model.flags == "--dangerously-skip-permissions")
    }

    @Test func aPlainAgentSuggestionNeverAcquiresASameAgentPreset() async {
        let library = AgentLibrary.inMemory([preset()])
        await library.remember(library.presets[0].id, host: Samples.studio, cwd: "/tmp/project")
        let model = NewSessionModel(services: FakeAppServices(), hostID: Samples.studio,
                                    machines: [machine(caps: [Capability.launch])], library: library)
        model.apply(NewSessionPrefill(hostID: Samples.studio, folder: "/tmp/project", presetID: nil, agentID: "claude"))
        await model.start()
        await model.refreshPresetEligibility()
        #expect(model.selectedRegistryID == "claude")
        #expect(model.presetID == nil)
        #expect(model.flags.isEmpty)
    }

    @Test func anUnavailableExplicitAgentCannotFallBackToAnotherProgram() async {
        let model = NewSessionModel(services: FakeAppServices(), hostID: Samples.studio,
                                    machines: [machine(caps: [Capability.launch])])
        model.apply(NewSessionPrefill(hostID: Samples.studio, folder: "/tmp/project", presetID: nil,
                                     agentID: "not-installed"))
        await model.start()
        #expect(model.selectedAgent == nil)
        #expect(!model.canStart)
    }

    @Test func capabilityLossKeepsManualFlagsAndRefusesToDropThemOnStart() async {
        var rows = [machine(caps: [Capability.launch])]
        let model = NewSessionModel(services: FakeAppServices(), hostID: Samples.studio,
                                    machines: rows, machinesNow: { rows })
        await model.choose(folder: "/tmp/project")
        model.flags = "--model custom"
        #expect(model.canStart)
        rows[0].capabilities = []
        #expect(!model.canStart)
        #expect(model.flags == "--model custom")
    }
}
