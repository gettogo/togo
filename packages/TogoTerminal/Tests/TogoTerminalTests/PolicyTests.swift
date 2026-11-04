import Foundation
import Testing
import UIKit
@testable import TogoTerminal

/// SplitMix64, so the flood is the same bytes on every run.
private struct SeededGenerator: RandomNumberGenerator {
    var state: UInt64
    mutating func next() -> UInt64 {
        state &+= 0x9E37_79B9_7F4A_7C15
        var z = state
        z = (z ^ (z >> 30)) &* 0xBF58_476D_1CE4_E5B9
        z = (z ^ (z >> 27)) &* 0x94D0_49BB_1331_11EB
        return z ^ (z >> 31)
    }
}

@MainActor
@Suite struct PolicyTests {
    @Test func osc52WriteReachesClipboard() async {
        let h = EngineHarness()
        h.feed("\(esc)]52;c;aGk=\u{07}")
        #expect(await waitUntil { !h.clipboardWrites.isEmpty })
        #expect(h.clipboardWrites == ["hi"])
    }

    /// The clipboard holds a known string, so a missing guard would show up as a reply.
    @Test func osc52ReadDenied() async {
        let h = EngineHarness()
        var reads = 0
        h.engine.onClipboardRead = { reads += 1; return "secret" }
        h.feed("\(esc)]52;c;?\u{07}")
        await settle()
        #expect(h.input.isEmpty)
        #expect(reads == 0)
    }

