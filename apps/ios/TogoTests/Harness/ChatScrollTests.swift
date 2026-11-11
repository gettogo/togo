import Foundation
import Observation
import Testing
import TogoKit
@testable import Togo

/// When the chat scrolls on its own: a needs card arriving or its answers landing, the user's
/// own send, a revealed field, never during a drag and never for a size change (the scroll
/// view's anchor keeps the foot); the capsule while a card is off screen; where a rebuilt
/// chat returns to.
@MainActor
struct ChatScrollTests {
    private let a = NeedsMark(id: "approval-a", ready: false)

    /// ⌘↩ and ⌘. answer the topmost open yes-or-no request; answered or text-only ones are skipped.
    @Test func keyAnswersTakeTheTopmostOpenRequest() {
        var answered = Samples.request(id: "r1", bib: "01", title: "Edit?")
        answered.phase = .done(.approved)
        var words = Samples.request(id: "r2", bib: "01", title: "Which file?")
        words.options = []
        words.allowText = true
        let open = Samples.request(id: "r3", bib: "01", title: "Run tests?")
        let cards = [answered, words, open, Samples.request(id: "r4", bib: "01", title: "Push?")]
        let target = ChatKeyAnswers.target(requestIDs: ["r0", "r1", "r2", "r3", "r4"]) { id in cards.first { $0.id == id } }
        #expect(target == ChatKeyAnswers.Target(requestID: "r3", approve: .option(1), deny: .option(2)))
        #expect(ChatKeyAnswers.target(requestIDs: ["r1", "r2"]) { id in cards.first { $0.id == id } } == nil)
    }

    @Test func aNewNeedsCardIsScrolledTo() {
        #expect(ChatScroll.decide(.needsChanged(old: [], new: [a]), dragging: false) == .row("approval-a"))
    }

    @Test func theNewestArrivalWins() {
        let b = NeedsMark(id: "q-b", ready: true)
        #expect(ChatScroll.decide(.needsChanged(old: [a], new: [a, b]), dragging: false) == .row("q-b"))
    }

    @Test func waitingTurningIntoACardScrollsAgain() {
        var ready = a
        ready.ready = true
        #expect(ChatScroll.decide(.needsChanged(old: [a], new: [ready]), dragging: false) == .row("approval-a"))
    }

    @Test func nothingNewMeansNoScroll() {
        #expect(ChatScroll.decide(.needsChanged(old: [a], new: [a]), dragging: false) == nil)
        #expect(ChatScroll.decide(.needsChanged(old: [a], new: []), dragging: false) == nil)
    }

    @Test func aDragSuppressesEveryAutomaticScroll() {
        #expect(ChatScroll.decide(.needsChanged(old: [], new: [a]), dragging: true) == nil)
        #expect(ChatScroll.decide(.sent, dragging: true) == nil)
        #expect(ChatScroll.decide(.liveAppeared(afterSend: true), dragging: true) == nil)
        #expect(ChatScroll.decide(.reveal("approval-a"), dragging: true) == nil)
    }

    @Test func theOwnSendAndItsLiveLineScrollToTheFoot() {
        #expect(ChatScroll.decide(.sent, dragging: false) == .bottom)
        #expect(ChatScroll.decide(.liveAppeared(afterSend: true), dragging: false) == .bottom)
        #expect(ChatScroll.decide(.liveAppeared(afterSend: false), dragging: false) == nil)
    }

    @Test func aRevealedFieldOrGoToRequestScrollsToItsRow() {
        #expect(ChatScroll.decide(.reveal("q-b"), dragging: false) == .row("q-b"))
    }

    /// The scroll bug's rule: the composer growing a line, the keyboard coming or going, a
    /// fold, a reply streaming in, are never a reason to scroll. The scroll view's own anchor
    /// keeps the foot through them, and a reader up in the history is left where they are.
    /// With or without a finger on the chat, at the foot or not.
    @Test(arguments: [false, true])
    func aSizeChangeNeverScrolls(dragging: Bool) {
        #expect(ChatScroll.decide(.resized, dragging: dragging) == nil)
        #expect(ChatScroll.decide(.contentChanged, dragging: dragging) == nil)
    }

    @Test func theFootIsTheLastLineAboveTheComposer() {
        #expect(ChatScroll.isPinned(offset: 300, topInset: 116, container: 580, content: 1_000))
        #expect(!ChatScroll.isPinned(offset: 0, topInset: 116, container: 580, content: 1_000))
    }

    /// Measured on an iPhone 18 Pro with a five-line draft and the keyboard down: 758 pt of
    /// conversation, 116 pt of bar above, 272 pt of composer and home indicator below. Scrolled
    /// to the top, the frame's edge is past the content's end, yet its last 272 pt sit under the
    /// composer: that is not the foot. Only at the furthest offset (156) is the last line above it.
    @Test func theLatestLinesUnderTheComposerAreNotTheFoot() {
        let container: CGFloat = 874 - 116 - 272
        #expect(!ChatScroll.isPinned(offset: -116, topInset: 116, container: container, content: 758))
        #expect(!ChatScroll.isPinned(offset: 100, topInset: 116, container: container, content: 758))
        #expect(ChatScroll.isPinned(offset: 156, topInset: 116, container: container, content: 758))
    }

