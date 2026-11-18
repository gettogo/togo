import Testing
@testable import Togo

/// Owned controls use Title Case; conversational messages keep sentence case. User and
/// harness text is not transformed (also covered by the session-name regression).
@MainActor struct CopyCapitalizationTests {
    @Test func ownedControlsUseTitleCase() {
        #expect(ControlCenterCopy.addMachine == "Add Machine")
        #expect(ControlCenterCopy.newSession == "New Session")
        #expect(ControlCenterCopy.terminalOnly == "Terminal Only")
        #expect(ControlCenterCopy.signInAgain == "Sign In Again")
        #expect(ControlCenterCopy.kill == "End Session")
        #expect(WorkspaceCopy.copyLines == "Copy Last 500 Lines")
        #expect(PrimitiveCopy.showMore == "Show More")
        #expect(ChatCopy.tryAgain == "Try Again")
        #expect(RestoreActions.title(.restoreAndContinue) == "Restore and Continue")
    }

    @Test func chatMessagesKeepSentenceCase() {
        #expect(ChatCopy.emptyTitle == "Nothing said yet")
        #expect(ChatCopy.emptyDetail == "Write below to start.")
        #expect(ControlCenterCopy.killMessage == "The agent stops and its screen is gone.")
    }
}
