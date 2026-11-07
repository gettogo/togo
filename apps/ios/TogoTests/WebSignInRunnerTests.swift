import Foundation
import Testing
import TogoKit
@testable import Togo

/// The authentication browser, as the runner drives it.
@MainActor final class FakeBrowser: WebAuthPresenting {
    private(set) var opened: [URL] = []
    private(set) var closes = 0
    private var ended: (@MainActor () -> Void)?

    func open(_ url: URL, ended: @escaping @MainActor () -> Void) -> Bool {
        opened.append(url)
        self.ended = ended
        return true
    }

    func close() {
        closes += 1
        userClosed()
    }

    func userClosed() {
        let ended = ended
        self.ended = nil
        ended?()
    }
}

@MainActor final class FakeCodeSender: SignInCodeSending {
    var result: CodeSendResult = .sent
    private(set) var sent: [String] = []

    func send(code: String, hostID: HostID, sessionID: String) async -> CodeSendResult {
        sent.append(code)
        return result
    }
}

@MainActor struct WebSignInRunnerTests {
    static let claude = URL(string:
        "https://claude.ai/oauth/authorize?code=true&client_id=x&redirect_uri=http%3A%2F%2Flocalhost%3A54545%2Fcallback")!

    @Test func withoutATunnelTheBrowserOpensAndTheCodeIsAsked() {
        let browser = FakeBrowser()
        let runner = SignInRunner(url: Self.claude, hostID: UUID(), sessionID: "s", tunnel: nil, browser: browser)
        runner.begin()
        #expect(browser.opened == [Self.claude])
        #expect(runner.phase == .needsCode)
        var closed = 0
        runner.onBrowserClosed = { closed += 1 }
        browser.userClosed()
        #expect(closed == 1)
        #expect(runner.phase == .needsCode)
        runner.codeSent()
        #expect(runner.phase == .signedIn)
    }

    @Test func deviceCodePageOpensWithoutAForward() {
        let browser = FakeBrowser()
        let url = URL(string: "https://github.com/login/device")!
        let runner = SignInRunner(url: url, hostID: UUID(), sessionID: "s", tunnel: nil, browser: browser)
        var end: WebAuthFlow.Phase?
        runner.onEnd = { end = $0 }
        runner.begin()
        #expect(runner.phase == .browsing)
        runner.providerSetUp()
        #expect(browser.closes == 1)
        #expect(end == .signedIn)
    }

    @Test func pastedCodeIsCleanedAndSent() async {
        let sender = FakeCodeSender()
        let model = PasteCodeModel(hostID: UUID(), sessionID: "s", sender: sender)
        var sent = 0
        model.onSent = { sent += 1 }
        model.text = "  abc123#state456\n"
        #expect(model.canSend)
        await model.send()
        #expect(sender.sent == ["abc123#state456"])
        #expect(model.phase == .sent)
        #expect(sent == 1)
    }

    @Test func codeThatDidNotSubmitSaysSo() async {
        let sender = FakeCodeSender()
        sender.result = .notSubmitted
        let model = PasteCodeModel(hostID: UUID(), sessionID: "s", sender: sender)
        model.text = "abcd"
        await model.send()
        #expect(model.phase == .failed(PasteCodeCopy.notSubmitted))
    }

    @Test func cleaningRefusesEmptyAndControlCharacters() {
        #expect(PasteCodeModel.clean("   ") == nil)
        #expect(PasteCodeModel.clean("ab\u{1b}[201~cd") == nil)
        #expect(PasteCodeModel.clean("a\nb") == nil)
        #expect(PasteCodeModel.clean(String(repeating: "x", count: 4097)) == nil)
        #expect(PasteCodeModel.clean("\tcode-1 ") == "code-1")
    }

    @Test func codeLeftOnTheInputLineIsNotSubmitted() {
        #expect(SignInCodeCheck.stillTyped("abc123", screen: "Paste code here if prompted > abc123\n\n"))
        #expect(!SignInCodeCheck.stillTyped("abc123", screen: "Paste code here if prompted > abc123\nLogin successful\n"))
        #expect(!SignInCodeCheck.stillTyped("abc", screen: "abc"))
    }
}
