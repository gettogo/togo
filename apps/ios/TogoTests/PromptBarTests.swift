import Foundation
import Testing
import TogoTerminal
@testable import Togo

/// The prompt bar's model: what it shows, when it moves, and what it hands back.
@MainActor struct PromptBarTests {
    private static let tab = TabID(hostID: UUID(), sessionID: "s-api")

    private static func request(_ id: String = "r-04", bib: String = "04", question: String = "Run npm test?") -> PromptRequest {
        PromptRequest(
            id: id, sessionID: "s-\(bib)", bib: bib, sessionName: "app", agentName: "Claude Code",
            machineName: "Studio", question: question, command: "npm test -- --watch=false"
        )
    }

    private static let suggestions = [
        StartSuggestion(id: "1", agentName: "Claude Code", folder: "~/src/api", machineName: "MacBook Pro"),
        StartSuggestion(id: "2", agentName: "Codex", folder: "~/src/web", machineName: "MacBook Pro"),
        StartSuggestion(id: "3", agentName: "OpenCode", folder: "~/infra", machineName: "Studio"),
        StartSuggestion(id: "4", agentName: "Claude Code", folder: "~/src/app", machineName: "Studio"),
    ]

    // MARK: Modes

    @Test func stageFollowsMode() {
        let model = PromptBarModel()
        #expect(model.stage == .hidden)
        model.mode = .start
        #expect(model.stage == .start(expanded: false))
        model.mode = .input(Self.tab)
        #expect(model.stage == .input(Self.tab))
        let request = Self.request()
        model.mode = .request(request, position: 1, count: 3)
        #expect(model.stage == .request(request, position: 1, count: 3))
    }

    @Test func replyingKeepsTheQueuePlace() {
        let request = Self.request()
        let model = PromptBarModel(mode: .request(request, position: 2, count: 3))
        model.beginReply()
        #expect(model.mode == .replying(request))
        #expect(model.stage == .replying(request, position: 2, count: 3))
        model.cancelReply()
        #expect(model.mode == .request(request, position: 2, count: 3))
    }

    @Test func replyStartsOnlyFromARequest() {
        let model = PromptBarModel(mode: .start)
        model.beginReply()
        #expect(model.mode == .start)
    }

    @Test func leavingStartClosesIt() {
        let model = PromptBarModel(mode: .start)
        model.openStart()
        model.startQuery = "api"
        #expect(model.stage == .start(expanded: true))
        model.mode = .request(Self.request(), position: 1, count: 1)
        model.mode = .start
        #expect(model.stage == .start(expanded: false))
        #expect(model.startQuery.isEmpty)
    }

    @Test func startOpensOnlyInStartMode() {
        let model = PromptBarModel(mode: .input(Self.tab))
        model.openStart()
        model.mode = .start
        #expect(model.stage == .start(expanded: false))
    }

    // MARK: Queue and announcement

    @Test func queueTextOnlyWhenSeveralWait() {
        #expect(PromptBarModel.queueText(position: 1, count: 1) == nil)
        #expect(PromptBarModel.queueText(position: 1, count: 0) == nil)
        #expect(PromptBarModel.queueText(position: 1, count: 3) == "1 of 3")
        #expect(PromptBarModel.queueText(position: 5, count: 3) == "3 of 3")
    }

    @Test func announcementNamesTheSessionAndAsks() {
        #expect(PromptBarModel.announcement(for: Self.request()) == "04 app asks: Run npm test?")
    }

    @Test func announcesEachNewRequestOnce() {
        let model = PromptBarModel(mode: .start)
        var spoken: [String] = []
        model.announce = { spoken.append($0) }
        let first = Self.request("r-04")
        model.mode = .request(first, position: 1, count: 2)
        model.mode = .request(first, position: 1, count: 3)
        model.beginReply()
        model.cancelReply()
        model.mode = .request(Self.request("r-02", bib: "02", question: "Edit 3 files?"), position: 1, count: 2)
        #expect(spoken == ["04 app asks: Run npm test?", "02 app asks: Edit 3 files?"])
    }

    @Test func arrivalsCountOnlyRequestsIntoAQuietBar() {
        let model = PromptBarModel(mode: .start)
        model.announce = { _ in }
        model.mode = .request(Self.request("r-1"), position: 1, count: 2)
        #expect(model.arrivals == 1)
        // The next in the queue rises into a bar that already showed a request.
        model.mode = .request(Self.request("r-2"), position: 1, count: 1)
        #expect(model.arrivals == 1)
        model.beginReply()
        #expect(model.arrivals == 1)
        model.mode = .input(Self.tab)
        model.mode = .request(Self.request("r-3"), position: 1, count: 1)
        #expect(model.arrivals == 2)
    }

