import Foundation
import Testing
import UIKit
@testable import TogoTerminal

@MainActor
@Suite struct TapBindingsTests {
    func gestures(_ h: EngineHarness, _ config: GestureConfig = GestureConfig()) -> GestureController {
        let host = UIView(frame: h.engine.view.frame)
        host.addSubview(h.engine.view)
        return GestureController(engine: h.engine, view: host, bindings: config)
    }

    @Test func doubleTapSendsTab() async {
        let h = EngineHarness()
        let controller = gestures(h)
        #expect(controller.handleTaps(2))
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data([0x09]))
    }

    @Test func tripleTapSendsEscape() async {
        let h = EngineHarness()
        let controller = gestures(h)
        #expect(controller.handleTaps(3))
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data([0x1b]))
    }

    @Test func tapsSuppressedDuringSelection() async {
        let h = EngineHarness()
        let controller = gestures(h)
        h.feed("hello")
        h.engine.selectAll()
        #expect(!controller.handleTaps(2))
        #expect(!controller.handleTaps(3))
        await settle()
        #expect(h.input.isEmpty)
    }

    @Test func unboundTapLeavesSelectionToTheTerminal() {
        let bindings = TapBindings(doubleTap: nil, tripleTap: [0x1b])
        #expect(bindings.sequence(forTaps: 2, isSelecting: false) == nil)
        #expect(bindings.sequence(forTaps: 3, isSelecting: false) == [0x1b])
        #expect(bindings.sequence(forTaps: 3, isSelecting: true) == nil)
        #expect(bindings.sequence(forTaps: 1, isSelecting: false) == nil)
    }

    @Test func tapOnBareURLFindsLink() throws {
        let h = EngineHarness()
        h.feed("see https://example.com/a. now")
        let cell = h.engine.cellSize
        let onURL = CGPoint(x: cell.width * 8.5, y: cell.height * 0.5)
        #expect(h.engine.bareLink(at: onURL) == URL(string: "https://example.com/a"))
        #expect(h.engine.bareLink(at: CGPoint(x: cell.width * 1.5, y: cell.height * 0.5)) == nil)
    }

    @Test func bareLinkTrimsProsePunctuation() {
        #expect(BareLink.url(in: "(see https://a.dev/x)", at: 6) == URL(string: "https://a.dev/x"))
        #expect(BareLink.url(in: "https://en.wikipedia.org/wiki/Tmux_(software)", at: 3)
            == URL(string: "https://en.wikipedia.org/wiki/Tmux_(software)"))
        #expect(BareLink.url(in: "javascript://alert", at: 3) == nil)
    }
}
