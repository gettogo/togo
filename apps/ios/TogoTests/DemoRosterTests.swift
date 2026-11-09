import Foundation
import Testing
import TogoKit
@testable import Togo

/// The busy demo roster the website's captures launch with, and the switches that pick it.
@MainActor struct DemoRosterTests {
    @Test func launchSwitches() {
        let plain = UITestLaunch(arguments: ["-ui-fakes"])
        #expect(!plain.demoRoster)
        #expect(!plain.noKeyboard)
        let demo = UITestLaunch(arguments: ["-ui-fakes", "-ui-demo-roster", "-ui-no-keyboard"])
        #expect(demo.demoRoster)
        #expect(demo.noKeyboard)
    }

    /// Two machines, nine sessions across eight agents: 2 need you, 3 working, 2 done, 2 idle.
    @Test func demoIsBusyAndCredible() {
        let state = Samples.demoControlCenter
        #expect(state.machines.map(\.name) == ["Studio", "build-box"])
        let rows = state.machines.flatMap(\.sessions)
        #expect(rows.count == 9)
        #expect(Set(rows.map(\.agent)).count == 8)
        #expect(rows.allSatisfy { AgentCatalog.entry($0.agent) != nil })
        #expect(Set(rows.map(\.bib)).count == rows.count)
        let roster = RosterModel.make(machines: state.machines, requests: state.requests)
        #expect(roster.headline == "2 need you")
        #expect(roster.tallyText.contains("3 working"))
        #expect(roster.tallyText.contains("2 idle"))
        #expect(roster.tallyText.contains("2 done"))
    }

    /// The UI-test machines serve the demo's two machines, not the usual samples.
    @Test func fixtureServesTheDemoMachines() {
        let machines = UITestMachines(firstRun: false, oneMachine: true, demoRoster: true)
        #expect(machines.services.hosts.map(\.name) == ["Studio", "build-box"])
    }
}
