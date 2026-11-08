import Foundation
import Testing
@testable import TogoKit

/// Sign-in pages as each agent's login opens them (audit2/login-flows.md).
struct SignInURLTests {
    static let claude = URL(string:
        "https://claude.ai/oauth/authorize?code=true&client_id=9d1c250a&response_type=code"
        + "&redirect_uri=http%3A%2F%2Flocalhost%3A54545%2Fcallback&scope=org%3Acreate_api_key&state=abc")!

    @Test func claudeReturnsToItsPortAndOffersACode() {
        let page = SignInURL(Self.claude)
        #expect(page.callbackPort == 54545)
        #expect(page.forwardablePort == 54545)
        #expect(page.offersCode)
        #expect(page.provider == "Anthropic")
        #expect(SignInRoute.isSignIn(Self.claude))
    }

    @Test func eachAgentsCallbackPort() {
        let cases: [(String, Int?, String)] = [
            // Codex and Crush (ChatGPT): fixed 1455.
            ("https://auth.openai.com/oauth/authorize?redirect_uri=http://localhost:1455/auth/callback&client_id=app", 1455, "OpenAI"),
            // Google: 127.0.0.1 on a random port, encoded.
            ("https://accounts.google.com/o/oauth2/v2/auth?redirect_uri=http%3A%2F%2F127.0.0.1%3A41231%2Foauth2callback", 41231, "Google"),
            // Copilot: GitHub, 127.0.0.1 random port.
            ("https://github.com/login/oauth/authorize?client_id=x&redirect_uri=http%3A%2F%2F127.0.0.1%3A50123%2Fcallback", 50123, "GitHub"),
            // Auggie.
            ("https://auth.augmentcode.com/authorize?redirect_uri=http%3A%2F%2F127.0.0.1%3A61000%2Fcb", 61000, "Augment"),
            // Aider: OpenRouter's callback_url.
            ("https://openrouter.ai/auth?callback_url=http://localhost:8484/callback/aider", 8484, "OpenRouter"),
            // Device and hosted flows return nowhere on the machine.
            ("https://github.com/login/device", nil, "GitHub"),
            ("https://app.factory.ai/device?user_code=ABCD-1234", nil, "Factory"),
            ("https://ampcode.com/auth/cli-login?authToken=x", nil, "Amp"),
            ("https://cursor.com/loginDeepControl?challenge=x&uuid=y", nil, "Cursor"),
        ]
        for (raw, port, provider) in cases {
            let page = SignInURL(URL(string: raw)!)
            #expect(page.callbackPort == port, "\(raw)")
            #expect(page.provider == provider, "\(raw)")
            #expect(SignInRoute.isSignIn(page.url), "\(raw)")
            #expect(!page.offersCode)
        }
    }

    @Test func aPortBelow1024CanOnlyTakeACode() {
        let page = SignInURL(URL(string: "https://x.example/oauth?redirect_uri=http%3A%2F%2Flocalhost%2Fcb")!)
        #expect(page.callbackPort == 80)
        #expect(page.forwardablePort == nil)
        let flow = WebAuthFlow(page)
        #expect(!flow.needsForward)
        #expect(flow.codeOnly && flow.offersCode)
    }

    @Test func ordinaryPagesAreNotSignIns() {
        for raw in ["https://docs.example.com/guide", "https://github.com/gettogo/togo/pull/3", "https://example.com/?q=1"] {
            #expect(!SignInRoute.isSignIn(URL(string: raw)!), "\(raw)")
        }
    }

    @Test func codeFlagReadsOnlyTrue() {
        #expect(!SignInURL(URL(string: "https://claude.ai/oauth/authorize?code=false")!).offersCode)
        #expect(SignInURL(URL(string: "https://claude.ai/oauth/authorize?code=1")!).offersCode)
    }
}

struct WebAuthFlowTests {
    static let claude = SignInURL(SignInURLTests.claude)
    static let device = SignInURL(URL(string: "https://github.com/login/device")!)

    @Test func callbackThroughTheForwardSignsIn() {
        var flow = WebAuthFlow(Self.claude)
        #expect(flow.start() == [.openForward])
        #expect(flow.phase == .preparing)
        // The browser opens only once the forward listens.
        #expect(flow.handle(.forwardReady) == [.openBrowser])
        #expect(flow.phase == .browsing)
        #expect(flow.handle(.callbackSeen) == [.settleLater])
        #expect(flow.phase == .delivered)
        #expect(flow.handle(.settled) == [.closeBrowser, .stopForward])
        #expect(flow.phase == .signedIn)
        // The cancel from code comes back as the browser ending; nothing more happens.
        #expect(flow.handle(.browserEnded) == [])
        #expect(flow.phase == .signedIn)
    }

