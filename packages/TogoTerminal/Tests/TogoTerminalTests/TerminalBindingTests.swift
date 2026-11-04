import Foundation
import Synchronization
import Testing
@testable import TogoTerminal

@MainActor
@Suite(.serialized) struct TerminalBindingTests {
    final class FakeEngine: TerminalEngine {
        var onInput: (Data) -> Void = { _ in }
        var onResize: ((cols: Int, rows: Int)) -> Void = { _ in }
        var size: (cols: Int, rows: Int) = (80, 24)
        var feeds: [Int] = []
        var received = 0
        var outOfOrder = false

        func feed(_ bytes: ArraySlice<UInt8>) {
            // The source writes a counting pattern; checking the ends of each slice keeps
            // the fake cheap enough that it does not dominate main-thread time.
            feeds.append(bytes.count)
            if bytes.first != UInt8(truncatingIfNeeded: received)
                || bytes.last != UInt8(truncatingIfNeeded: received + bytes.count - 1) {
                outOfOrder = true
            }
            received += bytes.count
        }

        func send(_ key: KeyStroke) {}
        func paste(_ text: String) {}
    }

    final class ManualTicker: FrameTicker {
        var onFrame: () -> Void = {}
        var running = false
        func start() { running = true }
        func stop() { running = false }
        func tick() { onFrame() }
    }

    /// Fires `onFrame` from a 120 Hz main-actor timer, like a display link.
    final class TimerTicker: FrameTicker {
        var onFrame: () -> Void = {}
        private var task: Task<Void, Never>?
        func start() {
            guard task == nil else { return }
            task = Task { [weak self] in
                while !Task.isCancelled {
                    try? await Task.sleep(for: .microseconds(8_333))
                    self?.onFrame()
                }
            }
        }
        func stop() {
            task?.cancel()
            task = nil
        }
    }

    final class Outbox: Sendable {
        enum Item: Equatable, Sendable { case bytes(Data), resize(Int, Int) }
        let items = Mutex<[Item]>([])
        var all: [Item] { items.withLock { $0 } }
    }

    /// Yields `total` bytes of a counting pattern in `chunk`-sized pieces, then nil.
    /// With a chunk size divisible by 256 every piece is the same, so it is built once.
    final class PatternSource: Sendable {
        let state = Mutex(0)
        let total: Int
        let block: Data
        init(total: Int, chunk: Int) {
            precondition(chunk % 256 == 0 && total % chunk == 0)
            self.total = total
            block = Data((0..<chunk).map { UInt8(truncatingIfNeeded: $0) })
        }
        func next() -> Data? {
            state.withLock { offset in
                guard offset < total else { return nil }
                offset += block.count
                return block
            }
        }
    }

    static func binding(
        _ engine: FakeEngine, _ ticker: any FrameTicker, outbox: Outbox = Outbox(),
        read: @escaping @Sendable () async -> Data? = { try? await Task.sleep(for: .seconds(60)); return nil }
    ) -> TerminalBinding {
        TerminalBinding(
            engine: engine, ticker: ticker, read: read,
            send: { data in
                // Suspends a varying amount so a reordering bug would show.
                if Bool.random() { await Task.yield() }
                outbox.items.withLock { $0.append(.bytes(data)) }
            },
            resize: { cols, rows in outbox.items.withLock { $0.append(.resize(cols, rows)) } }
        )
    }

    @Test func inputOrderPreserved() async {
        let engine = FakeEngine(), outbox = Outbox()
        let binding = Self.binding(engine, ManualTicker(), outbox: outbox)
        binding.start()
        for value in 0..<1_000 { engine.onInput(Data([UInt8(truncatingIfNeeded: value)])) }
        #expect(await eventually { outbox.all.count == 1_000 })
        let bytes = outbox.all.flatMap { item -> [UInt8] in if case .bytes(let data) = item { Array(data) } else { [] } }
        #expect(bytes == (0..<1_000).map { UInt8(truncatingIfNeeded: $0) })
        binding.stop()
    }

