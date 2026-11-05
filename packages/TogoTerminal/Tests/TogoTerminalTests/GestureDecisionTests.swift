import CoreGraphics
import Foundation
import Testing
import UIKit
@testable import TogoTerminal

@Suite struct TwoFingerIntentTests {
    @Test func sidewaysDragIsSwipe() {
        #expect(TwoFingerIntent.classify(translation: CGPoint(x: -80, y: 10), spreadChange: 2, startGap: 60) == .swipe)
    }

    @Test func verticalDragStaysScroll() {
        #expect(TwoFingerIntent.classify(translation: CGPoint(x: 10, y: 90), spreadChange: 3, startGap: 60) == .scroll)
        #expect(TwoFingerIntent.classify(translation: CGPoint(x: 40, y: 60), spreadChange: 0, startGap: 60) == .scroll)
    }

    @Test func spreadingFingersIsPinch() {
        #expect(TwoFingerIntent.classify(translation: CGPoint(x: 12, y: 3), spreadChange: 40, startGap: 80) == .pinch)
        #expect(TwoFingerIntent.classify(translation: CGPoint(x: 0, y: 10), spreadChange: -25, startGap: 80) == .pinch)
    }

    /// Recognizers decide after about 10 pt of travel, when a swipe's gap has drifted a few points.
    @Test func earlyGapDriftStaysSwipe() {
        #expect(TwoFingerIntent.classify(translation: CGPoint(x: 10, y: 1), spreadChange: 5, startGap: 60) == .swipe)
        #expect(TwoFingerIntent.classify(translation: CGPoint(x: 10, y: 1), spreadChange: -9.9, startGap: 60) == .swipe)
    }

    @Test func pinchBoundaryNeedsTravelAndGapShare() {
        #expect(TwoFingerIntent.classify(translation: CGPoint(x: 10, y: 0), spreadChange: 10, startGap: 60) == .pinch)
        // Matches the travel but is under a tenth of a 120 pt gap.
        #expect(TwoFingerIntent.classify(translation: CGPoint(x: 10, y: 0), spreadChange: 10, startGap: 120) == .swipe)
        #expect(TwoFingerIntent.classify(translation: .zero, spreadChange: 12, startGap: 120) == .pinch)
        #expect(TwoFingerIntent.classify(translation: .zero, spreadChange: 11, startGap: 120) == .scroll)
    }

    @Test func reusedTouchMeasuresFromItsOwnStart() {
        let first = NSObject()
        let second = NSObject()
        var starts = TouchStarts()
        starts.began(ObjectIdentifier(first), at: CGPoint(x: 0, y: 0))
        starts.began(ObjectIdentifier(second), at: CGPoint(x: 50, y: 0))
        starts.ended(ObjectIdentifier(first))
        starts.ended(ObjectIdentifier(second))
        #expect(starts.pair == nil)
        // UIKit hands out the same touch objects again for the next gesture.
        starts.began(ObjectIdentifier(first), at: CGPoint(x: 200, y: 300))
        #expect(starts.pair == nil)
        starts.began(ObjectIdentifier(second), at: CGPoint(x: 260, y: 300))
        let pair = starts.pair.map { [$0.0, $0.1].sorted { $0.x < $1.x } }
        #expect(pair == [CGPoint(x: 200, y: 300), CGPoint(x: 260, y: 300)])
    }

    @Test func beganAgainOverwritesAStaleStart() {
        let touch = NSObject()
        let other = NSObject()
        var starts = TouchStarts()
        starts.began(ObjectIdentifier(touch), at: CGPoint(x: 1, y: 1))
        starts.began(ObjectIdentifier(touch), at: CGPoint(x: 9, y: 9))
        starts.began(ObjectIdentifier(other), at: CGPoint(x: 20, y: 9))
        #expect(starts.pair.map { [$0.0, $0.1].contains(CGPoint(x: 9, y: 9)) } == true)
        starts.reset()
        #expect(starts.pair == nil)
    }

    @Test func swipeNeedsDistanceOrSpeed() {
        #expect(TwoFingerIntent.swipeDirection(translation: CGPoint(x: -70, y: 5), velocity: .zero) == .left)
        #expect(TwoFingerIntent.swipeDirection(translation: CGPoint(x: 20, y: 0), velocity: CGPoint(x: 900, y: 0)) == .right)
        #expect(TwoFingerIntent.swipeDirection(translation: CGPoint(x: 20, y: 0), velocity: CGPoint(x: 100, y: 0)) == nil)
        #expect(TwoFingerIntent.swipeDirection(translation: CGPoint(x: 70, y: 60), velocity: .zero) == nil)
    }
}

@MainActor
@Suite struct MouseTrackingTests {
    /// Press, motion, release at neighbouring cells; returns how many reports went out.
    func reports(after modes: String) async -> [String] {
        let h = EngineHarness()
        h.feed(modes)
        h.engine.sendMouse(button: 0, release: false, motion: false, at: GridCell(col: 0, row: 0))
        h.engine.sendMouse(button: 0, release: false, motion: true, at: GridCell(col: 1, row: 0))
        h.engine.sendMouse(button: 0, release: true, motion: false, at: GridCell(col: 1, row: 0))
        await settle()
        return h.input.map { String(decoding: $0, as: UTF8.self) }
    }

    @Test func nothingWithoutMouseMode() async {
        #expect(await reports(after: "") == [])
    }

    @Test func x10SendsPressesOnly() async {
        #expect(await reports(after: "\(esc)[?9h").count == 1)
    }

    @Test func normalTrackingSendsPressAndRelease() async {
        #expect(await reports(after: "\(esc)[?1000h\(esc)[?1006h") == ["\(esc)[<0;1;1M", "\(esc)[<0;2;1m"])
    }

    @Test func buttonEventTrackingSendsDrag() async {
        #expect(await reports(after: "\(esc)[?1002h\(esc)[?1006h") == [
            "\(esc)[<0;1;1M", "\(esc)[<32;2;1M", "\(esc)[<0;2;1m",
        ])
    }

    @Test func anyEventTrackingSendsDrag() async {
        #expect(await reports(after: "\(esc)[?1003h\(esc)[?1006h").count == 3)
    }

    @Test func twoFingerTapInPlainShellSendsNothing() async {
        let h = EngineHarness()
        let host = UIView(frame: h.engine.view.frame)
        host.addSubview(h.engine.view)
        let gestures = GestureController(engine: h.engine, view: host, bindings: GestureConfig(touchMouse: true))
        gestures.handleRightClick(at: CGPoint(x: 5, y: 5))
        await settle()
        #expect(h.input.isEmpty)

        h.feed("\(esc)[?1000h\(esc)[?1006h")
        gestures.handleRightClick(at: CGPoint(x: 5, y: 5))
        #expect(await waitUntil { h.input.count == 2 })
        #expect(h.input.map { String(decoding: $0, as: UTF8.self) } == ["\(esc)[<2;1;1M", "\(esc)[<2;1;1m"])
    }
}
