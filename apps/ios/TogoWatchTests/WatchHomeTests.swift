import Foundation
import Testing
import TogoPush
@testable import TogoWatch

struct WatchHomeTests {
    private func request(_ id: String, subject: PushSubject? = nil) -> PendingRequest {
        PendingRequest(
            requestId: id, hostId: "h", sessionId: "s", bib: "04", agent: "claude", host: "studio",
            title: "Run npm test?", subject: subject, options: [], receivedAt: Date(), answered: nil
        )
    }

    @Test func clearWhenNothingWaits() {
        #expect(WatchHome.page(for: []) == .allClear)
        #expect(WatchHome.clearWords(paired: false, sessions: 0) == WatchCopy.setUpOnPhone)
        #expect(WatchHome.clearWords(paired: true, sessions: 0) == WatchCopy.allClear)
        #expect(WatchHome.clearWords(paired: false, sessions: 2) == WatchCopy.allClear)
    }

    @Test func oldestRequestFillsThePage() {
        let page = WatchHome.page(for: [request("a"), request("b")])
        #expect(page == .request(id: "a", position: 1, count: 2))
        #expect(WatchHome.page(for: [request("b")]) == .request(id: "b", position: 1, count: 1))
    }

    /// An answered request stays on the page for a moment before the next one takes it.
    @Test func anAnsweredRequestIsHeldBeforeTheNext() {
        #expect(WatchHome.page(for: [request("b")], holding: "a") == .request(id: "a", position: 1, count: 1))
        #expect(WatchHome.page(for: [request("b")], holding: nil) == .request(id: "b", position: 1, count: 1))
        #expect(WatchHome.holds(previous: "a", next: "b", answeredHere: true))
        #expect(WatchHome.holds(previous: "a", next: nil, answeredHere: true))
        #expect(!WatchHome.holds(previous: "a", next: "b", answeredHere: false))
        #expect(!WatchHome.holds(previous: nil, next: "b", answeredHere: true))
        #expect(WatchHome.holdAfterAnswer == .milliseconds(1200))
        #expect(WatchHome.armDelay == .milliseconds(500))
    }

    @Test func counterOnlyWhenSeveralWait() {
        #expect(WatchHome.counter(position: 1, count: 1) == nil)
        #expect(WatchHome.counter(position: 1, count: 3) == "1 of 3")
    }

    @Test func commandCarriesPrompt() {
        let command = request("a", subject: PushSubject(kind: "command", text: "npm test"))
        let file = request("b", subject: PushSubject(kind: "file", text: "src/a.ts"))
        #expect(WatchHome.commandText(command) == "$ npm test")
        #expect(WatchHome.commandText(file) == "src/a.ts")
        #expect(WatchHome.commandText(request("c")) == nil)
    }

    @Test func rosterPutsNeedsYouFirst() {
        func session(_ bib: String, _ status: String) -> WatchSession {
            WatchSession(hostName: "Studio", bib: bib, agentName: "Claude Code", status: status, activity: nil)
        }
        let lines = WatchHome.lines([session("01", "working"), session("02", "needs_you"), session("03", "idle")])
        #expect(lines.map(\.bib) == ["02", "01", "03"])
        #expect(lines.map(\.status) == ["Needs you", "Working", "Idle"])
        #expect(lines.map(\.needsYou) == [true, false, false])
        #expect(WatchHome.lines([session("01", "idle"), session("02", "idle")], limit: 1).count == 1)
    }

    @Test func aLineKeepsItsIdentityWhileItsWordsChange() {
        let working = WatchSession(
            hostName: "Studio", bib: "04", agentName: "Claude Code", status: "working", activity: nil,
            machineId: "m", sessionId: "s-04"
        )
        var waiting = working
        waiting.status = "needs_you"
        waiting.activity = "Reading src/a.ts"
        #expect(WatchHome.lines([working])[0].id == WatchHome.lines([waiting])[0].id)
        let other = WatchSession(hostName: "Studio", bib: "05", agentName: "Codex", status: "idle", activity: nil)
        #expect(WatchHome.lines([working])[0].id != WatchHome.lines([other])[0].id)
    }
}