    /// The chat's top and bottom space is a content margin (`chatScrollMargins()`), not padding
    /// in the content, so a scroll to a needs card leaves the same gap above the composer as the
    /// foot does. The margin counts among the insets, not the content: the same chat as above,
    /// its 758 pt now 726 of rows and 16 each end, is at the foot at its furthest offset, and
    /// a margin and more above it is not.
    @Test func theComposerGapIsAMarginNotContent() {
        #expect(ChatLayout.endMargin == ChatLayout.inset)
        let margin = ChatLayout.endMargin
        let top = 116 + margin
        let container = 874 - 116 - 272 - 2 * margin
        let rows = 758 - 2 * margin
        let foot = rows - container - top
        #expect(ChatScroll.isPinned(offset: foot, topInset: top, container: container, content: rows))
        #expect(!ChatScroll.isPinned(offset: foot - margin - 9, topInset: top, container: container, content: rows))
    }

    /// A rebuilt chat returns to the reader's row while it is there, to the foot once it has
    /// gone, and does nothing at the foot (the default anchor gives it).
    @Test func aRebuiltChatReturnsToTheRowOrFallsBackToTheFoot() {
        let rows: Set<String> = ["p1", "r1"]
        #expect(ChatScroll.restore(row: "r1", pinned: false, has: rows.contains) == .row("r1"))
        #expect(ChatScroll.restore(row: "gone", pinned: false, has: rows.contains) == .bottom)
        #expect(ChatScroll.restore(row: "r1", pinned: true, has: rows.contains) == nil)
        #expect(ChatScroll.restore(row: nil, pinned: false, has: rows.contains) == nil)

        let model = ChatScrollModel()
        model.pinned = false
        model.settle(at: "gone")
        #expect(model.restoreTarget(has: rows.contains) == .bottom)
    }

    /// The settled row is written only when it changes, and never through observation: the
    /// scroll view's own reports must not invalidate the views that drive it (the freeze).
    @Test func settlingWritesOnlyAChange() {
        let model = ChatScrollModel()
        #expect(model.settle(at: "r1"))
        #expect(!model.settle(at: "r1"))
        #expect(model.settledRow == "r1")
        #expect(model.settle(at: nil))
        #expect(!model.settle(at: nil))

        let changed = Flag()
        withObservationTracking { _ = model.request; _ = model.visibleNeeds; _ = model.capsule } onChange: { changed.on = true }
        model.settle(at: "r2")
        model.pinned = false
        model.dragging = true
        #expect(!changed.on)
    }

    /// Each ask is a new request, even to the same place, so a second "go to the foot" moves.
    @Test func everyAskIsANewRequest() {
        let model = ChatScrollModel()
        model.go(.bottom, reduce: true)
        let first = model.request
        model.go(.bottom, reduce: true)
        #expect(model.request?.target == .bottom)
        #expect(model.request != first)
    }

    @Test func theCapsuleOffersTheOldestCardOffScreen() {
        let b = NeedsMark(id: "q-b", ready: true)
        #expect(ChatScroll.capsule(open: [a, b], visible: [], loaded: true) == "approval-a")
        #expect(ChatScroll.capsule(open: [a, b], visible: ["approval-a"], loaded: true) == "q-b")
        #expect(ChatScroll.capsule(open: [a, b], visible: ["approval-a", "q-b"], loaded: true) == nil)
        #expect(ChatScroll.capsule(open: [a], visible: [], loaded: false) == nil)
    }

    @Test func marksListOpenCardsOnly() {
        let at = WireDate(.now)
        let open = HarnessItem(id: "approval-1", seq: 1, turnId: "t", at: at, body: .approval(.init(requestId: "1")))
        let done = HarnessItem(id: "approval-2", seq: 2, turnId: "t", at: at, body: .approval(.init(requestId: "2", resolved: "phone")))
        let question = HarnessItem(id: "q", seq: 3, turnId: "t", at: at, body: .question(.init(questions: [])))
        let marks = ChatScroll.marks([.needs(open), .needs(done), .needs(question)]) { $0.requestId == "1" }
        #expect(marks == [NeedsMark(id: "approval-1", ready: true), NeedsMark(id: "q", ready: true)])
    }

    @Test func revealNeedsSwitchesToTheChatAndAsksForTheCard() async throws {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        let chat = HarnessSessionStore(subject: ChatTests.subject, services: services, cached: ChatTests.claude)
        chat.layer = .terminal
        chat.revealNeeds(id: "approval-9")
        #expect(chat.layer == .chat)
        #expect(chat.scrollRequest?.row == "approval-9")
    }
}
