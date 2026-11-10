import CoreGraphics
import Testing
@testable import Togo

/// The tab strip fades an end only when chips lie past it, so a short strip never fades a chip.
struct TabStripOverflowTests {
    @Test func aStripThatFitsFadesNeitherEnd() {
        #expect(TabStripOverflow(visible: CGRect(x: 0, y: 0, width: 400, height: 44), contentWidth: 320) == .init())
    }

    @Test func chipsPastTheTrailingEdgeFadeIt() {
        #expect(TabStripOverflow(visible: CGRect(x: 0, y: 0, width: 380, height: 44), contentWidth: 520)
            == .init(leading: false, trailing: true))
    }

    @Test func scrolledToTheEndFadesOnlyTheLeadingEdge() {
        #expect(TabStripOverflow(visible: CGRect(x: 140, y: 0, width: 380, height: 44), contentWidth: 520)
            == .init(leading: true, trailing: false))
    }

    @Test func inTheMiddleBothEndsFade() {
        #expect(TabStripOverflow(visible: CGRect(x: 60, y: 0, width: 380, height: 44), contentWidth: 520)
            == .init(leading: true, trailing: true))
    }
}
