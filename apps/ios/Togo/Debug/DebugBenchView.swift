#if DEBUG
import Observation
import QuartzCore
import SwiftUI
import Synchronization
import TogoTerminal

/// Replays a recorded Claude Code stream at 16 KiB per display frame, then scrolls the
/// buffer for 5 s, and reports frame pacing for both.
@MainActor @Observable final class DebugBenchModel {
    static let chunk = 16 * 1024
    static let fillerLines = 10_000

    let engine = SwiftTermEngine(settings: TerminalSettings(), theme: BuiltInThemes.all[0])
    let controller: TerminalViewController
    private(set) var status = "Ready"
    private(set) var replay: String?
    private(set) var scroll: String?
    private(set) var running = false

    init() {
        controller = TerminalViewController(engine: engine)
    }

    func run() async {
        guard !running else { return }
        running = true
        defer { running = false }
        replay = nil
        scroll = nil
        if let url = Bundle.main.url(forResource: "claude-stream", withExtension: "bin"),
           let data = try? Data(contentsOf: url, options: .mappedIfSafe) {
            status = "Replaying \(data.count / 1024) KiB…"
            replay = Self.describe(await pacedReplay(data))
        } else {
            replay = "No recording in this build (Debug/Resources/claude-stream.bin)."
        }
        status = "Filling the buffer…"
        var filler = ""
        for line in 1...Self.fillerLines { filler += "line \(line)\r\n" }
        engine.feed(ArraySlice(Array(filler.utf8)))
        status = "Scrolling for 5 s…"
        scroll = Self.describe(await scrollPass())
        status = "Done"
    }

    private func pacedReplay(_ data: Data) async -> FrameStats {
        let source = ChunkSource(data: data, chunk: Self.chunk)
        let ticker = DisplayLinkTicker()
        ticker.recordsFrameStats = true
        let binding = TerminalBinding(
            engine: engine, ticker: ticker,
            read: { source.next() }, send: { _ in }, resize: { _, _ in }
        )
        binding.start()
        while !source.isDone || binding.pendingBytes > 0 {
            try? await Task.sleep(for: .milliseconds(20))
        }
        let stats = ticker.frameStats
        binding.stop()
        return stats
    }

    private func scrollPass() async -> FrameStats? {
        guard let scrollView = engine.view as? UIScrollView else { return nil }
        let ticker = DisplayLinkTicker()
        ticker.recordsFrameStats = true
        let start = CACurrentMediaTime()
        let range = max(scrollView.contentSize.height - scrollView.bounds.height, 0)
        ticker.onFrame = {
            // A triangle wave: down through the whole buffer and back each 2.5 s.
            let phase = (CACurrentMediaTime() - start).truncatingRemainder(dividingBy: 2.5) / 2.5
            let position = phase < 0.5 ? phase * 2 : (1 - phase) * 2
            scrollView.contentOffset.y = range * position
        }
        ticker.start()
        try? await Task.sleep(for: .seconds(5))
        ticker.stop()
        return ticker.frameStats
    }

    private static func describe(_ stats: FrameStats?) -> String {
        guard let stats else { return "No scroll view to drive." }
        let p95 = stats.p95Interval * 1000
        let dropped = stats.droppedFraction * 100
        return String(format: "p95 frame interval %.1f ms, %.1f %% of frames dropped", p95, dropped)
    }
}

/// Hands out a recording in fixed pieces, then nil.
private final class ChunkSource: Sendable {
    private let data: Data
    private let chunk: Int
    private let offset = Mutex(0)

    init(data: Data, chunk: Int) {
        self.data = data
        self.chunk = chunk
    }

    var isDone: Bool { offset.withLock { $0 >= data.count } }

    func next() -> Data? {
        offset.withLock { offset in
            guard offset < data.count else { return nil }
            let end = min(offset + chunk, data.count)
            defer { offset = end }
            return data.subdata(in: offset..<end)
        }
    }
}

struct DebugBenchView: View {
    @State private var model = DebugBenchModel()

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(model.status).font(.headline)
            if let replay = model.replay { Text("Paced replay: \(replay)").font(.caption.monospaced()) }
            if let scroll = model.scroll { Text("Scroll pass: \(scroll)").font(.caption.monospaced()) }
            Button("Run bench") { Task { await model.run() } }.disabled(model.running)
            TerminalHost(controller: model.controller)
        }
        .padding(.horizontal)
        .navigationTitle("Rendering bench")
        .navigationBarTitleDisplayMode(.inline)
    }
}
#endif
