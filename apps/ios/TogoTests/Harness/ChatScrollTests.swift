import Foundation
import Testing
import TogoKit
@testable import Togo

/// When the chat scrolls on its own: a needs card arriving or its answers landing, the user's
/// own send, never during a drag; the capsule while a card is off screen; staying at the foot
/// when the composer changes height.
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
        #expect(ChatScroll.target(old: [], new: [a], dragging: false) == "approval-a")
    }

    @Test func theNewestArrivalWins() {
        let b = NeedsMark(id: "q-b", ready: true)
        #expect(ChatScroll.target(old: [a], new: [a, b], dragging: false) == "q-b")
    }

    @Test func waitingTurningIntoACardScrollsAgain() {
        var ready = a
        ready.ready = true
        #expect(ChatScroll.target(old: [a], new: [ready], dragging: false) == "approval-a")
    }

    @Test func nothingNewMeansNoScroll() {
        #expect(ChatScroll.target(old: [a], new: [a], dragging: false) == nil)
        #expect(ChatScroll.target(old: [a], new: [], dragging: false) == nil)
    }

    @Test func aDragSuppressesEveryAutomaticScroll() {
        #expect(ChatScroll.target(old: [], new: [a], dragging: true) == nil)
        #expect(ChatScroll.followsSend(dragging: true) == nil)
        #expect(ChatScroll.liveAppeared(was: false, now: true, afterSend: true, dragging: true) == nil)
        #expect(!ChatScroll.repins(pinned: true, old: 60, new: 90, dragging: true))
    }

    @Test func theOwnSendAndItsLiveLineScrollToTheFoot() {
        #expect(ChatScroll.followsSend(dragging: false) == .bottom)
        #expect(ChatScroll.liveAppeared(was: false, now: true, afterSend: true, dragging: false) == .bottom)
        #expect(ChatScroll.liveAppeared(was: false, now: true, afterSend: false, dragging: false) == nil)
        #expect(ChatScroll.liveAppeared(was: true, now: true, afterSend: true, dragging: false) == nil)
    }

    @Test func aComposerHeightChangeRepinsOnlyAtTheFoot() {
        #expect(ChatScroll.repins(pinned: true, old: 60, new: 90, dragging: false))
        #expect(!ChatScroll.repins(pinned: false, old: 60, new: 90, dragging: false))
        #expect(!ChatScroll.repins(pinned: true, old: 60, new: 60, dragging: false))
        #expect(ChatScroll.isPinned(offset: 300, topInset: 116, container: 580, content: 1_000))
        #expect(!ChatScroll.isPinned(offset: 0, topInset: 116, container: 580, content: 1_000))
    }

    @Test func theKeyboardKeepsAReaderAtTheFootAtTheFoot() {
        // The keyboard shrinks the chat by its height: a reader at the foot stays there, the
        // latest line right above the composer.
        let foot = ChatScroll.Frame(pinned: true, height: 640)
        #expect(ChatScroll.repinsOnResize(old: foot, new: .init(pinned: false, height: 330), dragging: false))
        // And when it goes, the same.
        #expect(ChatScroll.repinsOnResize(old: .init(pinned: true, height: 330), new: .init(pinned: true, height: 640), dragging: false))
        // A reader up in the history keeps their place; so does a drag, and a scroll at the same height.
        #expect(!ChatScroll.repinsOnResize(old: .init(pinned: false, height: 640), new: .init(pinned: false, height: 330), dragging: false))
        #expect(!ChatScroll.repinsOnResize(old: foot, new: .init(pinned: false, height: 330), dragging: true))
        #expect(!ChatScroll.repinsOnResize(old: foot, new: .init(pinned: false, height: 640), dragging: false))
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