    // MARK: Answers

    /// A model with its request already armed, as it is half a second after it shows.
    private static func armed(_ request: PromptRequest = request()) async -> PromptBarModel {
        let model = PromptBarModel(armDelay: .zero)
        model.mode = .request(request, position: 1, count: 1)
        await model.arming?.value
        return model
    }

    @Test func answersGoBackWithTheirRequest() async {
        var request = Self.request()
        request.more = [PromptOption(id: 3, label: "Yes, and don't ask again")]
        let model = await Self.armed(request)
        var answers: [PromptAnswer] = []
        model.onAnswer = { answered, answer in
            #expect(answered.id == request.id)
            answers.append(answer)
        }
        model.answer(.yes, to: request.id)
        model.answer(.no, to: request.id)
        model.answer(.option(3), to: request.id)
        model.beginReply()
        model.replyDraft = "   "
        model.sendReply()
        model.replyDraft = "  Only run the date tests first \n"
        model.sendReply()
        #expect(answers == [.yes, .no, .option(3), .reply("Only run the date tests first")])
        #expect(model.replyDraft.isEmpty)
    }

    @Test func noAnswerWithoutARequest() {
        let model = PromptBarModel(mode: .start)
        var called = false
        model.onAnswer = { _, _ in called = true }
        model.answer(.yes, to: "r-04")
        model.sendReply()
        #expect(!called)
    }

    /// A new request's answers wait about half a second, so a tap meant for the one before
    /// cannot land on it.
    @Test func answersWaitUntilTheRequestIsArmed() async {
        let request = Self.request()
        let model = PromptBarModel(armDelay: .zero)
        var answers: [PromptAnswer] = []
        model.onAnswer = { _, answer in answers.append(answer) }
        model.mode = .request(request, position: 1, count: 1)
        model.answer(.yes, to: request.id)
        #expect(answers.isEmpty)
        #expect(!model.acceptsKeyAnswers)
        await model.arming?.value
        #expect(model.acceptsKeyAnswers)
        model.answer(.yes, to: request.id)
        #expect(answers == [.yes])
        #expect(PromptBarModel.defaultArmDelay == .milliseconds(500))
    }

    /// An answer names the request that was on screen; one meant for another request is dropped.
    @Test func anAnswerForAnotherRequestIsDropped() async {
        let first = Self.request("r-a")
        let second = Self.request("r-b", bib: "05")
        let model = await Self.armed(first)
        var answered: [String] = []
        model.onAnswer = { request, _ in answered.append(request.id) }
        model.mode = .request(second, position: 1, count: 1)
        model.answer(.yes, to: first.id)
        model.answer(.yes, to: second.id)
        #expect(answered.isEmpty)
        await model.arming?.value
        model.answer(.yes, to: first.id)
        model.answer(.yes, to: second.id)
        #expect(answered == ["r-b"])
    }

    @Test func noAnswerWhileOneIsOnItsWay() async {
        var request = Self.request()
        let model = await Self.armed(request)
        var called = 0
        model.onAnswer = { _, _ in called += 1 }
        request.state = .sending
        model.mode = .request(request, position: 1, count: 1)
        model.answer(.yes, to: request.id)
        #expect(!model.acceptsKeyAnswers)
        request.state = .failed("Studio did not get the answer.")
        model.mode = .request(request, position: 1, count: 1)
        model.answer(.yes, to: request.id)
        #expect(called == 1)
    }

    /// Words typed into a prompt that takes none would pick one of its options, so there is
    /// no Reply.
    @Test func noReplyWhenTheRequestTakesNoWords() async {
        var request = Self.request()
        request.allowsReply = false
        let model = await Self.armed(request)
        model.beginReply()
        #expect(model.mode == .request(request, position: 1, count: 1))
    }

    /// A question that only takes words has no Yes or No, on screen or on the keys.
    @Test func aWordsOnlyQuestionTakesNoYesOrNo() async {
        var request = Self.request()
        request.asksYesOrNo = false
        let model = await Self.armed(request)
        var called = false
        model.onAnswer = { _, _ in called = true }
        model.answer(.yes, to: request.id)
        #expect(!called)
        #expect(!model.acceptsKeyAnswers)
    }

