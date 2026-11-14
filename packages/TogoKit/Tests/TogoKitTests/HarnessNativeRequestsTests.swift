import Foundation
import Testing
@testable import TogoKit

/// Which waiting question the chat attaches the agent's own screen to, and
/// that it does so once. Client state only: no harness is run here.
struct HarnessNativeRequestsTests {
    static let t0 = Date(timeIntervalSince1970: 1_800_000_000)

    static func question(_ id: String, seq: Int, answerable: Bool? = false,
                         answered: [String: [String]]? = nil) -> HarnessItem {
        let asked = HarnessQuestion(id: "q", prompt: "Pick one", options: [], multi: false, allowText: true)
        return HarnessItem(id: id, seq: seq, at: WireDate(t0.addingTimeInterval(Double(seq))),
                           body: .question(.init(questions: [asked], answered: answered, answerable: answerable)))
    }

    static func text(_ id: String, seq: Int) -> HarnessItem {
        HarnessItem(id: id, seq: seq, at: WireDate(t0), body: .assistant(.init(text: "hello", partial: false)))
    }

    @Test func attachesAnOpenUnanswerableQuestionOnce() {
        var requests = HarnessNativeRequests()
        let items = [Self.text("a", seq: 1), Self.question("form", seq: 2)]
        #expect(requests.next(in: items, available: true) == "form")
        // Still waiting: closing the screen or reconnecting does not reopen it.
        #expect(requests.next(in: items, available: true) == nil)
        #expect(requests.next(in: items + [Self.text("b", seq: 3)], available: true) == nil)
    }

    @Test func leavesQuestionsThePhoneCanAnswerToTheChat() {
        var requests = HarnessNativeRequests()
        let items = [Self.question("old", seq: 1, answerable: nil), Self.question("card", seq: 2, answerable: true)]
        #expect(requests.next(in: items, available: true) == nil)
    }

    @Test func skipsAnsweredQuestions() {
        var requests = HarnessNativeRequests()
        let items = [Self.question("done", seq: 1, answered: ["q": ["yes"]]), Self.question("open", seq: 2, answered: [:])]
        #expect(requests.next(in: items, available: true) == "open")
    }

    @Test func picksTheOldestWhateverTheOrderAndThenTheNext() {
        var requests = HarnessNativeRequests()
        let items = [Self.question("second", seq: 9), Self.question("first", seq: 4)]
        #expect(requests.next(in: items, available: true) == "first")
        #expect(requests.next(in: items, available: true) == "second")
        #expect(requests.next(in: items, available: true) == nil)
    }

    @Test func aSessionThatCannotShowTheScreenUsesNothingUp() {
        var requests = HarnessNativeRequests()
        let items = [Self.question("form", seq: 2)]
        #expect(requests.next(in: items, available: false) == nil)
        #expect(requests.next(in: items, available: false) == nil)
        #expect(requests.next(in: items, available: true) == "form")
        // Not offered again while unavailable either.
        #expect(requests.next(in: items, available: false) == nil)
        #expect(requests.next(in: items, available: true) == nil)
    }

    @Test func forgetsAQuestionOnceItIsAnsweredOrGone() {
        var requests = HarnessNativeRequests()
        #expect(requests.next(in: [Self.question("form", seq: 2)], available: true) == "form")
        // Answered: forgotten, even while the session cannot show screens.
        #expect(requests.next(in: [Self.question("form", seq: 3, answered: ["q": ["x"]])], available: false) == nil)
        // The same id waiting again is a new request.
        #expect(requests.next(in: [Self.question("form", seq: 4)], available: true) == "form")
        // Gone from the items: forgotten too.
        #expect(requests.next(in: [], available: true) == nil)
        #expect(requests.next(in: [Self.question("form", seq: 5)], available: true) == "form")
    }

    @Test func resetAllowsAFreshRequestInTheNewConversation() {
        var requests = HarnessNativeRequests()
        let items = [Self.question("form", seq: 2)]
        #expect(requests.next(in: items, available: true) == "form")
        requests.reset()
        #expect(requests.next(in: items, available: true) == "form")
    }

    /// A command's own form is matched to the question it adds, without
    /// disturbing what is known about questions that were waiting before it.
    @Test func skippedQuestionsAreNeitherOfferedNorForgotten() {
        var requests = HarnessNativeRequests()
        let earlier = Self.question("earlier", seq: 1)
        #expect(requests.next(in: [earlier], available: true) == "earlier")
        let items = [earlier, Self.question("fromCommand", seq: 5)]
        #expect(requests.next(in: items, available: true, skipping: ["earlier"]) == "fromCommand")
        // The earlier one is still attached: it is not offered again afterwards.
        #expect(requests.next(in: items, available: true) == nil)
        // A skipped one that was never attached stays available for later.
        var fresh = HarnessNativeRequests()
        #expect(fresh.next(in: items, available: true, skipping: ["earlier"]) == "fromCommand")
        #expect(fresh.next(in: items, available: true) == "earlier")
    }

    /// The helper's wire form of a question shown only in the terminal.
    @Test func readsTheWireFormOfATerminalOnlyQuestion() throws {
        let wire = #"{"id":"ask-1","seq":7,"at":"2026-11-14T10:00:00Z","kind":"question","question":{"questions":[{"id":"memo","prompt":"Memo","options":[],"multi":false,"allowText":true}],"answerable":false}}"#
        let item = try JSONDecoder().decode(HarnessItem.self, from: Data(wire.utf8))
        var requests = HarnessNativeRequests()
        #expect(requests.next(in: [item], available: true) == "ask-1")
    }
}
