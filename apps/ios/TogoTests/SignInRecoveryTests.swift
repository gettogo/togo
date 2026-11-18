import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct SignInRecoveryTests {
    @Test func editingAnotherCodeAfterSendReenablesTheVisibleButton() async {
        let sender = FakeCodeSender()
        let model = PasteCodeModel(hostID: UUID(), sessionID: "test", sender: sender)
        model.text = "old-code"
        await model.send()
        #expect(model.phase == .sent)
        #expect(!model.canSend)
        model.text = "replacement-code"
        #expect(model.phase == .editing)
        #expect(model.canSend)
        await model.send()
        #expect(sender.sent == ["old-code", "replacement-code"])
    }

    @Test func exhaustedProviderWatchBecomesRetryableInsteadOfSpinning() async throws {
        let model = SignInPageModel(provider: "Test", itemID: "test", agent: "OMP", hostID: UUID(),
                                    sessionID: "test", send: { HarnessActionResult(result: .typed) },
                                    read: nil, refresh: nil, watchEvery: .milliseconds(1), watchChecks: 2)
        model.start()
        defer { model.leave() }
        let deadline = ContinuousClock.now + .seconds(2)
        while model.progress.watching, ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(5))
        }
        guard case .failed(let message) = model.progress.phase else {
            Issue.record("Exhausted watch must leave a visible retryable failure")
            return
        }
        #expect(message.contains("not confirmed"))
        model.retry()
        #expect(model.progress.phase == .sending)
    }

    @Test(arguments: [false, true])
    func requestIsConsumedOnlyAfterTheBrowserStarts(_ canStart: Bool) {
        let browser = FakeBrowser()
        browser.canStart = canStart
        let runner = SignInRunner(url: URL(string: "https://github.com/login/device")!, hostID: UUID(),
                                  sessionID: "test", tunnel: nil, browser: browser)
        var opened = 0
        runner.onOpened = { opened += 1 }
        runner.begin()
        #expect(opened == (canStart ? 1 : 0))
        runner.finish()
    }

    @Test func failedOrUnconfirmedSignInHasAVisibleOutcome() {
        #expect(LinkCenter.hasOutcome(.failed("forward unavailable")))
        #expect(LinkCenter.hasOutcome(.delivered))
        #expect(LinkCenter.hasOutcome(.needsCode))
        #expect(!LinkCenter.hasOutcome(.browsing))
        #expect(!LinkCenter.hasOutcome(.cancelled))
    }

    @Test func lateWatchExpiryCannotReplaceConfirmedSuccess() {
        var progress = SignInProgress()
        progress.listed(setUp: true)
        progress.expired("late timeout")
        #expect(progress.phase == .signedIn)
    }
}
