import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct WorkingStepTests {
    private func item(_ id: String, _ body: HarnessItem.Body, turn: String = "current") -> HarnessItem {
        .init(id: id, seq: 1, turnId: turn, at: HarnessSamples.at, body: body)
    }
    private func tool(_ id: String, category: HarnessItem.ToolCategory = .shell, state: HarnessState = .ok,
                      subject: String = "npm test", turn: String = "current") -> HarnessItem {
        item(id, .tool(.init(callId: id, name: "tool", title: "Run checks", category: category,
                            subject: .init(kind: .command, text: subject), state: state, startedAt: HarnessSamples.at)), turn: turn)
    }
    private func state(_ items: [HarnessItem], needs: Int = 0) throws -> WorkingState {
        try #require(WorkingLogic.state(items: items, working: true, awaitingSince: nil, localSince: nil,
                                       now: HarnessSamples.at.date, needsYou: needs))
    }

    @Test func runningWorkWinsEvenWhenANewerStepHasFinished() throws {
        let result = try state([tool("running", state: .running), tool("finished")])
        #expect(result.step == "Running npm test" && !result.completedStep)
    }

    @Test(arguments: [HarnessItem.ToolCategory.read, .edit, .write, .search, .shell, .web])
    func completedStepsUsePastTense(category: HarnessItem.ToolCategory) throws {
        let result = try state([tool("finished", category: category, subject: "subject")])
        let expected: [HarnessItem.ToolCategory: String] = [.read: "Read subject", .edit: "Edited subject",
            .write: "Wrote subject", .search: "Searched subject", .shell: "Ran subject", .web: "Fetched subject"]
        #expect(result.step == expected[category] && result.completedStep)
    }

    @Test func anUnnamedRunningStepStillShowsThinking() throws {
        let unnamed = item("unnamed", .tool(.init(callId: "unnamed", name: "tool", title: " ", category: .mcp,
                                                 state: .running, startedAt: HarnessSamples.at)))
        #expect(try state([unnamed]).step == WorkingCopy.thinking)
    }

    @Test func failedAndCancelledStepsNeverClaimSuccess() throws {
        #expect(try state([tool("failed", state: .failed)]).step == "Failed: Run checks")
        #expect(try state([tool("cancelled", state: .cancelled)]).step == "Stopped: Run checks")
    }

    @Test func aNewPromptOrTurnStartsThinkingAndExcludesSubagentSteps() throws {
        let prompt = item("prompt", .user(.init(text: "next", queued: false, source: "phone")))
        #expect(try state([tool("old"), prompt]).step == WorkingCopy.thinking)
        let next = item("turn", .turn(.init(state: .running, startedAt: HarnessSamples.at, endedAt: nil, steps: 0, filesChanged: 0)), turn: "next")
        #expect(try state([tool("old"), next]).step == WorkingCopy.thinking)
        var subagent = tool("child", state: .running); subagent.parent = "agent"
        #expect(try state([prompt, subagent]).step == WorkingCopy.thinking)
        let ended = item("ended", .turn(.init(state: .ok, startedAt: HarnessSamples.at, endedAt: HarnessSamples.at, steps: 1, filesChanged: 0)))
        #expect(try state([tool("old"), ended]).step == WorkingCopy.thinking)
    }

    @Test func waitingOverridesTheStepAndDoesNotDimAnOldSuccess() throws {
        let result = try state([tool("done")], needs: 1)
        #expect(result.waiting && result.step == nil && !result.completedStep)
    }

    @Test func allStepStatesKeepOneConstantHeight() throws {
        let states = try [state([]), state([tool("running", state: .running)]), state([tool("done")]),
                          state([tool("long", subject: String(repeating: "path/", count: 100))]), state([], needs: 1)]
        let heights = states.map { state in
            UIHostingController(rootView: LiveLine(state: state)).sizeThatFits(in: CGSize(width: 280, height: 1000)).height
        }
        #expect(Set(heights).count == 1)
    }
}
