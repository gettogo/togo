import CoreGraphics
import Foundation
import Testing
import UIKit
@testable import TogoTerminal

@MainActor
@Suite struct PointerTests {
    /// A pointer controller over a real engine, with the edit menu recorded instead of shown.
    @MainActor private final class Rig {
        let h = EngineHarness()
        let host: UIView
        let pointer: PointerController
        var menus: [CGPoint] = []

        init() {
            host = UIView(frame: h.engine.view.frame)
            host.addSubview(h.engine.view)
            pointer = PointerController(target: h.engine, host: host)
            pointer.showMenu = { [unowned self] in self.menus.append($0) }
        }

        /// The middle of a cell, in terminal view coordinates.
        func point(col: Int, row: Int) -> CGPoint {
            let cell = h.engine.cellSize
            return CGPoint(x: (CGFloat(col) + 0.5) * cell.width, y: (CGFloat(row) + 0.5) * cell.height)
        }

        var sent: [String] { h.input.map { String(decoding: $0, as: UTF8.self) } }

        /// Delivers a hover sample to the terminal's own hover recognizer target.
        func hover(at point: CGPoint) throws {
            let view = h.engine.view
            let handler = NSSelectorFromString("handleHover:")
            // A renamed SwiftTerm handler fails the test instead of crashing the suite.
            try #require(view.responds(to: handler), "SwiftTerm's hover handler is gone")
            let installed = view.gestureRecognizers?.contains { $0 is UIHoverGestureRecognizer && $0.isEnabled } ?? false
            #expect(installed, "the terminal keeps a hover recognizer")
            let sample = HoverSample(at: point)
            view.perform(handler, with: sample)
        }
    }

    /// A hover recognizer frozen mid-hover at one point.
    private final class HoverSample: UIHoverGestureRecognizer {
        private let point: CGPoint

        init(at point: CGPoint) {
            self.point = point
            super.init(target: nil, action: nil)
        }

        override var state: UIGestureRecognizer.State {
            get { .changed }
            set {}
        }

        override func location(in view: UIView?) -> CGPoint {
            point
        }
    }

    /// X11 numbers the right button 3; on the wire it is button code 2.
    @Test func secondaryClickSendsButtonThreeWhenReporting() async {
        let rig = Rig()
        rig.h.feed("\(esc)[?1000h\(esc)[?1006h")
        rig.pointer.secondaryClick(at: rig.point(col: 2, row: 1))
        #expect(await waitUntil { rig.h.input.count == 2 })
        #expect(rig.sent == ["\(esc)[<2;3;2M", "\(esc)[<2;3;2m"])
        #expect(rig.menus.isEmpty)
    }

    @Test func secondaryClickShowsMenuWhenNotReporting() async {
        let rig = Rig()
        let point = rig.point(col: 2, row: 1)
        rig.pointer.secondaryClick(at: point)
        await settle()
        #expect(rig.h.input.isEmpty)
        #expect(rig.menus == [point])
    }

    /// SwiftTerm's hover recognizer stays installed and reports buttonless motion itself.
    @Test func hoverSendsMotionOnlyInAnyEventMode() async throws {
        for mode in ["1000", "1002"] {
            let rig = Rig()
            rig.h.feed("\(esc)[?\(mode)h\(esc)[?1006h")
            try rig.hover(at: rig.point(col: 0, row: 0))
            try rig.hover(at: rig.point(col: 1, row: 0))
            await settle()
            #expect(rig.h.input.isEmpty, "mode \(mode) asked for no hover motion")
        }

        let rig = Rig()
        rig.h.feed("\(esc)[?1003h\(esc)[?1006h")
        try rig.hover(at: rig.point(col: 1, row: 0))
        try rig.hover(at: rig.point(col: 3, row: 2))
        #expect(await waitUntil { rig.h.input.count == 2 })
        #expect(rig.sent == ["\(esc)[<35;2;1M", "\(esc)[<35;4;3M"])
    }

    @Test func trackpadScrollMapsToWheel() async {
        let rig = Rig()
        let scroll = rig.h.engine.view.gestureRecognizers?.first { recognizer in
            (recognizer as? UIPanGestureRecognizer)?.allowedScrollTypesMask == .all && recognizer.isEnabled
        }
        #expect(scroll != nil, "the terminal takes trackpad and wheel scrolling")

        rig.h.feed("\(esc)[?1000h\(esc)[?1006h")
        let line = rig.h.engine.cellSize.height
        rig.pointer.scroll(by: line * 2, at: rig.point(col: 0, row: 0))
        #expect(await waitUntil { rig.h.input.count == 2 })
        rig.pointer.scroll(by: -line, at: rig.point(col: 0, row: 0))
        #expect(await waitUntil { rig.h.input.count == 3 })
        #expect(rig.sent == ["\(esc)[<64;1;1M", "\(esc)[<64;1;1M", "\(esc)[<65;1;1M"])
    }

