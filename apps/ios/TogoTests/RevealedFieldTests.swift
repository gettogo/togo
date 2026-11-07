import Testing
@testable import Togo

/// A field an action reveals takes the keyboard at once, so one tap is enough to type.
struct RevealedFieldTests {
    @Test func revealingFocusesTheField() {
        var fields = RevealedFields<String>()
        #expect(fields.isEmpty && fields.focus == nil)
        fields.reveal("q1")
        #expect(fields.isOpen("q1"))
        #expect(fields.focus == "q1")
    }

    @Test func theLatestRevealHoldsTheKeyboard() {
        var fields = RevealedFields<String>()
        fields.reveal("q1")
        fields.reveal("q2")
        #expect(fields.focus == "q2")
        #expect(fields.isOpen("q1") && fields.isOpen("q2"))
    }

    @Test func hidingTheFocusedFieldLetsTheKeyboardGo() {
        var fields = RevealedFields<String>()
        fields.reveal("q1")
        fields.reveal("q2")
        fields.hide("q1")
        #expect(fields.focus == "q2")
        fields.hide("q2")
        #expect(fields.focus == nil)
        #expect(fields.isEmpty)
    }

    @Test func focusFollowsTheUserOnlyToOpenFields() {
        var fields = RevealedFields<String>()
        fields.reveal("q1")
        fields.reveal("q2")
        fields.focusMoved(to: "q1")
        #expect(fields.focus == "q1")
        fields.focusMoved(to: "closed")
        #expect(fields.focus == nil)
        fields.focusMoved(to: nil)
        #expect(fields.focus == nil)
    }

    @Test func revealingAgainRefocuses() {
        var fields = RevealedFields<Int>()
        fields.reveal(1)
        fields.focusMoved(to: nil)
        fields.reveal(1)
        #expect(fields.focus == 1)
    }

    @Test func focusIsAskedForUntilItLands() {
        #expect(FocusRetry.shouldRetry(attempt: 0, focused: false))
        #expect(!FocusRetry.shouldRetry(attempt: 0, focused: true))
        #expect(!FocusRetry.shouldRetry(attempt: FocusRetry.delays.count, focused: false))
        #expect(FocusRetry.delays.first == .zero)
    }

    @MainActor @Test func landingStopsOnceFocused() async {
        var asks = 0
        var focused = false
        await FocusRetry.land(isFocused: { focused }, focus: {
            asks += 1
            if asks == 2 { focused = true }
        })
        #expect(asks == 2)
    }
}
