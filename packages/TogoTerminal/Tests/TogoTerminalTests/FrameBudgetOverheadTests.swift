import Foundation
import Testing
@testable import TogoTerminal

@MainActor struct FrameBudgetOverheadTests {
    @Test func bookkeepingFitsInsideTheFrameBudget() async {
        let engine = TerminalBindingTests.TimedEngine()
        let ticker = TerminalBindingTests.ManualTicker()
        let source = TerminalBindingTests.PatternSource(total: 1 << 18, chunk: 1 << 18)
        let binding = TerminalBinding(
            engine: engine, ticker: ticker, read: { source.next() }, send: { _ in }, resize: { _, _ in },
            now: {
                // Parsing is not the whole frame: clock reads, adaptive sizing and
                // bookkeeping also consume time between the engine's feeds.
                engine.elapsed += .microseconds(10)
                return engine.elapsed
            }
        )
        binding.start()
        #expect(await eventually { binding.pendingBytes == 1 << 18 })
        for _ in 0..<3 {
            let before = engine.elapsed
            ticker.tick()
            #expect(engine.elapsed - before <= binding.frameBudget)
        }
        #expect(engine.received > 0)
        #expect(binding.pendingBytes == (1 << 18) - engine.received)
        binding.stop()
    }
}
