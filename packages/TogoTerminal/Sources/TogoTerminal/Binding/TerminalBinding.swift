import Darwin
import Foundation

/// Connects a terminal engine to a byte link.
///
/// Keystrokes and resizes go through one stream drained by one task, so the remote end
/// sees them in the order the user made them even when `send` suspends. Output is parsed
/// on the main actor once per display frame for at most `frameBudget`, and what does not
/// fit waits for the next frame, so a burst such as a long diff or `cat /dev/urandom`
/// drains without the screen dropping below its refresh rate. The engine redraws once per
/// frame whatever it was fed. While `maxPending` bytes wait, the reader stops reading,
/// which pushes back on the link and the transport below it.
@MainActor public final class TerminalBinding {
    private enum Event: Sendable {
        case bytes(Data)
        case resize(Int, Int)
    }

    /// One drained burst, for diagnostics: how many frames it took, how long the slowest
    /// parsing took and how many bytes it carried.
    public struct Burst: Sendable, Equatable {
        public var frames: Int
        public var bytes: Int
        public var p95Work: Duration
        public var maxWork: Duration

        public init(frames: Int, bytes: Int, p95Work: Duration, maxWork: Duration) {
            self.frames = frames
            self.bytes = bytes
            self.p95Work = p95Work
            self.maxWork = maxWork
        }
    }

    /// Parsing time per display frame: a third of a 120 Hz frame, leaving the rest to layout,
    /// drawing and touches.
    public static let defaultFrameBudget = Duration.milliseconds(4)
    /// Output allowed to wait before the reader stops reading.
    public static let maxPending = 512 * 1024
    /// Bytes fed before the first timing is known, and the most fed first in any frame: output
    /// can turn dearer between frames, so each frame measures before it feeds more.
    static let firstChunk = 8 * 1024
    static let minChunk = 512
    static let maxChunk = 64 * 1024

    public let frameBudget: Duration
    /// Called when output stops after a burst of at least `burstFrames` frames.
    public var onBurst: (Burst) -> Void = { _ in }
    public static let burstFrames = 30

    private let engine: any TerminalEngine
    private let ticker: any FrameTicker
    private let read: @Sendable () async -> Data?
    private let send: @Sendable (Data) async -> Void
    private let resize: @Sendable (Int, Int) async -> Void
    private let now: () -> Duration

    private var buffer: [UInt8] = []
    private var head = 0
    private var isTicking = false
    /// Estimated parsing cost of one byte, from the frames so far.
    private var nanosPerByte: Double?
    private var burstWork: [Duration] = []
    private var burstBytes = 0
    private var frameWaiters: [CheckedContinuation<Void, Never>] = []
    private var inputTask: Task<Void, Never>?
    private var readTask: Task<Void, Never>?
    private var inputSink: AsyncStream<Event>.Continuation?

    public convenience init(
        engine: any TerminalEngine, ticker: any FrameTicker,
        read: @escaping @Sendable () async -> Data?,
        send: @escaping @Sendable (Data) async -> Void,
        resize: @escaping @Sendable (Int, Int) async -> Void,
        frameBudget: Duration = TerminalBinding.defaultFrameBudget
    ) {
        self.init(
            engine: engine, ticker: ticker, read: read, send: send, resize: resize, frameBudget: frameBudget,
            now: { .nanoseconds(clock_gettime_nsec_np(CLOCK_UPTIME_RAW)) }
        )
    }

    /// `now` is a monotonic time; tests pass a clock the engine advances.
    init(
        engine: any TerminalEngine, ticker: any FrameTicker,
        read: @escaping @Sendable () async -> Data?,
        send: @escaping @Sendable (Data) async -> Void,
        resize: @escaping @Sendable (Int, Int) async -> Void,
        frameBudget: Duration = TerminalBinding.defaultFrameBudget,
        now: @escaping () -> Duration
    ) {
        precondition(frameBudget > .zero, "a zero budget would never feed the engine")
        self.engine = engine
        self.ticker = ticker
        self.read = read
        self.send = send
        self.resize = resize
        self.frameBudget = frameBudget
        self.now = now
    }

    public var pendingBytes: Int { buffer.count - head }