    /// The agent stops asking mid-reply: the words stay, nothing typed turns into an answer, and
    /// the next request arms afresh once the reply is put away.
    @Test func aWithdrawnReplyKeepsItsWords() async {
        let request = Self.request()
        let model = await Self.armed(request)
        model.beginReply()
        model.replyDraft = "yes, but use pnpm"
        var gone = request
        gone.state = .withdrawn
        model.mode = .replying(gone)
        #expect(model.replyDraft == "yes, but use pnpm")
        var called = false
        model.onAnswer = { _, _ in called = true }
        model.sendReply()
        #expect(!called)
        let dismissals = model.dismissals
        model.cancelReply()
        #expect(model.dismissals == dismissals + 1)
        #expect(model.replyDraft.isEmpty)
        let next = Self.request("r-next", bib: "06")
        model.mode = .request(next, position: 1, count: 1)
        #expect(!model.acceptsKeyAnswers)
    }

    /// Back from a reply, the keys wait out the arming delay again.
    @Test func cancellingAReplyRearms() async {
        let model = await Self.armed()
        model.beginReply()
        model.cancelReply()
        #expect(!model.acceptsKeyAnswers)
        await model.arming?.value
        #expect(model.acceptsKeyAnswers)
    }

    @Test func inputSendsItsTextAndClears() {
        let model = PromptBarModel(mode: .input(Self.tab))
        var sent: [String] = []
        model.onSend = { tab, text in
            #expect(tab == Self.tab)
            sent.append(text)
        }
        model.inputDraft = "  "
        model.sendInput()
        model.inputDraft = "  ls -la\n"
        model.sendInput()
        #expect(sent == ["  ls -la"])
        #expect(model.inputDraft.isEmpty)
    }

    /// Each terminal keeps what was typed for it while a request takes the bar.
    @Test func inputDraftsStayWithTheirTerminal() {
        let other = TabID(hostID: UUID(), sessionID: "s-web")
        let model = PromptBarModel(mode: .input(Self.tab))
        model.inputDraft = "git status"
        model.mode = .request(Self.request(), position: 1, count: 1)
        model.mode = .input(other)
        #expect(model.inputDraft.isEmpty)
        model.mode = .input(Self.tab)
        #expect(model.inputDraft == "git status")
    }

    /// Only keys made of text can be sent from the bar; the rest stay on the terminal's own bar.
    @Test func inputKeysAreOnlyKeysThatSend() {
        let model = PromptBarModel(mode: .input(Self.tab))
        let esc = KeyBarItem(title: "Esc", action: .steps([.text("\u{1B}")]))
        let up = KeyBarItem(title: "Up", action: .steps([.stroke(KeyStroke(.up))]))
        model.keyBar = KeyBarConfig(items: [
            esc, up, KeyBarItem(title: "Ctrl", action: .ctrlLatch), KeyBarItem(title: "Paste", action: .paste),
        ])
        #expect(model.inputKeys == [esc])
        var tapped: [String] = []
        model.onKey = { _, item in tapped.append(item.title) }
        model.tapKey(esc)
        #expect(tapped == ["Esc"])
    }

    /// Single letters can be turned off; ⌘↩ and ⌘. keep working, even while a field types.
    @Test func singleKeyAnswersCanBeTurnedOff() async {
        let model = await Self.armed()
        model.singleKeyAnswers = false
        #expect(!model.acceptsKeyAnswers)
        #expect(model.acceptsCommandAnswers)
        model.otherTextFocused = true
        #expect(model.acceptsCommandAnswers)
        model.mode = .start
        #expect(!model.acceptsCommandAnswers)
    }

    @Test func keyAnswersOnlyWhileNoFieldHoldsTheKeyboard() async {
        let model = await Self.armed()
        #expect(model.acceptsKeyAnswers)
        model.otherTextFocused = true
        #expect(!model.acceptsKeyAnswers)
        model.otherTextFocused = false
        model.barTextFocused = true
        #expect(!model.acceptsKeyAnswers)
        model.barTextFocused = false
        model.beginReply()
        #expect(!model.acceptsKeyAnswers)
        model.mode = .start
        #expect(!model.acceptsKeyAnswers)
    }

    // MARK: Start

