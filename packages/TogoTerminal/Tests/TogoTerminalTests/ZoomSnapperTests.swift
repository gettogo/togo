import CoreGraphics
import Testing
@testable import TogoTerminal

@Suite struct ZoomSnapperTests {
    /// A monospaced face whose advance is 0.6 em, close to SF Mono.
    let mono: (CGFloat) -> CGFloat = { $0 * 0.6 }

    func columns(_ width: CGFloat, _ size: CGFloat) -> Int {
        Int(width / mono(size))
    }

    @Test func snapsToEightyColumns() {
        let size = ZoomSnapper.snap(width: 486, fontSize: 10.6, charWidth: mono)
        #expect(columns(486, size) == 80)
        #expect(abs(size - 10.1) < 0.001)
    }

    @Test func snapsToNearestColumnCount() {
        #expect(columns(960, ZoomSnapper.snap(width: 960, fontSize: 13.5, charWidth: mono)) == 120)
        #expect(columns(960, ZoomSnapper.snap(width: 960, fontSize: 12.4, charWidth: mono)) == 132)
        #expect(columns(960, ZoomSnapper.snap(width: 960, fontSize: 16, charWidth: mono)) == 100)
    }

    @Test func snapClampsToRange() {
        #expect(ZoomSnapper.snap(width: 320, fontSize: 4, charWidth: mono) == 9)
        #expect(ZoomSnapper.snap(width: 4000, fontSize: 30, charWidth: mono) == 24)
    }

    /// 80 columns on a 402 pt iPhone in portrait need under 9 pt, so no stop is reachable.
    @Test func portraitKeepsPinchedSize() {
        #expect(ZoomSnapper.snap(width: 402, fontSize: 13, charWidth: mono) == 13)
        #expect(ZoomSnapper.snap(width: 402, fontSize: 9.5, charWidth: mono) == 9.5)
    }

    @Test func distantStopKeepsPinchedSize() {
        // 132 columns at 960 pt need 12.1 pt, more than a quarter away from 9 pt.
        #expect(ZoomSnapper.snap(width: 960, fontSize: 9, charWidth: mono) == 9)
    }
}
