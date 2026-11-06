import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct AgentPresetSnapshotTests {
    private let services = FakeAppServices()
    private let presets = [
        AgentPreset(name: "Claude, no prompts", agent: "claude", args: ["--dangerously-skip-permissions"]),
        AgentPreset(name: "Kimi", agent: AgentPreset.custom, program: "kimi", args: ["--yolo"]),
    ]
    private let agents = [
        AgentInfo(id: "claude", name: "Claude Code", installed: true),
        AgentInfo(id: "codex", name: "Codex", installed: false),
    ]

    private func agentStep(launch: Bool, remembered: Bool) -> NewSessionModel {
        var machines = Samples.controlCenter.machines
        if launch { machines[0].capabilities = [Capability.launch] }
        let model = NewSessionModel(
            services: services, hostID: Samples.studio, machines: machines, library: .inMemory(presets)
        )
        model.step = .agent
        model.cwd = "/Users/sam/src/app"
        model.agents = .loaded(agents)
        if remembered {
            model.rememberedPreset = presets[1].id
            model.choose(preset: presets[1])
            model.rememberFolder = true
        } else {
            model.flags = "--model opus"
        }
        return model
    }

    @Test func agentStepWithPresetAndFlags() {
        assertScreen(NewSessionSheet(model: agentStep(launch: true, remembered: true)), named: "agent-preset")
    }

    @Test func agentStepWithFlags() {
        assertScreen(NewSessionSheet(model: agentStep(launch: true, remembered: false)), named: "agent-flags")
    }

    @Test func agentStepWithOldHelper() {
        assertScreen(NewSessionSheet(model: agentStep(launch: false, remembered: false)), named: "agent-old-helper")
    }

    @Test func agentsList() {
        assertScreen(named: "agents-list") {
            NavigationStack {
                AgentsSettingsView(library: .inMemory(presets), machines: SettingsValues.sample.machines)
            }
            .togoControls()
        }
    }

    @Test func agentsEmpty() {
        assertScreen(named: "agents-empty") {
            NavigationStack {
                AgentsSettingsView(library: .inMemory([]), machines: SettingsValues.sample.machines)
            }
            .togoControls()
        }
    }

    @Test func editor() {
        assertScreen(named: "editor") {
            NavigationStack {
                AgentEditor(
                    preset: presets[1], machines: SettingsValues.sample.machines,
                    checks: [Samples.studio: .found("/opt/homebrew/bin/kimi"), Samples.build: .notFound],
                    save: { _ in }
                )
            }
            .togoControls()
        }
    }
}
