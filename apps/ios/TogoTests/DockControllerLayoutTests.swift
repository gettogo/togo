import CoreGraphics
import Testing
import TogoTerminal
@testable import Togo

@MainActor struct DockControllerLayoutTests {
    @Test func aDockFaceHasItsFinalBoundsBeforeTheToolbarMeasuresIt() {
        let face = DockControllerView.makeFace()
        let expected = CGSize(width: ControllerFace.side, height: ControllerFace.side)
        #expect(face.bounds.size == expected, "Native glass must receive a sized controller, not a zero-frame placeholder")
        #expect(face.frame.size == expected)
        #expect(face.transform.isIdentity)
    }

    @Test func loadingChatNeverChoosesTerminalInputOrItsControls() {
        let store = HarnessSessionStore(subject: ChatTests.subject,
            services: FakeHarnessServices(descriptor: ChatTests.claude), expectsChat: true)
        #expect(store.shownLayer == .pending)
        #expect(SessionInput(store.shownLayer) == .none)
    }
}
