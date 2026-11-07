import Foundation
import Testing
@testable import TogoKit

/// What each recorded scenario must show, for every harness that has a recording of it. The
/// fixture name says the scenario: `<agent>.<scenario>.jsonl`.
@Suite("Replay scenarios")
struct ReplayScenarioTests {
    static func named(_ scenario: String) -> [String] {
        ReplayFixture.all.filter { $0.split(separator: ".").dropFirst().first.map(String.init) == scenario }
    }

    /// Approving from the phone: the request leaves the prompt bar and the chat card resolves,
    /// and the tool it held runs.
    @Test("Approve", arguments: named("approve"))
    func approve(_ name: String) throws {
        try ReplayKnownIssues.run(name, "scenario") { try checkApprove(name) }
    }

    private func checkApprove(_ name: String) throws {
        let r = try Replay.run(name)
        guard let answer = r.request("approval.answer") else {
            Issue.record("\(name): no approval was answered (agent asked nothing?)")
            return
        }
        let before = r.after(answer)
        #expect(!before.pending.isEmpty, "\(name): nothing pending when the phone answered")
        #expect(!before.openNeeds.isEmpty, "\(name): no open card in the chat when the phone answered")
        let settled = r.first(from: answer) { $0.pending.isEmpty && $0.openApprovalRequests.isEmpty }
        #expect(settled != nil, "\(name): the answered request never left the prompt bar and the chat")
        if let settled { #expect(r.t(settled) - r.t(answer) <= ReplayInvariants.grace) }
        let tools = r.final.main.compactMap { item -> HarnessItem.Tool? in
            if case .tool(let tool) = item.body { tool } else { nil }
        }
        #expect(tools.contains { $0.state == .ok }, "\(name): no tool ran after the approval")
    }

    /// Denying: the request goes, the tool shows denied, the chat card resolves.
    @Test("Deny", arguments: named("deny"))
    func deny(_ name: String) throws {
        try ReplayKnownIssues.run(name, "scenario") { try checkDeny(name) }
    }

    private func checkDeny(_ name: String) throws {
        let r = try Replay.run(name)
        guard let answer = r.request("approval.answer") else {
            Issue.record("\(name): no approval was answered")
            return
        }
        #expect(r.first(from: answer) { $0.pending.isEmpty && $0.openNeeds.isEmpty } != nil)
        let ran = r.final.main.contains { item in
            if case .tool(let tool) = item.body { return tool.state == .ok && r.isAfter(item, answer) }
            return false
        }
        #expect(!ran, "\(name): a tool ran after the deny")
    }

    /// A multi-question ask: answered in one action, every question answered, and no second
    /// (approval) card for the same prompt.
    @Test("Multi-question", arguments: named("question"))
    func question(_ name: String) throws {
        try ReplayKnownIssues.run(name, "scenario") { try checkQuestion(name) }
    }

    private func checkQuestion(_ name: String) throws {
        let r = try Replay.run(name)
        guard let answer = r.request("harness.action") else {
            Issue.record("\(name): the question never reached the chat")
            return
        }
        let asked = r.after(answer).openNeeds.compactMap { item -> HarnessItem.Question? in
            if case .question(let q) = item.body { q } else { nil }
        }
        #expect(asked.first.map { $0.questions.count >= 2 } ?? false, "\(name): not a multi-question ask")
        #expect(r.answerError(after: answer) == nil, "\(name): answering failed: \(r.answerError(after: answer) ?? "")")
        let answered = r.final.items.compactMap { item -> HarnessItem.Question? in
            if case .question(let q) = item.body { q } else { nil }
        }.last
        #expect((answered?.answered ?? [:]).count == asked.first?.questions.count, "\(name): not every question answered")
        #expect(!r.ever { s in s.openNeeds.contains { if case .approval = $0.body { true } else { false } } },
                "\(name): an approval card showed for the question")
    }

    /// Stop: after the interrupt, the turn ends and no new work starts on its own.
    @Test("Interrupt", arguments: named("interrupt"))
    func interrupt(_ name: String) throws {
        try ReplayKnownIssues.run(name, "scenario") { try checkInterrupt(name) }
    }

