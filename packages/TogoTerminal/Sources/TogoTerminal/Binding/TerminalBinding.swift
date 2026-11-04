import Foundation

/// Connects a terminal engine to a byte link.
///
/// Keystrokes and resizes go through one stream drained by one task, so the remote end
/// sees them in the order the user made them even when `send` suspends. Output is
/// parsed on the main actor, at most `frameBudget` bytes per display frame, so a flood
/// such as `cat /dev/urandom` cannot hold the main thread for longer than one frame's
/// worth of parsing. While two frames of output are waiting, the reader stops reading,
/// which pushes back on the link and the transport below it.
@MainActor public final class TerminalBinding {
    private enum Event: Sendable {
        case bytes(Data)
        case resize(Int, Int)
    }

    private let engine: any TerminalEngine
    private let ticker: any FrameTicker
    private let read: @Sendable () async -> Data?
    private let send: @Sendable (Data) async -> Void
    private let resize: @Sendable (Int, Int) async -> Void
    private let frameBudget: Int

    private var buffer: [UInt8] = []
    private var head = 0
    private var isTicking = false
    private var frameWaiters: [CheckedContinuation<Void, Never>] = []
    private var inputTask: Task<Void, Never>?
    private var readTask: Task<Void, Never>?
    private var inputSink: AsyncStream<Event>.Continuation?

    public init(
        engine: any TerminalEngine, ticker: any FrameTicker,
        read: @escaping @Sendable () async -> Data?,
        send: @escaping @Sendable (Data) async -> Void,
        resize: @escaping @Sendable (Int, Int) async -> Void,
        frameBudget: Int = 256 * 1024
    ) {
        precondition(frameBudget > 0, "a zero budget would never feed the engine")
        self.engine = engine
        self.ticker = ticker
        self.read = read
        self.send = send
        self.resize = resize
        self.frameBudget = frameBudget
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

    private func frame() {
        let count = min(frameBudget, pendingBytes)
        if count > 0 {
            engine.feed(buffer[head..<head + count])
            head += count
        }
        if head == buffer.count {
            buffer.removeAll(keepingCapacity: true)
            head = 0
            isTicking = false
            ticker.stop()
        } else if head >= frameBudget * 4 {
            buffer.removeFirst(head)
            head = 0
        }
        wakeFrameWaiters()
    }

    private func waitForRoom() async {
        while pendingBytes >= 2 * frameBudget, readTask != nil {
            await withCheckedContinuation { frameWaiters.append($0) }
        }
    }

    private func wakeFrameWaiters() {
        let waiters = frameWaiters
        frameWaiters = []
        for waiter in waiters { waiter.resume() }
    }
}
