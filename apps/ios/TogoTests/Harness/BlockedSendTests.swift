import Foundation
import Testing
import TogoKit
@testable import Togo

/// Error and blocked-state handling: a refused send says why, an error or exit ends the
/// working row, and a turn that never ran does not read as "Worked 0s".
@MainActor struct BlockedSendTests {
    private let t0 = Date(timeIntervalSince1970: 1_793_000_000)

    private func notice(_ n: HarnessItem.Notice, at: Date) -> HarnessItem {
        HarnessItem(id: "n", seq: 1, at: WireDate(at), body: .notice(n))
    }

    @Test func aRefusalNamesTheQuestion() {
        let why = "Aider is asking “Login to OpenRouter or create a free account?”. Nothing was typed: answer it first, then send again."
        let error = HelperError.rpc(RPCErrorObject(code: RPCErrorCode.refused, message: "refused",
                                                   data: .object(["reason": .string("fallback"), "message": .string(why)])))
        #expect(CommandLineLogic.notSentNotice(error) == ComposerCopy.notSent + ". " + why)
        #expect(CommandLineLogic.notSentNotice(HelperError.closed) == ComposerCopy.notSent)
    }

    @Test func anErrorNoticeEndsTheSentHold() {
        let err = notice(.init(level: "error", code: "error", reason: "auth", text: "401"), at: t0 + 2)
        #expect(WorkingLogic.awaitingEnded(awaitingSince: t0, wasWorking: false, working: false, items: [err], now: t0 + 3))
        let ended = notice(.init(level: "info", code: HarnessItem.Notice.ended, text: "Aider exited."), at: t0 + 2)
        #expect(WorkingLogic.awaitingEnded(awaitingSince: t0, wasWorking: false, working: false, items: [ended], now: t0 + 3))
        let old = notice(.init(level: "error", code: "error", text: "earlier"), at: t0 - 10)
        #expect(!WorkingLogic.awaitingEnded(awaitingSince: t0, wasWorking: false, working: false, items: [old], now: t0 + 3))
    }

    @Test func aTurnThatNeverRanCountsStepsOnly() {
        let instant = WorkSummary(running: false, startedAt: t0, endedAt: t0, steps: 2, filesChanged: 0, failed: false)
        #expect(ChatCopy.workLine(instant, now: t0 + 5) == ChatCopy.steps(2))
        let real = WorkSummary(running: false, startedAt: t0, endedAt: t0 + 12, steps: 2, filesChanged: 0, failed: false)
        #expect(ChatCopy.workLine(real, now: t0 + 20).hasPrefix("Worked"))
    }

    @Test func errorNoticesHaveAPlainLead() {
        #expect(NoticeCopy.lead(.init(level: "error", code: "error", reason: "provider_unreachable", text: "x")) == "Couldn't reach the model")
        #expect(HarnessItem.Notice(level: "error", code: "error", text: "x").isError)
        #expect(!HarnessItem.Notice(level: "info", code: "compacted", text: "x").isError)
    }
}
