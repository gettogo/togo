import Foundation
import Testing
import TogoKit
@testable import Togo

/// The sign-in link's way to the user: held until used, claimed by a screen that waits for it,
/// presented only once the presentation stack is still, and the redirect's port.
@MainActor struct SignInFlowTests {
    private let host = HostID()
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private let page = "https://claude.ai/oauth/authorize?client_id=x&redirect_uri=http%3A%2F%2Flocalhost%3A54545%2Fcallback&state=s"

    private func request(_ url: String? = nil, session: String? = "s_1", age: TimeInterval = 1) -> BrowserOpen {
        BrowserOpen(sessionID: session, url: url ?? page, at: now.addingTimeInterval(-age))
    }

    // MARK: Pending requests

    @Test func freshRequestIsPresentedAndStaysPendingUntilUsed() {
        let requests = OpenRequests()
        let asked = request()
        let got = requests.receive(asked, hostID: host, agent: "OMP", now: now)
        #expect(got?.route == .present)
        #expect(requests.pending(for: TabID(hostID: host, sessionID: "s_1")).map(\.id) == [asked.id])
        #expect(requests.waitingTabs == [TabID(hostID: host, sessionID: "s_1")])
        // A snapshot read again does not ask again.
        #expect(requests.receive(asked, hostID: host, agent: "OMP", now: now) == nil)
        requests.resolve(asked.id)
        #expect(requests.pending.isEmpty)
    }

    @Test func olderRequestWaitsWithoutASheetAndExpires() {
        let requests = OpenRequests()
        #expect(requests.receive(request(age: 300), hostID: host, agent: nil, now: now)?.route == .hold)
        #expect(requests.pending.count == 1)
        requests.expire(now: now.addingTimeInterval(OpenRequests.lifetime))
        #expect(requests.pending.isEmpty)
        #expect(requests.receive(request(age: OpenRequests.lifetime + 1), hostID: host, agent: nil, now: now) == nil)
    }

    @Test func notAWebPageIsIgnored() {
        let requests = OpenRequests()
        #expect(requests.receive(request("file:///etc/passwd"), hostID: host, agent: nil, now: now) == nil)
        #expect(requests.pending.isEmpty)
    }

    @Test func repeatOfTheSamePageReplacesTheOlder() {
        let requests = OpenRequests()
        _ = requests.receive(request(age: 5), hostID: host, agent: nil, now: now)
        let again = request(age: 1)
        _ = requests.receive(again, hostID: host, agent: nil, now: now)
        #expect(requests.pending.map(\.id) == [again.id])
    }

    @Test func waitingScreenClaimsItsSessionsPagesInsteadOfASheet() {
        let requests = OpenRequests()
        final class Owner {}
        let owner = Owner()
        var delivered: [UUID] = []
        #expect(requests.wait(hostID: host, sessionID: "s_1", owner: owner) { delivered.append($0.id) } == nil)

        let mine = request()
        #expect(requests.receive(mine, hostID: host, agent: nil, now: now)?.route == .claimed)
        #expect(delivered == [mine.id])
        // Claimed requests stay pending, so the chat still offers them.
        #expect(requests.isPending(mine.id))

        // Another session's page still gets its sheet.
        #expect(requests.receive(request(session: "s_2"), hostID: host, agent: nil, now: now)?.route == .present)
        // A page that names no session goes to the screen waiting on that machine.
        #expect(requests.receive(request("https://example.com/a", session: nil), hostID: host, agent: nil, now: now)?.route == .claimed)

        requests.stopWaiting(owner)
        #expect(requests.receive(request("https://example.com/b"), hostID: host, agent: nil, now: now)?.route == .present)
    }

    @Test func screenThatStartsWaitingAfterThePageArrivedGetsIt() {
        let requests = OpenRequests()
        let early = request(age: 0)
        _ = requests.receive(early, hostID: host, agent: nil, now: Date())
        final class Owner {}
        let owner = Owner()
        #expect(requests.wait(hostID: host, sessionID: "s_1", owner: owner) { _ in }?.id == early.id)
    }

    @Test func deadWaiterNoLongerClaims() {
        let requests = OpenRequests()
        final class Owner {}
        var owner: Owner? = Owner()
        requests.wait(hostID: host, sessionID: "s_1", owner: owner!) { _ in }
        owner = nil
        #expect(requests.receive(request(), hostID: host, agent: nil, now: now)?.route == .present)
    }

    // MARK: Presenting over sheets

    @Test func presentsOnTheTopOnceStill() {
        typealias Level = LinkPresenter.Level
        #expect(LinkPresenter.anchor([Level()]) == .on(0))
        #expect(LinkPresenter.anchor([Level(), Level()]) == .on(1))
        // The picker sheet is still coming up, or going away: wait.
        #expect(LinkPresenter.anchor([Level(), Level(moving: true)]) == .wait)
        #expect(LinkPresenter.anchor([Level(), Level(leaving: true)]) == .wait)
        // Out of patience: on the top that stays, never on one leaving.
        #expect(LinkPresenter.anchor([Level(), Level(leaving: true)], patience: false) == .on(0))
        #expect(LinkPresenter.anchor([Level(), Level(), Level(moving: true)], patience: false) == .on(2))
        #expect(LinkPresenter.anchor([]) == .wait)
    }