    @Test func busyPortFallsBackToTheCode() {
        var flow = WebAuthFlow(Self.claude)
        _ = flow.start()
        #expect(flow.handle(.forwardBusy) == [.openBrowser])
        #expect(flow.phase == .needsCode)
        #expect(flow.handle(.browserEnded) == [])
        #expect(flow.phase == .needsCode)
        #expect(flow.handle(.codeSent) == [.stopForward])
        #expect(flow.phase == .signedIn)
    }

    @Test func closingTheBrowserFirstOffersTheCodeWhenThereIsOne() {
        var flow = WebAuthFlow(Self.claude)
        _ = flow.start()
        _ = flow.handle(.forwardReady)
        #expect(flow.handle(.browserEnded) == [])
        #expect(flow.phase == .needsCode)
    }

    @Test func closingTheBrowserWithoutACodeCancels() {
        let codex = SignInURL(URL(string: "https://auth.openai.com/oauth/authorize?redirect_uri=http://localhost:1455/auth/callback")!)
        var flow = WebAuthFlow(codex)
        _ = flow.start()
        _ = flow.handle(.forwardReady)
        #expect(flow.handle(.browserEnded) == [.stopForward])
        #expect(flow.phase == .cancelled)
        #expect(flow.isOver)
        #expect(flow.handle(.reopen) == [.stopForward, .openForward])
        #expect(flow.phase == .preparing)
    }

    @Test func providerSetUpEndsItFromAnyStep() {
        var flow = WebAuthFlow(Self.claude)
        _ = flow.start()
        _ = flow.handle(.forwardReady)
        #expect(flow.handle(.providerSetUp) == [.closeBrowser, .stopForward])
        #expect(flow.phase == .signedIn)
        #expect(flow.handle(.providerSetUp) == [])
    }

    @Test func closingAfterTheCallbackStillSignsIn() {
        var flow = WebAuthFlow(Self.claude)
        _ = flow.start()
        _ = flow.handle(.forwardReady)
        _ = flow.handle(.callbackSeen)
        #expect(flow.handle(.browserEnded) == [.stopForward])
        #expect(flow.phase == .signedIn)
    }

    @Test func lostForwardWithACodeWaitsForIt() {
        var flow = WebAuthFlow(Self.claude)
        _ = flow.start()
        _ = flow.handle(.forwardReady)
        #expect(flow.handle(.forwardLost("gone")) == [])
        #expect(flow.phase == .needsCode)
    }

    @Test func lostForwardWithoutACodeFails() {
        let google = SignInURL(URL(string: "https://accounts.google.com/o/oauth2/v2/auth?redirect_uri=http%3A%2F%2F127.0.0.1%3A41231%2Fcb")!)
        var flow = WebAuthFlow(google)
        _ = flow.start()
        _ = flow.handle(.forwardReady)
        #expect(flow.handle(.forwardLost("gone")) == [.closeBrowser, .stopForward])
        #expect(flow.phase == .failed("gone"))
    }

    @Test func deviceCodePagesOpenAtOnce() {
        var flow = WebAuthFlow(Self.device)
        #expect(!flow.needsForward)
        #expect(flow.start() == [.openBrowser])
        #expect(flow.phase == .browsing)
        #expect(flow.handle(.browserEnded) == [.stopForward])
        #expect(flow.phase == .cancelled)
    }

    @Test func withoutATunnelOnlyTheCodeIsLeft() {
        var flow = WebAuthFlow(Self.claude, forwardless: true)
        #expect(flow.start() == [.openBrowser])
        #expect(flow.phase == .needsCode)
    }
}

/// Stands in for the helper's SSH connection, recording how it was asked for.
@MainActor final class FakeTunnel: CallbackTunnel {
    var openers: [FakeOpener]
    private(set) var asked: [(host: UUID, fresh: Bool)] = []
    var address = "127.0.0.1"
    var failFresh = false

    init(_ openers: [FakeOpener]) {
        self.openers = openers
    }

    func opener(hostID: UUID, fresh: Bool) async throws -> any TCPChannelOpener {
        asked.append((hostID, fresh))
        if fresh, failFresh { throw SSHError.channelClosed(exitStatus: nil) }
        return openers.count > 1 ? openers.removeFirst() : openers[0]
    }

    func loopbackAddress(hostID: UUID, sessionID: String?, port: Int) async -> String { address }
}

/// Polls a main-actor condition until it holds.
@MainActor func settles(_ timeout: Duration = .seconds(3), _ condition: @MainActor () -> Bool) async -> Bool {
    let deadline = ContinuousClock.now + timeout
    while ContinuousClock.now < deadline {
        if condition() { return true }
        try? await Task.sleep(for: .milliseconds(5))
    }
    return condition()
}

@MainActor struct CallbackForwardTests {
    static let host = UUID()