    @Test func resizeForwardedInOrder() async {
        let engine = FakeEngine(), outbox = Outbox()
        let binding = Self.binding(engine, ManualTicker(), outbox: outbox)
        binding.start()
        engine.onInput(Data("a".utf8))
        engine.onResize((100, 30))
        engine.onInput(Data("b".utf8))
        #expect(await eventually { outbox.all.count == 3 })
        #expect(outbox.all == [.bytes(Data("a".utf8)), .resize(100, 30), .bytes(Data("b".utf8))])
        binding.stop()
    }

    @Test func feedPerFrameAtMostBudget() async {
        let engine = FakeEngine(), ticker = ManualTicker()
        let source = PatternSource(total: 1 << 20, chunk: 1 << 20)
        let binding = Self.binding(engine, ticker, read: { source.next() })
        binding.start()
        #expect(await eventually { binding.pendingBytes == 1 << 20 })
        ticker.tick()
        #expect(engine.feeds == [256 * 1024])
        #expect(binding.pendingBytes == 768 * 1024)
        binding.stop()
    }

    @Test func floodThroughBindingKeepsMainResponsive() async {
        await MainActorHeavyWork.run { await flood() }
    }

    private func flood() async {
        let engine = FakeEngine()
        let total = 32 << 20, chunk = 64 << 10
        let source = PatternSource(total: total, chunk: chunk)
        let binding = Self.binding(engine, TimerTicker(), read: { source.next() })
        var peakPending = 0
        var worstPing = Duration.zero
        binding.start()
        // Bounded by polls, not wall time, for the same reason as `eventually`.
        var polls = 0
        while engine.received < total, polls < 6_000 {
            polls += 1
            let sent = ContinuousClock.now
            try? await Task.sleep(for: .milliseconds(10))
            worstPing = max(worstPing, ContinuousClock.now - sent - .milliseconds(10))
            peakPending = max(peakPending, binding.pendingBytes)
        }
        binding.stop()
        #expect(engine.received == total)
        #expect(!engine.outOfOrder)
        #expect(peakPending <= 2 * 256 * 1024 + chunk)
        Attachment.record("worst main-actor ping: \(worstPing)", named: "flood-ping.txt")
        // Unoptimised Debug code parses several times slower; the 50 ms target is for Release.
        #if DEBUG
        let bound = Duration.milliseconds(150)
        #else
        let bound = Duration.milliseconds(50)
        #endif
        #expect(worstPing <= bound, "main actor answered after \(worstPing)")
    }

    @Test func stopRestoresEngineCallbacks() async {
        let engine = FakeEngine(), outbox = Outbox()
        let binding = Self.binding(engine, ManualTicker(), outbox: outbox)
        binding.start()
        binding.stop()
        engine.onInput(Data("x".utf8))
        try? await Task.sleep(for: .milliseconds(20))
        #expect(outbox.all.isEmpty)
    }

    @Test func tickerRunsOnlyWhileOutputWaits() async {
        let engine = FakeEngine(), ticker = ManualTicker()
        let source = PatternSource(total: 1_024, chunk: 1_024)
        let binding = Self.binding(engine, ticker, read: { source.next() })
        binding.start()
        #expect(await eventually { ticker.running })
        ticker.tick()
        #expect(engine.received == 1_024)
        #expect(!ticker.running)
        binding.stop()
    }
}

/// Polls a fixed number of times rather than until a wall-clock deadline: other suites
/// share the main actor, and while one of them holds it, a deadline would expire without
/// the code under test ever getting a turn.
@MainActor
func eventually(polls: Int = 1_500, _ condition: () -> Bool) async -> Bool {
    for _ in 0..<polls {
        if condition() { return true }
        try? await Task.sleep(for: .milliseconds(2))
    }
    return condition()
}

/// Tests that hold the main actor for long stretches take turns here. `.serialized` orders
/// tests only within one suite, and a flood in another suite would show up as a slow ping.
@MainActor
enum MainActorHeavyWork {
    private static var busy = false
    private static var waiters: [CheckedContinuation<Void, Never>] = []

    static func run(_ body: () async -> Void) async {
        while busy { await withCheckedContinuation { waiters.append($0) } }
        busy = true
        await body()
        busy = false
        if !waiters.isEmpty { waiters.removeFirst().resume() }
    }
}
