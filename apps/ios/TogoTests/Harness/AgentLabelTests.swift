import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct AgentLabelTests {
    @Test(arguments: ["", " \n\t"])
    func missingRowTitlesUseReadableContentWithoutChangingIdentity(_ missing: String) {
        let row = HarnessRow(id: "worker-42", title: missing, subtitle: "explorer", state: .running,
                             open: HarnessOpen(kind: .chat, ref: "agent-item"))
        #expect(AgentLabel.title(row) == "explorer")
        #expect(AgentLabel.subtitle(row) == nil)
        let display = AgentLabel.resolved(row, in: HarnessTimeline())
        #expect(display.title == "explorer")
        #expect(display.id == row.id)
        #expect(display.open == row.open)
        #expect(display.state == row.state)
    }

    @Test func aNamedAgentInTheTimelineRepairsAnIncompleteRow() {
        var timeline = HarnessTimeline()
        timeline.load(HarnessOpenResult(epoch: "e1", cursor: 0, items: [], more: false,
                                        mission: HarnessMission(rev: 0, summary: MissionSummary(), sections: [])))
        let agent = HarnessItem.Agent(agentId: "worker-42", name: "Scout", role: "explorer", state: .running, startedAt: HarnessSamples.at)
        timeline.insertSent(HarnessItem(id: "agent-item", seq: 1, at: HarnessSamples.at, body: .agent(agent)))
        timeline.insertSent(HarnessItem(id: "other-agent", seq: 2, at: HarnessSamples.at,
                                        body: .agent(.init(agentId: "worker-43", name: "Reviewer", state: .running, startedAt: HarnessSamples.at))))
        let row = HarnessRow(id: "worker-42", title: "", state: .running, open: HarnessOpen(kind: .chat, ref: "agent-item"))
        let display = AgentLabel.resolved(row, in: timeline)
        #expect(display.title == "Scout")
        #expect(display.subtitle == "explorer")
        #expect(display.id == "worker-42")
        #expect(display.open == row.open)
        let named = HarnessRow(id: row.id, title: "Current name", state: .running, open: row.open)
        #expect(AgentLabel.resolved(named, in: timeline) == named)
    }

    @Test func missingNamesStayReadableInChatAndWorkingText() {
        var agent = HarnessItem.Agent(agentId: "worker-42", name: "", role: "explorer", state: .running, startedAt: HarnessSamples.at)
        #expect(AgentLabel.name(agent) == "explorer")
        #expect(AgentLabel.line(agent) == "explorer")
        agent.role = " \n"
        #expect(AgentLabel.name(agent) == "Agent worker-42")
        agent.name = "Scout"
        agent.role = "explorer"
        #expect(AgentLabel.line(agent) == "Scout  explorer")
    }

    @Test func stableIDsDistinguishUnnamedRows() {
        let first = HarnessRow(id: "worker-42", title: "", state: .running)
        let second = HarnessRow(id: "worker-43", title: "", state: .running)
        #expect(AgentLabel.title(first) == "worker-42")
        #expect(AgentLabel.title(second) == "worker-43")
    }
}