    private func checkInterrupt(_ name: String) throws {
        let r = try Replay.run(name)
        guard let stop = r.request("harness.interrupt") else {
            Issue.record("\(name): never interrupted (the turn never started?)")
            return
        }
        #expect(r.answerError(after: stop) == nil)
        // The pill's word: the mission summary.
        let idle = r.first(from: stop) { !$0.feed.mission.summary.working }
        #expect(idle != nil, "\(name): still working after the interrupt")
        if let idle, let resumed = r.first(from: idle, where: { $0.feed.mission.summary.working }),
           !r.sent(between: stop, and: resumed) {
            Issue.record("\(name): working again \(r.t(resumed) - r.t(stop))ms after the interrupt, without a new prompt")
        }
        #expect(r.final.workingSignals.isEmpty, "\(name): still working at the end: \(r.final.workingSignals)")
    }

    /// A model switch from the picker: the chip (status model) shows the new model, and the
    /// session is idle again afterwards.
    @Test("Model switch", arguments: named("model"))
    func model(_ name: String) throws {
        try ReplayKnownIssues.run(name, "scenario") { try checkModel(name) }
    }

    private func checkModel(_ name: String) throws {
        let r = try Replay.run(name)
        guard let action = r.request("harness.action"),
              case .request(_, let params) = r.fixture.records[action].step,
              case .object(let p) = params, case .string(let target)? = p["target"] else {
            Issue.record("\(name): no model switch in the recording")
            return
        }
        let before = r.after(action).status?.model ?? ""
        let switched = r.first(from: action) { ($0.status?.model ?? "") != before && !($0.status?.model ?? "").isEmpty }
        #expect(switched != nil, "\(name): the model chip never changed from \(before) after choosing \(target)")
        if let switched {
            #expect(r.first(from: switched) { $0.workingSignals.isEmpty } != nil, "\(name): still working after the switch")
        }
        #expect((r.fixture.header.configChanged ?? []).isEmpty,
                "\(name): switching the model changed the agent's own config \(r.fixture.header.configChanged ?? [])")
    }

    /// A background subagent: mission control counts it while it runs and not after.
    @Test("Background subagent", arguments: named("subagent"))
    func subagent(_ name: String) throws {
        try ReplayKnownIssues.run(name, "scenario") { try checkSubagent(name) }
    }

    private func checkSubagent(_ name: String) throws {
        let r = try Replay.run(name)
        #expect(r.ever { $0.mission?.summary.agents ?? 0 > 0 }, "\(name): mission control never showed the subagent")
        #expect(r.final.mission?.summary.agents == 0, "\(name): the subagent still counts at the end")
        #expect(r.final.main.contains { if case .agent = $0.body { true } else { false } })
    }

    /// An error, a sign-in prompt or a refused start says so in the chat, and nothing works.
    @Test("Error or sign-in", arguments: named("error") + named("signin"))
    func failure(_ name: String) throws {
        try ReplayKnownIssues.run(name, "scenario") { try checkFailure(name) }
    }

    private func checkFailure(_ name: String) throws {
        let r = try Replay.run(name)
        let said = r.final.items.contains { item in
            switch item.body {
            case .notice, .approval, .question: true
            default: false
            }
        }
            || r.fixture.records.contains { if case .answer(_, let e) = $0.step { e != nil } else { false } }
        #expect(said, "\(name): the failure never reached the chat")
        #expect(r.final.workingSignals.isEmpty, "\(name): working after the failure: \(r.final.workingSignals)")
    }
}

extension Replay {
    /// The error of the answer to request `index`, if it failed.
    func answerError(after index: Int) -> String? {
        guard case .request(let method, _) = fixture.records[index].step else { return nil }
        for record in fixture.records[(index + 1)...] {
            if case .answer(let m, let error) = record.step, m == method { return error }
        }
        return nil
    }

    /// Whether `item` first arrived after record `index`.
    func isAfter(_ item: HarnessItem, _ index: Int) -> Bool {
        after(index).items.allSatisfy { $0.id != item.id }
    }

    /// Whether the app sent a prompt between records `start` and `end`.
    func sent(between start: Int, and end: Int) -> Bool {
        fixture.records[start...end].contains { if case .request(let m, _) = $0.step { m == "harness.send" } else { false } }
    }
}
