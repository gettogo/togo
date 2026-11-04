import Foundation

public enum LinkState: Sendable, Equatable {
    case waking(String)
    case connecting
    case live(TransportKind, notes: [FallbackNote])
    case roaming
    case reconnecting(attempt: Int)
    case failed(String)
    /// The tmux session behind this link no longer exists; nothing will reconnect it.
    case ended
}

public typealias LinkConnect = @Sendable (
    _ cols: Int, _ rows: Int, _ onWaking: @escaping @Sendable (String) -> Void
) async throws -> Attachment

/// Keeps one terminal attached across drops, network changes and backgrounding.
///
/// Each attempt gets a new `Attachment` from `connect`. A generation number tags the
/// current one, so output, state changes and failures from a replaced attachment are
/// ignored, and the user never sees bytes from a dead connection after the new screen.
/// Input is accepted only while live, so keystrokes typed during a reconnect are not
/// replayed into the next attach. Roaming counts as live: mosh queues and predicts
/// keystrokes itself while the network is out.
///
/// A drop never ends the link by itself. Only the helper reporting that the tmux session
/// is gone does, so a remote exit is answered with one more attach to find out.
public actor TerminalLink {
    static let backoff: [Duration] = [.zero, .milliseconds(500), .seconds(1), .seconds(2), .seconds(4), .seconds(8)]
    static let resizeQuiet: Duration = .milliseconds(100)
    static let backgroundReconnect: Duration = .seconds(5)
    /// A link must stay live this long before a drop starts the backoff from zero again,
    /// so an attach that connects and then exits at once does not loop without waiting.
    static let stableLive: Duration = .seconds(10)
    /// Output held for `read()` before the transport is made to wait.
    static let outputLimit = 1 << 20

    public nonisolated let states: AsyncStream<LinkState>
    private let stateSink: AsyncStream<LinkState>.Continuation
    private let connect: LinkConnect
    private let path: any NetworkPathSource
    private let clock: any LinkClock

    private var size: (cols: Int, rows: Int)
    private var sentSize: (cols: Int, rows: Int)?
    private var current: Attachment?
    private var generation = 0
    private var isLive = false
    private var isStopped = false
    /// The attempt number that made the current attachment, and when it went live.
    private var streak = 0
    private var liveSince: ContinuousClock.Instant?
    private var backgroundedAt: ContinuousClock.Instant?
    public private(set) var resizesSent = 0

    private var attempt: Task<Void, Never>?
    private var resizeTask: Task<Void, Never>?
    private var pathTask: Task<Void, Never>?
    private var generationTasks: [Task<Void, Never>] = []

    private var output: [Data] = []
    private var outputBytes = 0
    private var reader: CheckedContinuation<Data?, Never>?
    private var roomWaiters: [CheckedContinuation<Void, Never>] = []

    public init(
        connect: @escaping LinkConnect, path: any NetworkPathSource, clock: any LinkClock = SystemClock(),
        cols: Int, rows: Int
    ) {
        self.connect = connect
        self.path = path
        self.clock = clock
        size = (cols, rows)
        (states, stateSink) = AsyncStream<LinkState>.makeStream()
    }

    // MARK: - Public API

    public func start() async {
        guard attempt == nil, current == nil, !isStopped else { return }
        let changes = path.changes
        pathTask = Task { [weak self] in
            for await _ in changes { await self?.pathChanged() }
        }
        beginAttempts(from: 0)
    }

    /// Single consumer: TerminalBinding. Returns nil after `stop()` or when the caller is cancelled.
    public func read() async -> Data? {
        if let chunk = takeOutput() { return chunk }
        if isStopped || Task.isCancelled { return nil }
        return await withTaskCancellationHandler {
            await withCheckedContinuation { continuation in
                precondition(reader == nil, "TerminalLink.read supports exactly one consumer")
                reader = continuation
            }
        } onCancel: {
            Task { await self.cancelRead() }
        }
    }

    public func send(_ data: Data) async {
        guard isLive, let transport = current?.transport else { return }
        try? await transport.send(data)
    }

    public func resize(cols: Int, rows: Int) async {
        size = (cols, rows)
        resizeTask?.cancel()
        resizeTask = Task { [clock] in
            do { try await clock.sleep(for: Self.resizeQuiet) } catch { return }
            await self.flushResize()
        }
    }

    public func didEnterBackground() async {
        backgroundedAt = clock.now
    }

    /// Mosh survives a suspended app by itself; an SSH channel left idle in the
    /// background is usually dead without having noticed, so it is replaced.
    public func willEnterForeground() async {
        defer { backgroundedAt = nil }
        guard let since = backgroundedAt, clock.now - since >= Self.backgroundReconnect else { return }
        if current?.kind == .ssh { reconnect() }
    }

    public func stop() async {
        guard !isStopped else { return }
        isStopped = true
        attempt?.cancel()
        attempt = nil
        resizeTask?.cancel()
        pathTask?.cancel()
        retire()
        reader?.resume(returning: nil)
        reader = nil
        stateSink.finish()
    }

    // MARK: - Attempts

    private func beginAttempts(from first: Int) {
        attempt = Task { await self.runAttempts(from: first) }
    }

    private func reconnect() {
        guard !isStopped, attempt == nil else { return }
        let stable = liveSince.map { clock.now - $0 >= Self.stableLive } ?? false
        let next = stable ? 1 : streak + 1
        retire()
        beginAttempts(from: next)
    }

    private func runAttempts(from first: Int) async {
        var number = first
        while !isStopped, !Task.isCancelled {
            if number == 0 {
                stateSink.yield(.connecting)
            } else {
                stateSink.yield(.reconnecting(attempt: number))
                let delay = Self.backoff[min(number - 1, Self.backoff.count - 1)]
                do { try await clock.sleep(for: delay) } catch { return }
            }
            let requested = size
            let sink = stateSink
            do {
                let attachment = try await connect(requested.cols, requested.rows) { sink.yield(.waking($0)) }
                guard !isStopped, !Task.isCancelled else {
                    await Self.close(attachment)
                    return
                }
                try await adopt(attachment, requested: requested, number: number)
                return
            } catch {
                if isStopped || Task.isCancelled { return }
                if let error = error as? HelperError, error.isSessionGone {
                    attempt = nil
                    stateSink.yield(.ended)
                    return
                }
                if number == 0 || Self.isPermanent(error) {
                    attempt = nil
                    stateSink.yield(.failed(error.localizedDescription))
                    return
                }
                number += 1
            }
        }
    }

    /// The new transport learns the latest size before anything reads its output, so the
    /// first screen it paints is already laid out for the current window.
    private func adopt(_ attachment: Attachment, requested: (cols: Int, rows: Int), number: Int) async throws {
        if requested != size {
            let latest = size
            do {
                try await attachment.transport.resize(cols: latest.cols, rows: latest.rows)
            } catch {
                await Self.close(attachment)
                throw error
            }
            // stop() may have run during the resize; a stopped link never takes an attachment.
            guard !isStopped, !Task.isCancelled else {
                await Self.close(attachment)
                return
            }
            resizesSent += 1
            sentSize = latest
        } else {
            sentSize = requested
        }
        generation += 1
        let tag = generation
        current = attachment
        isLive = true
        streak = number
        liveSince = clock.now
        // From here a drop starts a new run of attempts, so this one must no longer count.
        attempt = nil
        stateSink.yield(.live(attachment.kind, notes: attachment.notes))
        generationTasks = [
            Task { await self.pump(attachment.transport, generation: tag) },
            Task { await self.watch(attachment.transport, generation: tag) },
        ]
        if let sentSize, sentSize != size { await resize(cols: size.cols, rows: size.rows) }
    }

    /// Detaches the current attachment. Its output still queued for `read()` is dropped.
    private func retire() {
        generation += 1
        isLive = false
        sentSize = nil
        liveSince = nil
        for task in generationTasks { task.cancel() }
        generationTasks = []
        output.removeAll()
        outputBytes = 0
        wakeRoomWaiters()
        if let old = current {
            current = nil
            Task { await Self.close(old) }
        }
    }

    private static func close(_ attachment: Attachment) async {
        await attachment.transport.close()
        await attachment.connection.close()
    }

    /// Failures the user has to act on; retrying them would only repeat the prompt or the block.
    private static func isPermanent(_ error: any Error) -> Bool {
        switch error {
        case SSHError.hostKeyChanged, SSHError.hostKeyRejected, SSHError.authFailed, SSHError.cancelled:
            true
        // Reaches here only when Mosh was chosen explicitly; Auto falls back to the next kind.
        case MoshBootstrapError.serverMissing:
            true
        // Thrown only when no kind may run for this host: an explicit kind that is
        // unavailable, or Auto with every kind skipped.
        case is ConnectorError:
            true
        default:
            false
        }
    }

    // MARK: - Transport events

    private func watch(_ transport: any ByteTransport, generation tag: Int) async {
        var roaming = false
        for await state in transport.state {
            guard tag == generation else { return }
            switch state {
            case .roaming:
                roaming = true
                stateSink.yield(.roaming)
            case .connected where roaming:
                roaming = false
                if let current { stateSink.yield(.live(current.kind, notes: current.notes)) }
            case .closed(let reason) where reason != .requested:
                reconnect()
                return
            default:
                break
            }
        }
    }

    private func pathChanged() {
        if current?.kind == .ssh { reconnect() }
    }

    private func flushResize() async {
        guard isLive, let transport = current?.transport else { return }
        let latest = size
        if let sentSize, sentSize == latest { return }
        sentSize = latest
        resizesSent += 1
        try? await transport.resize(cols: latest.cols, rows: latest.rows)
    }

    // MARK: - Output queue

    private func pump(_ transport: any ByteTransport, generation tag: Int) async {
        for await chunk in transport.incoming {
            guard await deliver(chunk, generation: tag) else { return }
        }
    }

    private func deliver(_ chunk: Data, generation tag: Int) async -> Bool {
        guard tag == generation, !isStopped else { return false }
        if let reader {
            self.reader = nil
            reader.resume(returning: chunk)
            return true
        }
        output.append(chunk)
        outputBytes += chunk.count
        while outputBytes >= Self.outputLimit, tag == generation, !isStopped {
            await withCheckedContinuation { roomWaiters.append($0) }
        }
        return tag == generation && !isStopped
    }

    private func takeOutput() -> Data? {
        guard !output.isEmpty else { return nil }
        let chunk = output.removeFirst()
        outputBytes -= chunk.count
        if outputBytes < Self.outputLimit { wakeRoomWaiters() }
        return chunk
    }

    private func wakeRoomWaiters() {
        let waiters = roomWaiters
        roomWaiters = []
        for waiter in waiters { waiter.resume() }
    }

    private func cancelRead() {
        reader?.resume(returning: nil)
        reader = nil
    }
}
