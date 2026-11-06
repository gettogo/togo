import Darwin
import Foundation
import Synchronization
import SwiftTerm
import Testing
import TogoTerminal

/// A 4 MB burst drained through the binding into the real engine, one simulated 120 Hz frame
/// at a time. The ticker is driven by the test, not by a clock, so the frames are exactly the
/// ones the binding asked for; only the work inside each frame is timed.
@MainActor
@Suite(.serialized) struct ThroughputTests {
    /// Forwards to SwiftTerm and checks every byte against the fixture as it arrives.
    final class CheckingEngine: TerminalEngine {
        let inner: SwiftTermEngine
        let expected: [UInt8]
        var received = 0
        var mismatch: Int?
        var feeds = 0

        init(expected: [UInt8]) {
            self.expected = expected
            inner = SwiftTermEngine(settings: TerminalSettings(), theme: BuiltInThemes.all[0])
            inner.view.frame = CGRect(x: 0, y: 0, width: 1_800, height: 600)
            inner.view.layoutIfNeeded()
        }

        var onInput: (Data) -> Void {
            get { inner.onInput }
            set { inner.onInput = newValue }
        }

        var onResize: ((cols: Int, rows: Int)) -> Void {
            get { inner.onResize }
            set { inner.onResize = newValue }
        }

        var size: (cols: Int, rows: Int) { inner.size }

        func feed(_ bytes: ArraySlice<UInt8>) {
            feeds += 1
            if mismatch == nil {
                let end = received + bytes.count
                if end > expected.count || !bytes.elementsEqual(expected[received..<end]) { mismatch = received }
            }
            received += bytes.count
            inner.feed(bytes)
        }

        func send(_ key: KeyStroke) {}
        func paste(_ text: String) {}
    }

    final class SimulatedTicker: FrameTicker {
        var onFrame: () -> Void = {}
        var running = false
        func start() { running = true }
        func stop() { running = false }
    }

    /// Hands the burst out in the 32 KB pieces a transport delivers.
    final class BurstSource: Sendable {
        let bytes: [UInt8]
        let offset = Mutex(0)
        init(_ bytes: [UInt8]) { self.bytes = bytes }
        func next() -> Data? {
            offset.withLock { at in
                guard at < bytes.count else { return nil }
                let end = min(at + 32 * 1024, bytes.count)
                defer { at = end }
                return Data(bytes[at..<end])
            }
        }
    }

    private static func hostLoad() -> Double {
        var load = [Double](repeating: 0, count: 1)
        guard getloadavg(&load, 1) == 1 else { return 0 }
        return load[0] / Double(ProcessInfo.processInfo.activeProcessorCount)
    }

    private static func threadTime() -> Duration {
        .nanoseconds(clock_gettime_nsec_np(CLOCK_THREAD_CPUTIME_ID))
    }

    private static func p95(_ values: [Duration]) -> Duration {
        let sorted = values.sorted()
        guard !sorted.isEmpty else { return .zero }
        return sorted[min(sorted.count - 1, Int((Double(sorted.count) * 0.95).rounded(.up)) - 1)]
    }

    private static func milliseconds(_ duration: Duration) -> Double {
        Double(duration.components.seconds) * 1e3 + Double(duration.components.attoseconds) / 1e15
    }

    @Test func burstDrainsWithinFrameBudget() async {
        let burst = BurstFixture.make(bytes: 4 << 20)
        let engine = CheckingEngine(expected: burst)
        let ticker = SimulatedTicker()
        let source = BurstSource(burst)
        let binding = TerminalBinding(
            engine: engine, ticker: ticker, read: { source.next() }, send: { _ in }, resize: { _, _ in }
        )
        #expect(binding.frameBudget == .milliseconds(4))
        binding.start()
        let clock = ContinuousClock()
        var work: [Duration] = []
        var cpu: [Duration] = []
        var idleTurns = 0
        while engine.received < burst.count, idleTurns < 20_000 {
            guard ticker.running else {
                // The reader has not delivered yet; let it run.
                idleTurns += 1
                await Task.yield()
                continue
            }
            let cpuStart = Self.threadTime()
            work.append(clock.measure { ticker.onFrame() })
            cpu.append(Self.threadTime() - cpuStart)
            // One frame later the reader may top the buffer up.
            await Task.yield()
        }
        binding.stop()

        #expect(engine.received == burst.count, "the engine got \(engine.received) of \(burst.count) bytes")
        #expect(engine.mismatch == nil, "bytes out of order at \(engine.mismatch ?? -1)")
        let wall = Self.p95(work), onCPU = Self.p95(cpu)
        let report = String(
            format: "%d frames, p95 %.2f ms on the main thread's CPU, p95 %.2f ms wall, slowest %.2f ms wall, %d feeds",
            work.count, Self.milliseconds(onCPU), Self.milliseconds(wall), Self.milliseconds(work.max() ?? .zero), engine.feeds
        )
        Attachment.record(report, named: "burst-frames.txt")
        print("Burst drain: \(report)")
        // The work is the main thread's own time in each frame. Wall time also counts the moments
        // the Mac gave the thread's core to something else, so it is reported, and enforced
        // only where `TOGO_ENFORCE_FRAME_BUDGET=1` says the Mac has cores to spare.
        #expect(onCPU < .milliseconds(4), Comment(rawValue: report))
        if ProcessInfo.processInfo.environment["TOGO_ENFORCE_FRAME_BUDGET"] == "1" || Self.hostLoad() <= 1 {
            #expect(wall < .milliseconds(4), Comment(rawValue: report))
        }
    }
}