    @Test func clickDragSelectsWhenNotReporting() async {
        let rig = Rig()
        rig.h.feed("hello world")
        rig.pointer.dragBegan(at: rig.point(col: 0, row: 0))
        rig.pointer.dragMoved(to: rig.point(col: 4, row: 0))
        rig.pointer.dragEnded(at: rig.point(col: 4, row: 0))
        await settle()
        #expect(rig.h.engine.isSelecting)
        #expect(rig.h.engine.selectedText?.hasPrefix("hello") == true)
        #expect(rig.h.input.isEmpty)
    }

    /// The drag selects what is on screen, not the rows it would cover at the bottom of history.
    @Test func clickDragSelectsTheRowsInViewAfterScrollingBack() async {
        let rig = Rig()
        rig.h.feed((0..<200).map { String(format: "row %03d", $0) }.joined(separator: "\r\n"))
        let bottom = rig.h.engine.lineText(0)
        rig.h.engine.pan(by: rig.h.engine.cellSize.height * 40, at: rig.point(col: 0, row: 0))
        let top = rig.h.engine.lineText(0)
        #expect(top != bottom)
        rig.pointer.dragBegan(at: rig.point(col: 0, row: 0))
        rig.pointer.dragMoved(to: rig.point(col: 6, row: 0))
        rig.pointer.dragEnded(at: rig.point(col: 6, row: 0))
        await settle()
        #expect(rig.h.engine.selectedText == String(top.prefix(7)))
    }

    private func drag(_ rig: Rig, from start: (Int, Int), to end: (Int, Int)) async -> String? {
        rig.pointer.dragBegan(at: rig.point(col: start.0, row: start.1))
        rig.pointer.dragMoved(to: rig.point(col: end.0, row: end.1))
        rig.pointer.dragEnded(at: rig.point(col: end.0, row: end.1))
        await settle()
        // What the user gets is the copy.
        rig.h.engine.copySelection()
        return rig.h.clipboardWrites.last
    }

    /// A drag that ends on the last column keeps its last character, either way round.
    @Test func dragToTheLastColumnKeepsTheLastCharacter() async {
        let rig = Rig()
        let cols = rig.h.engine.size.cols
        let line = String((0..<cols).map { Character(String($0 % 10)) })
        rig.h.feed(line + "\r\nnext")
        let picked1 = await drag(rig, from: (0, 0), to: (cols - 1, 0))
        #expect(picked1 == line)
        let picked2 = await drag(rig, from: (cols - 1, 0), to: (0, 0))
        #expect(picked2 == line)
        let picked3 = await drag(rig, from: (cols - 3, 0), to: (cols - 1, 0))
        #expect(picked3 == String(line.suffix(3)))
    }

    /// A wide glyph is selected whole from either of its halves, at either end of the drag.
    @Test func dragKeepsWideGlyphsWhole() async {
        let rig = Rig()
        // 你 covers columns 2 and 3, 好 columns 4 and 5.
        rig.h.feed("ab你好cd")
        let picked4 = await drag(rig, from: (3, 0), to: (6, 0))
        #expect(picked4 == "你好c")
        let picked5 = await drag(rig, from: (6, 0), to: (3, 0))
        #expect(picked5 == "你好c")
        let picked6 = await drag(rig, from: (0, 0), to: (4, 0))
        #expect(picked6 == "ab你好")
        let picked7 = await drag(rig, from: (0, 0), to: (5, 0))
        #expect(picked7 == "ab你好")
        let picked8 = await drag(rig, from: (5, 0), to: (0, 0))
        #expect(picked8 == "ab你好")
        let picked9 = await drag(rig, from: (5, 0), to: (5, 0))
        #expect(picked9 == "好")
    }

    @Test func clickDragReportsWhenTracking() async {
        let rig = Rig()
        rig.h.feed("\(esc)[?1002h\(esc)[?1006h")
        rig.pointer.dragBegan(at: rig.point(col: 0, row: 0))
        rig.pointer.dragMoved(to: rig.point(col: 1, row: 0))
        rig.pointer.dragEnded(at: rig.point(col: 1, row: 0))
        #expect(await waitUntil { rig.h.input.count == 3 })
        #expect(rig.sent == ["\(esc)[<0;1;1M", "\(esc)[<32;2;1M", "\(esc)[<0;2;1m"])
        #expect(!rig.h.engine.isSelecting)
    }

    @Test func pointerIsAnIBeamOverTheGrid() {
        let rig = Rig()
        let style = rig.pointer.style
        #expect(style.length == rig.h.engine.cellSize.height)
    }
}