    public func start() {
        guard inputTask == nil else { return }
        let (events, sink) = AsyncStream<Event>.makeStream()
        inputSink = sink
        engine.onInput = { sink.yield(.bytes($0)) }
        engine.onResize = { sink.yield(.resize($0.cols, $0.rows)) }
        inputTask = Task { [send, resize] in
            for await event in events {
                switch event {
                case .bytes(let data): await send(data)
                case .resize(let cols, let rows): await resize(cols, rows)
                }
            }
        }
        ticker.onFrame = { [weak self] in self?.frame() }
        readTask = Task { [weak self, read] in
            while !Task.isCancelled {
                await self?.waitForRoom()
                // A chunk read after stop() is dropped, so it cannot restart the ticker.
                guard !Task.isCancelled, let chunk = await read(), !Task.isCancelled else { return }
                self?.append(chunk)
            }
        }
    }

    public func stop() {
        inputTask?.cancel()
        readTask?.cancel()
        inputTask = nil
        readTask = nil
        inputSink?.finish()
        inputSink = nil
        engine.onInput = { _ in }
        engine.onResize = { _ in }
        ticker.onFrame = {}
        ticker.stop()
        isTicking = false
        wakeFrameWaiters()
    }

    private func append(_ chunk: Data) {
        guard !chunk.isEmpty else { return }
        chunk.withUnsafeBytes { buffer.append(contentsOf: $0.bindMemory(to: UInt8.self)) }
        // The display link runs only while output waits, so an idle terminal costs no frames.
        if !isTicking {
            isTicking = true
            ticker.start()
        }
    }

    /// Feeds pieces sized from the measured cost per byte until the next one would not fit in
    /// what is left of the budget. At least one piece goes in every frame, so output always moves.
    private func frame() {
        let start = now()
        // Parsing cost varies between chunks. Leave the existing 20% sizing margin
        // across the whole frame too, for bookkeeping and the engine's change callbacks.
        let parseBudget = frameBudget * 4 / 5
        var fed = 0
        var size = min(nanosPerByte.map { Self.chunk(for: parseBudget, nanosPerByte: $0) } ?? Self.firstChunk, Self.firstChunk)
        while pendingBytes > 0 {
            let count = min(size, pendingBytes)
            let before = now()
            engine.feed(buffer[head..<head + count])
            let after = now()
            head += count
            fed += count
            learn(cost: after - before, bytes: count)
            let left = parseBudget - (after - start)
            guard left > .zero, let perByte = nanosPerByte else { break }
            size = Self.chunk(for: left, nanosPerByte: perByte)
            guard Double(Self.minChunk) * perByte < Self.nanoseconds(left) * 0.8 else { break }
        }
        let drained = head == buffer.count
        if drained {
            buffer.removeAll(keepingCapacity: true)
            head = 0
            isTicking = false
            ticker.stop()
        } else if head >= Self.maxPending {
            buffer.removeFirst(head)
            head = 0
        }
        wakeFrameWaiters()
        noteFrame(work: now() - start, bytes: fed)
        if drained { endBurst() }
    }

    /// The largest piece expected to parse within `time`, with a margin for variance.
    private static func chunk(for time: Duration, nanosPerByte: Double) -> Int {
        let bytes = nanoseconds(time) * 0.8 / max(nanosPerByte, 0.01)
        return Int(min(max(bytes, Double(minChunk)), Double(maxChunk)))
    }

    private func learn(cost: Duration, bytes: Int) {
        guard bytes >= Self.minChunk else { return }
        let sample = Self.nanoseconds(cost) / Double(bytes)
        // Leans on recent pieces: output changes character, from plain text to dense colour.
        // A dearer piece is believed at once, so the next one is not sized on the cheap text.
        nanosPerByte = nanosPerByte.map { sample > $0 ? sample : $0 * 0.5 + sample * 0.5 } ?? sample
    }

    private static func nanoseconds(_ duration: Duration) -> Double {
        Double(duration.components.seconds) * 1e9 + Double(duration.components.attoseconds) / 1e9
    }

    private func noteFrame(work: Duration, bytes: Int) {
        burstWork.append(work)
        burstBytes += bytes
    }

    private func endBurst() {
        defer {
            burstWork.removeAll(keepingCapacity: true)
            burstBytes = 0
        }
        guard burstWork.count >= Self.burstFrames else { return }
        let sorted = burstWork.sorted()
        let rank = min(sorted.count - 1, Int((Double(sorted.count) * 0.95).rounded(.up)) - 1)
        onBurst(Burst(frames: sorted.count, bytes: burstBytes, p95Work: sorted[rank], maxWork: sorted[sorted.count - 1]))
    }

    private func waitForRoom() async {
        while pendingBytes >= Self.maxPending, readTask != nil {
            await withCheckedContinuation { frameWaiters.append($0) }
        }
    }

    private func wakeFrameWaiters() {
        let waiters = frameWaiters
        frameWaiters = []
        for waiter in waiters { waiter.resume() }
    }
}