    private func get(_ port: Int) async throws -> String {
        let url = try #require(URL(string: "http://127.0.0.1:\(port)/callback?code=abc&state=s"))
        return String(decoding: try await URLSession(configuration: .ephemeral).data(from: url).0, as: UTF8.self)
    }

    /// The callback goes down a direct-tcpip channel on the tunnel's SSH connection to the
    /// machine's own loopback port: the forward never learns or dials the machine's address,
    /// so it works on whichever route the connection took (LAN, Tailscale).
    @Test func callbackRidesTheTunnelsConnectionToTheMachinesLoopback() async throws {
        let opener = FakeOpener(.open(.http("Signed in")))
        let tunnel = FakeTunnel([opener])
        let port = LoopbackSocket.freePort(in: 30_000...39_999)
        let forward = CallbackForward(tunnel: tunnel, hostID: Self.host, sessionID: "s1", port: port)
        var seen = 0
        forward.onCallback = { seen += 1 }
        #expect(try await forward.start())
        #expect(forward.state == .listening)
        #expect(try await get(port) == "Signed in")
        #expect(opener.targets == ["127.0.0.1:\(port)"])
        #expect(tunnel.asked.map(\.host) == [Self.host])
        #expect(tunnel.asked.map(\.fresh) == [false])
        let settled = await settles { seen >= 1 }
        #expect(settled)
        await forward.stop()
        #expect(LoopbackSocket.connect(port: port) == nil)
    }

    @Test func agentOnIPv6LoopbackIsReachedThere() async throws {
        let opener = FakeOpener(.open(.http("ok")))
        let tunnel = FakeTunnel([opener])
        tunnel.address = "::1"
        let port = LoopbackSocket.freePort(in: 30_000...39_999)
        let forward = CallbackForward(tunnel: tunnel, hostID: Self.host, sessionID: nil, port: port)
        #expect(try await forward.start())
        _ = try await get(port)
        #expect(opener.targets == ["::1:\(port)"])
        await forward.stop()
    }

    @Test func takenPortReportsBusy() async throws {
        let holder = LocalForward(opener: FakeOpener(.open(.hold)), target: LocalForwardTests.target, preferredLocalPort: nil)
        let taken = try await holder.start()
        let forward = CallbackForward(tunnel: FakeTunnel([FakeOpener(.open(.hold))]), hostID: Self.host, sessionID: nil, port: taken)
        #expect(try await forward.start() == false)
        #expect(forward.state == .busy)
        await holder.stop()
    }

    @Test func droppedConnectionReopensOnTheSamePortOverAFreshOne() async throws {
        let first = FakeOpener(.open(.http("old")))
        let second = FakeOpener(.open(.http("new route")))
        let tunnel = FakeTunnel([first, second])
        let port = LoopbackSocket.freePort(in: 30_000...39_999)
        let forward = CallbackForward(tunnel: tunnel, hostID: Self.host, sessionID: nil, port: port, backoff: [.milliseconds(50)])
        #expect(try await forward.start())
        first.drop()
        let settled = await settles { tunnel.asked.count == 2 && forward.state == .listening }
        #expect(settled)
        #expect(tunnel.asked.map(\.fresh) == [false, true])
        #expect(try await get(port) == "new route")
        #expect(second.targets == ["127.0.0.1:\(port)"])
        await forward.stop()
    }

    @Test func machineGoneForGoodIsReported() async throws {
        let opener = FakeOpener(.open(.hold))
        let tunnel = FakeTunnel([opener])
        tunnel.failFresh = true
        let port = LoopbackSocket.freePort(in: 30_000...39_999)
        let forward = CallbackForward(tunnel: tunnel, hostID: Self.host, sessionID: nil, port: port, backoff: [.milliseconds(10)])
        var lost: CallbackForward.Loss?
        forward.onLost = { lost = $0 }
        #expect(try await forward.start())
        opener.drop()
        let settled = await settles { lost != nil }
        #expect(settled)
        if case .unreachable = lost {} else { Issue.record("expected unreachable, got \(String(describing: lost))") }
        #expect(LoopbackSocket.connect(port: port) == nil)
        await forward.stop()
    }

    @Test func stoppedForwardDoesNotReconnect() async throws {
        let opener = FakeOpener(.open(.hold))
        let tunnel = FakeTunnel([opener])
        let port = LoopbackSocket.freePort(in: 30_000...39_999)
        let forward = CallbackForward(tunnel: tunnel, hostID: Self.host, sessionID: nil, port: port, backoff: [.milliseconds(10)])
        #expect(try await forward.start())
        await forward.stop()
        opener.drop()
        try await Task.sleep(for: .milliseconds(100))
        #expect(tunnel.asked.count == 1)
        #expect(forward.state == .stopped)
    }
}
