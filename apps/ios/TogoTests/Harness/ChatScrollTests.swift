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
        #expect(ChatScroll.isPinned(visibleMaxY: 995, content: 1_000))
        #expect(!ChatScroll.isPinned(visibleMaxY: 700, content: 1_000))
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