    @Test func suggestionsFilterByAgentFolderOrMachine() {
        let all = Self.suggestions
        #expect(PromptBarModel.filter(all, query: "").map(\.id) == ["1", "2", "3"])
        #expect(PromptBarModel.filter(all, query: "codex").map(\.id) == ["2"])
        #expect(PromptBarModel.filter(all, query: "SRC").map(\.id) == ["1", "2", "4"])
        #expect(PromptBarModel.filter(all, query: "studio").map(\.id) == ["3", "4"])
        #expect(PromptBarModel.filter(all, query: "claude studio").map(\.id) == ["4"])
        #expect(PromptBarModel.filter(all, query: "nothing").isEmpty)
    }

    @Test func pickingStartsAndCloses() {
        let model = PromptBarModel(mode: .start)
        model.startSuggestions = Self.suggestions
        var started: [String] = []
        model.onStart = { started.append($0.id) }
        model.openStart()
        model.startQuery = "infra"
        model.pickFirst()
        #expect(started == ["3"])
        #expect(model.stage == .start(expanded: false))
        #expect(model.startQuery.isEmpty)
        model.openStart()
        model.startQuery = "nothing"
        model.pickFirst()
        #expect(started == ["3"])
    }

    @Test func cancelAndMoreOptionsClose() {
        let model = PromptBarModel(mode: .start)
        var sheets = 0
        model.onOpenStartSheet = { sheets += 1 }
        model.openStart()
        model.startQuery = "web"
        model.cancelStart()
        #expect(model.stage == .start(expanded: false))
        #expect(model.startQuery.isEmpty)
        model.openStart()
        model.openStartSheet()
        #expect(sheets == 1)
        #expect(model.stage == .start(expanded: false))
    }

    // MARK: Motion

    @Test func changesPickTheirMotion() {
        let a = Self.request("a"), b = Self.request("b")
        typealias Stage = PromptBarModel.Stage
        func change(_ old: Stage, _ new: Stage) -> PromptBarModel.Change { PromptBarModel.change(from: old, to: new) }
        #expect(change(.start(expanded: false), .request(a, position: 1, count: 1)) == .rise(.dropIn))
        #expect(change(.input(Self.tab), .request(a, position: 1, count: 1)) == .rise(.dropIn))
        #expect(change(.request(a, position: 1, count: 2), .request(b, position: 1, count: 1)) == .rise(.dropIn))
        #expect(change(.replying(a, position: 1, count: 1), .request(b, position: 1, count: 1)) == .rise(.dropIn))
        #expect(change(.request(a, position: 1, count: 1), .start(expanded: false)) == .rise(.collapse))
        #expect(change(.request(a, position: 1, count: 1), .input(Self.tab)) == .rise(.collapse))
        #expect(change(.request(a, position: 1, count: 2), .request(a, position: 1, count: 3)) == .none)
        #expect(change(.request(a, position: 1, count: 1), .replying(a, position: 1, count: 1)) == .resize)
        #expect(change(.start(expanded: false), .start(expanded: true)) == .resize)
        #expect(change(.hidden, .start(expanded: false)) == .rise(.dropIn))
        #expect(change(.start(expanded: false), .hidden) == .resize)
    }

    @Test func eachRequestIntroducesOnce() {
        let model = PromptBarModel()
        #expect(model.introPending(.command, for: "a"))
        #expect(model.takeIntro(.command, for: "a"))
        #expect(!model.takeIntro(.command, for: "a"))
        #expect(!model.introPending(.command, for: "a"))
        #expect(model.introPending(.beacon, for: "a"))
        #expect(model.introPending(.command, for: "b"))
    }

    /// With a hardware keyboard the key row is gone: Esc, ⌃C, Tab, ⇧Tab and the arrows still
    /// reach the terminal from the input line, as its bytes.
    @Test func hardwareKeysReachTheTerminal() {
        #expect(PromptHardwareKey(name: .escape).bytes(draftEmpty: false) == "\u{1b}")
        #expect(PromptHardwareKey(name: .control("c")).bytes(draftEmpty: false) == "\u{03}")
        #expect(PromptHardwareKey(name: .tab).bytes(draftEmpty: true) == "\t")
        #expect(PromptHardwareKey(name: .tab, shift: true).bytes(draftEmpty: true) == "\u{1b}[Z")
        #expect(PromptHardwareKey(name: .up).bytes(draftEmpty: false) == "\u{1b}[A")
        // Left and right move the caret while there is text.
        #expect(PromptHardwareKey(name: .left).bytes(draftEmpty: false) == nil)
        #expect(PromptHardwareKey(name: .left).bytes(draftEmpty: true) == "\u{1b}[D")
    }
}