    @Test func osc52ReadAnsweredWhenAllowed() async {
        let h = EngineHarness(settings: TerminalSettings(allowClipboardRead: true))
        h.engine.onClipboardRead = { "secret" }
        h.feed("\(esc)]52;c;?\u{07}")
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("\(esc)]52;c;c2VjcmV0\(esc)\\".utf8))
    }

    @Test func osc8JavascriptLinkIsNotOpened() async throws {
        let h = EngineHarness()
        var opened: [URL] = []
        h.engine.onOpenURL = { opened.append($0) }
        h.feed("\(esc)]8;;javascript:alert(1)\(esc)\\click\(esc)]8;;\(esc)\\")
        try tapCell(h, col: 1, row: 0)
        await settle()
        #expect(opened.isEmpty)
    }

    /// Positive control for the test above: the same tap path does open a web link.
    @Test func osc8HttpsLinkIsOpened() async throws {
        let h = EngineHarness()
        var opened: [URL] = []
        h.engine.onOpenURL = { opened.append($0) }
        h.feed("\(esc)]8;;https://example.com/x\(esc)\\click\(esc)]8;;\(esc)\\")
        try tapCell(h, col: 1, row: 0)
        #expect(await waitUntil { !opened.isEmpty })
        #expect(opened == [URL(string: "https://example.com/x")])
    }

    @Test func linkSchemeAllowlist() throws {
        #expect(LinkPolicy.allows(try #require(URL(string: "https://x"))))
        #expect(LinkPolicy.allows(try #require(URL(string: "http://example.com/a?b=c"))))
        #expect(!LinkPolicy.allows(try #require(URL(string: "javascript:alert(1)"))))
        #expect(!LinkPolicy.allows(try #require(URL(string: "file:///etc"))))
        #expect(!LinkPolicy.allows(try #require(URL(string: "togo://x"))))
    }

    @Test func wheelEventsWhenMouseReporting() async {
        let h = EngineHarness()
        h.feed("\(esc)[?1000h\(esc)[?1006h")
        let cell = h.engine.cellSize
        let point = CGPoint(x: cell.width * 2.5, y: cell.height * 3.5)
        h.engine.pan(by: cell.height * 3, at: point)
        #expect(await waitUntil { h.input.count >= 3 })
        await settle()
        let report = Data("\(esc)[<64;3;4M".utf8)
        #expect(h.input == [report, report, report])
    }

    @Test func wheelDownSendsButton65() async {
        let h = EngineHarness()
        h.feed("\(esc)[?1000h\(esc)[?1006h")
        let cell = h.engine.cellSize
        h.engine.pan(by: -cell.height * 1.5, at: CGPoint(x: 1, y: 1))
        #expect(await waitUntil { !h.input.isEmpty })
        await settle()
        #expect(h.input == [Data("\(esc)[<65;1;1M".utf8)])
    }

    /// SwiftTerm adds its own one-finger wheel pan when tracking starts; two live ones would
    /// report every pan twice.
    @Test func onlyOneOneFingerPanIsLive() async {
        let h = EngineHarness()
        h.feed("\(esc)[?1000h\(esc)[?1006h")
        await settle()
        let livePans = (h.engine.view.gestureRecognizers ?? []).compactMap { $0 as? UIPanGestureRecognizer }
            .filter { $0.isEnabled && $0.maximumNumberOfTouches == 1 }
        #expect(livePans.count == 1)
    }

    @Test func localScrollWithoutMouseReporting() async {
        let h = EngineHarness()
        h.feed((0..<200).map { "line \($0)" }.joined(separator: "\r\n"))
        #expect(h.engine.scrollPosition == 1)
        h.engine.pan(by: h.engine.cellSize.height * 3, at: CGPoint(x: 1, y: 1))
        await settle()
        #expect(h.input.isEmpty)
        #expect(h.engine.scrollPosition < 1)
    }

    @Test func floodDoesNotCrash() async {
        await MainActorHeavyWork.run { floodWithoutCrashing() }
    }

    private func floodWithoutCrashing() {
        let h = EngineHarness()
        var rng = SeededGenerator(state: 0x70_67_6F)
        var chunk = [UInt8](repeating: 0, count: 64 * 1024)
        for _ in 0..<(32 * 1024 * 1024 / chunk.count) {
            for i in chunk.indices { chunk[i] = UInt8(truncatingIfNeeded: rng.next()) }
            h.engine.feed(chunk[...])
        }
        var titles: [String] = []
        h.engine.onTitle = { titles.append($0) }
        let title = "\(esc)]2;" + String(repeating: "t", count: 1024 * 1024) + "\u{07}"
        h.feed(title)
        // CAN aborts any sequence the random bytes left open; RIS resets modes and charsets.
        h.feed("\(esc)\\\u{18}\(esc)c")
        h.feed("ok")
        #expect(h.engine.lineText(0).hasPrefix("ok"))
        // The title is over the 1 MiB OSC cap, so the parser drops it.
        #expect(titles.isEmpty)
    }

    @Test func frameStatsCountsDrops() {
        var stats = FrameStats()
        let step = 1.0 / 120.0
        var t: CFTimeInterval = 1000
        stats.record(timestamp: t)
        for i in 0..<100 {
            t += (i == 30 || i == 70) ? 3 * step : step
            stats.record(timestamp: t)
        }
        #expect(abs(stats.droppedFraction - 0.02) < 1e-9)
        #expect(abs(stats.p95Interval - step) < 1e-6)
    }

    @Test func frameStatsUsesTargetInterval() {
        var stats = FrameStats()
        let step = 1.0 / 60.0
        var t: CFTimeInterval = 0
        for _ in 0..<10 {
            stats.record(timestamp: t, targetTimestamp: t + 1.0 / 120.0)
            t += step
        }
        #expect(stats.droppedFraction == 1)
    }
}

/// A tap recognizer frozen at `.ended` on one point. SwiftTerm's `singleTap:` is where a real
/// finger tap on a link goes, so the test drives that handler rather than calling the
/// engine's link callback directly.
private final class FakeTap: UITapGestureRecognizer {
    let point: CGPoint
    let host: UIView

    init(point: CGPoint, host: UIView) {
        self.point = point
        self.host = host
        super.init(target: nil, action: nil)
    }

    override var state: UIGestureRecognizer.State {
        get { .ended }
        set {}
    }

    override var view: UIView? { host }

    override func location(in view: UIView?) -> CGPoint { point }
}

@MainActor
private func tapCell(_ h: EngineHarness, col: Int, row: Int) throws {
    let selector = Selector(("singleTap:"))
    try #require(h.engine.view.responds(to: selector), "SwiftTerm renamed its tap handler")
    let cell = h.engine.cellSize
    let point = CGPoint(x: (CGFloat(col) + 0.5) * cell.width, y: (CGFloat(row) + 0.5) * cell.height)
    h.engine.view.perform(selector, with: FakeTap(point: point, host: h.engine.view))
}
