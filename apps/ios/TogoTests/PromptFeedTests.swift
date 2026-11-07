import Foundation
import Testing
import TogoKit
@testable import Togo

/// What the prompt bar holds: the oldest request first with its place in the queue, a reply
/// kept while its request waits, then the focused terminal's input, then the start line.
@MainActor struct PromptFeedTests {
    private let services = FakeAppServices(hosts: Samples.hosts)
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    private func request(_ id: String, bib: String = "01") -> PromptRequest {
        PromptRequest(
            id: id, sessionID: "s-\(bib)", bib: bib, sessionName: "api", agentName: "Claude Code",
            machineName: "Studio", question: "Run npm test?", command: nil
        )
    }

    private func mode(
        _ requests: [PromptRequest], current: PromptBarModel.Mode = .start, focused: TabID? = nil,
        composing: Bool = false, hasMachines: Bool = true
    ) -> PromptBarModel.Mode {
        PromptFeed.mode(requests: requests, current: current, focused: focused, composing: composing, hasMachines: hasMachines)
    }

    // MARK: Queue

    @Test func requestsComeOldestFirstAcrossMachines() {
        var state = Samples.controlCenter
        state.requests.append(Samples.request(id: "r2", hostID: Samples.build, bib: "02", title: "Ship it?"))
        state.requests.append(Samples.request(id: "r3", bib: "01", title: "Edit routes.ts?"))
        let queue = PromptFeed.requests(in: AppStore(services: services, controlCenter: state))
        #expect(queue.map(\.id) == ["r1", "r2", "r3"])
        #expect(queue[0].bib == "04")
        #expect(queue[0].sessionName == "app")
        #expect(queue[0].machineName == "Studio")
        #expect(queue[1].machineName == "Build box")
        #expect(queue[0].question == "Run npm test?")
    }

    /// An answered request shows its outcome until the store lets it go; one being sent shows
    /// that, and a failed send says so with the request open again.
    @Test func requestsCarryWhereTheirAnswerStands() {
        var state = Samples.controlCenter
        state.requests[0].phase = .done(.approved)
        state.requests.append(Samples.request(id: "r2", bib: "01", title: "Edit routes.ts?"))
        state.requests.append(Samples.request(id: "r3", bib: "02", title: "Ship it?"))
        state.requests[2].phase = .sending(.approve)
        let queue = PromptFeed.requests(in: AppStore(services: services, controlCenter: state))
        #expect(queue.map(\.id) == ["r1", "r2", "r3"])
        #expect(queue.map(\.state) == [.done(.approved), .open, .sending])
        #expect(PromptFeed.state(of: state.requests[1], failure: "Studio did not get it.") == .failed("Studio did not get it."))
    }

    /// Reply only where the agent takes words; Yes and No only where they mean something; the
    /// agent's other options kept.
    @Test func requestsCarryWhatTheyCanBeAnswered() {
        var card = Samples.request(id: "r", bib: "04", title: "Run npm test?")
        card.options = [
            ApprovalOption(n: 1, label: "Yes", role: .approve),
            ApprovalOption(n: 2, label: "Yes, and don't ask again", role: .approveAlways),
            ApprovalOption(n: 3, label: "No", role: .deny),
        ]
        card.allowText = false
        let approval = PromptFeed.request(card, row: nil, machineName: "Studio")
        #expect(!approval.allowsReply)
        #expect(approval.asksYesOrNo)
        #expect(approval.more == [PromptOption(id: 2, label: "Yes, and don't ask again")])
        card.options = []
        card.allowText = true
        let question = PromptFeed.request(card, row: nil, machineName: "Studio")
        #expect(question.allowsReply)
        #expect(!question.asksYesOrNo)
    }

    /// Words for a prompt that takes none are never sent, by the helper or into the terminal.
    @Test func wordsForAPromptThatTakesNoneGoNowhere() {
        var card = Samples.request(id: "r", bib: "04", title: "Run npm test?")
        card.allowText = false
        #expect(PromptFeed.decision(.reply("use pnpm"), for: card) == nil)
        #expect(PromptFeed.decision(.option(9), for: card) == nil)
    }

    @Test func theOldestRequestShowsWithItsPlaceAndCount() {
        #expect(mode([request("a"), request("b"), request("c")]) == .request(request("a"), position: 1, count: 3))
        #expect(mode([request("a")]) == .request(request("a"), position: 1, count: 1))
    }

    // MARK: Mode switching

    @Test func aReplyStaysWhileItsRequestWaits() {
        let writing = PromptBarModel.Mode.replying(request("b"))
        #expect(mode([request("a"), request("b")], current: writing) == .replying(request("b")))
    }