    // MARK: Progress

    @Test func signInRunsFromCommandToSignedIn() {
        var progress = SignInProgress()
        #expect(progress.phase == .sending)
        progress.sent(HarnessActionResult(result: .typed))
        #expect(progress.phase == .waiting)
        let open = PendingOpen(id: UUID(), hostID: host, sessionID: "s_1", url: URL(string: page)!, agent: "OMP", at: now)
        progress.arrived(open)
        #expect(progress.phase == .link)
        progress.browsing()
        #expect(progress.phase == .browsing)
        progress.browserClosed()
        #expect(progress.phase == .link)
        progress.browsing()
        progress.callbackDelivered()
        #expect(progress.phase == .confirming)
        progress.listed(setUp: false)
        #expect(progress.phase == .confirming)
        progress.listed(setUp: true)
        #expect(progress.phase == .signedIn)
        #expect(!progress.watching)
        // Nothing after the outcome takes it away.
        progress.arrived(open)
        progress.browserClosed()
        #expect(progress.phase == .signedIn)
    }

    @Test func pageBeforeTheCommandsAnswerShowsAtOnce() {
        var progress = SignInProgress()
        let open = PendingOpen(id: UUID(), hostID: host, sessionID: "s_1", url: URL(string: page)!, agent: nil, at: now)
        progress.arrived(open)
        #expect(progress.phase == .link)
        progress.sent(HarnessActionResult(result: .typed))
        #expect(progress.phase == .link)
    }

    @Test func failedCommandSaysSoAndCanRetry() {
        var progress = SignInProgress()
        progress.sent(HarnessActionResult(result: .failed, message: "No answer"))
        #expect(progress.phase == .failed("No answer"))
        #expect(!progress.watching)
        progress.retry()
        #expect(progress.phase == .sending)
        progress.sent(nil)
        #expect(progress.phase == .failed(""))
    }

    @Test func openedOnTheMacWaitsForTheAgent() {
        var progress = SignInProgress()
        progress.sent(HarnessActionResult(result: .typed))
        progress.openedElsewhere()
        #expect(progress.phase == .waiting)
        progress.arrived(PendingOpen(id: UUID(), hostID: host, sessionID: nil, url: URL(string: page)!, agent: nil, at: now))
        progress.openedElsewhere()
        #expect(progress.phase == .confirming)
    }

    @Test func providerListedAsSetUpEndsItFromAnyStep() {
        var progress = SignInProgress()
        progress.sent(HarnessActionResult(result: .typed))
        progress.listed(setUp: true)
        #expect(progress.phase == .signedIn)
    }

    // MARK: Redirect port

    @Test func callbackPortComesFromTheRedirect() {
        #expect(SignInRoute.callbackPort(URL(string: page)!) == 54545)
        // Codex, Gemini: their own ports, IPv4 and IPv6 loopback.
        #expect(SignInRoute.callbackPort(URL(string: "https://auth.openai.com/oauth/authorize?redirect_uri=http://localhost:1455/auth/callback")!) == 1455)
        #expect(SignInRoute.callbackPort(URL(string: "https://accounts.google.com/o/oauth2/v2/auth?redirect_uri=http%3A%2F%2F127.0.0.1%3A41231%2Foauth2callback")!) == 41231)
        #expect(SignInRoute.callbackPort(URL(string: "https://x.example/a?redirect_uri=http%3A%2F%2F%5B%3A%3A1%5D%3A8085%2Fcb")!) == 8085)
        // A loopback page itself, and a redirect with no port.
        #expect(SignInRoute.callbackPort(URL(string: "http://localhost:54545/callback?code=c")!) == 54545)
        #expect(SignInRoute.callbackPort(URL(string: "https://x.example/a?redirect_uri=http%3A%2F%2Flocalhost%2Fcb")!) == 80)
        // Claude Code's sign-in returns to a web page that shows a code to paste: no port.
        #expect(SignInRoute.callbackPort(URL(string: "https://claude.ai/oauth/authorize?redirect_uri=https%3A%2F%2Fconsole.anthropic.com%2Foauth%2Fcode%2Fcallback")!) == nil)
        #expect(SignInRoute.callbackPort(URL(string: "https://example.com/")!) == nil)
    }

    @Test func callbackPortAndRouteAgree() {
        for url in [page, "https://example.com/", "http://127.0.0.1:9000/cb"] {
            let parsed = URL(string: url)!
            #expect((SignInRoute.callbackPort(parsed) != nil) == SignInRoute.returnsToLoopback(parsed))
        }
    }
}