/// Pieces cut anywhere, inside a CJK character or a colour sequence, draw the same screen as
/// the whole output fed at once. SwiftTerm's view keeps its terminal to itself, so the engine
/// here feeds the same parser without a view.
@MainActor struct SplitSequenceTests {
    final class HeadlessEngine: TerminalEngine, TerminalDelegate {
        var onInput: (Data) -> Void = { _ in }
        var onResize: ((cols: Int, rows: Int)) -> Void = { _ in }
        let size: (cols: Int, rows: Int) = (BurstFixture.columns, 30)
        private(set) var terminal: Terminal!
        private(set) var received = 0

        init() {
            terminal = Terminal(delegate: self, options: TerminalOptions(cols: size.cols, rows: size.rows))
        }

        func feed(_ bytes: ArraySlice<UInt8>) {
            received += bytes.count
            terminal.feed(buffer: bytes)
        }
        func send(_ key: KeyStroke) {}
        func paste(_ text: String) {}
        nonisolated func send(source: Terminal, data: ArraySlice<UInt8>) {}

        /// Every visible cell: its character and its attributes.
        var screen: [String] {
            (0..<terminal.rows).map { row in
                (0..<terminal.cols).map { col in
                    guard let cell = terminal.getCharData(col: col, row: row) else { return "" }
                    return "\(cell.getCharacter())|\(cell.attribute)"
                }.joined(separator: ",")
            }
        }
    }

    @Test func splitInsideCJKAndColourDrawsTheSameScreen() throws {
        let text = "a 読み込み \u{1b}[38;2;12;200;99mcolour\u{1b}[39m 완료 \u{1b}[1;35mbold\u{1b}[0m 🙂 end\r\n"
        let bytes = Array(text.utf8)
        let whole = HeadlessEngine()
        whole.feed(bytes[...])
        let expected = whole.screen
        #expect(expected[0].contains("読"), "the whole output drew nothing")
        let cjk = try #require(bytes.firstRange(of: Array("込".utf8))).lowerBound
        let colour = try #require(bytes.firstRange(of: Array("38;2".utf8))).lowerBound
        for cut in [cjk + 1, cjk + 2, colour + 3] + Array(1..<bytes.count) {
            let pieces = HeadlessEngine()
            pieces.feed(bytes[..<cut])
            pieces.feed(bytes[cut...])
            #expect(pieces.screen == expected, "cut at byte \(cut) drew a different screen")
        }
    }

    @Test func burstThroughBindingDrawsTheSameScreen() async {
        let burst = BurstFixture.make(bytes: 256 << 10)
        let whole = HeadlessEngine()
        whole.feed(burst[...])
        let pieces = HeadlessEngine()
        let ticker = ThroughputTests.SimulatedTicker()
        let source = ThroughputTests.BurstSource(burst)
        let binding = TerminalBinding(engine: pieces, ticker: ticker, read: { source.next() }, send: { _ in }, resize: { _, _ in })
        binding.start()
        var turns = 0
        // Until the engine has every byte: the last chunk read may still be on its way in.
        while pieces.received < burst.count, turns < 20_000 {
            turns += 1
            if ticker.running { ticker.onFrame() }
            await Task.yield()
        }
        binding.stop()
        #expect(pieces.received == burst.count, "the burst did not drain")
        #expect(pieces.screen == whole.screen)
    }
}