    /// Gone or answered elsewhere mid-reply: the reply stays, marked, and nothing new takes the
    /// bar until the user puts it away.
    @Test func aReplyOutlivesItsRequest() {
        let writing = PromptBarModel.Mode.replying(request("b"))
        var gone = request("b")
        gone.state = .withdrawn
        #expect(mode([request("a")], current: writing) == .replying(gone))
        #expect(mode([], current: writing) == .replying(gone))
        var elsewhere = request("b")
        elsewhere.state = .done(.answeredElsewhere)
        #expect(mode([elsewhere, request("a")], current: writing) == .replying(gone))
        // Put away, the next request rises.
        #expect(mode([request("a")], current: .hidden) == .request(request("a"), position: 1, count: 1))
    }

    @Test func requestsWinOverTheTerminalAndTheComposeBar() {
        #expect(mode([request("a")], focused: four) == .request(request("a"), position: 1, count: 1))
        #expect(mode([request("a")], composing: true) == .request(request("a"), position: 1, count: 1))
    }

    @Test func nothingWaitingIsTheTerminalsInputOrTheStartLine() {
        #expect(mode([], focused: four) == .input(four))
        #expect(mode([], focused: nil) == .start)
    }

    @Test func theComposeBarTakesOverTheInput() {
        #expect(mode([], focused: four, composing: true) == .hidden)
    }

    @Test func noMachineHidesTheBar() {
        #expect(mode([request("a")], hasMachines: false) == .hidden)
        #expect(mode([], hasMachines: false) == .hidden)
    }

    // MARK: Answers and starts

    @Test func yesAndNoTakeTheAgentsOwnOptions() {
        let card = Samples.request(id: "r", bib: "04", title: "Run npm test?")
        #expect(PromptFeed.decision(.yes, for: card) == .option(1))
        #expect(PromptFeed.decision(.no, for: card) == .option(2))
        var words = card
        words.allowText = true
        #expect(PromptFeed.decision(.reply("only the date tests"), for: words) == .text("only the date tests"))

        var plain = card
        plain.options = []
        #expect(PromptFeed.decision(.yes, for: plain) == .approve)
        #expect(PromptFeed.decision(.no, for: plain) == .deny)
    }

    @Test func suggestionsComeFromKnownFoldersOnceEach() {
        var state = Samples.controlCenter
        state.machines[0].sessions.append(
            Samples.row(bib: "09", name: "api2", status: .idle, cwd: "/Users/sam/src/api", since: Samples.now.addingTimeInterval(60))
        )
        let found = PromptFeed.suggestions(machines: state.machines, archived: state.archived)
        #expect(found.filter { $0.folder == "/Users/sam/src/api" }.count == 1)
        #expect(found.first?.folder == "/Users/sam/src/api")
        #expect(found.first?.machineName == "Studio")

        let target = PromptFeed.target(of: found[0])
        #expect(target?.hostID == Samples.studio)
        #expect(target?.folder == "/Users/sam/src/api")
        #expect(target?.agent == "claude code")
        #expect(PromptFeed.suggestions(machines: state.machines, archived: [], limit: 2).count == 2)
    }

    /// Suggestions follow the agent picker: a favorite agent's folders come first.
    @Test func suggestionsPutFavoriteAgentsFirst() {
        var state = Samples.controlCenter
        state.machines[0].sessions.append(
            Samples.row(bib: "09", name: "api2", status: .idle, cwd: "/Users/sam/src/api", since: Samples.now.addingTimeInterval(60))
        )
        var older = Samples.row(bib: "10", name: "old", status: .idle, cwd: "/Users/sam/src/old", since: Samples.now.addingTimeInterval(-6000))
        older.agent = "qwen"
        state.machines[0].sessions.append(older)
        let found = PromptFeed.suggestions(machines: state.machines, archived: state.archived, favorites: ["qwen"])
        #expect(found.first?.folder == "/Users/sam/src/old")
        #expect(found.dropFirst().first?.folder == "/Users/sam/src/api")
    }

    /// ⌘T asks the bar, which opens its start line or, when it is busy, the sheet.
    @Test func commandTAsksTheBar() {
        let store = AppStore(services: services, controlCenter: Samples.controlCenter)
        store.requestStart()
        #expect(store.startRequests == 1)

        let none = AppStore(services: FakeAppServices(hosts: []), controlCenter: Samples.controlCenter)
        none.requestStart()
        #expect(none.startRequests == 0)
    }
}
