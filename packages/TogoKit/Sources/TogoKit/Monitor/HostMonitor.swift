import Foundation

/// Keeps one machine's sessions and approvals current over a live `SessionSource`.
///
/// Each connection starts from a snapshot, then folds events in seq order. A seq gap
/// means events were lost, so the state is replaced by a new snapshot rather than
/// patched. A generation number tags the current connection; events, poll ticks and
/// failures from a replaced one are ignored, so a reconnect never leaves ghost rows.
///
/// Snapshot calls never overlap. Events read while one is in flight are held and
/// replayed on top of its result, because the snapshot may or may not contain them.
public actor HostMonitor {
    typealias Probe = @Sendable (HostRecord, any LinkClock, @escaping @Sendable () -> Void) async throws -> Void

    static let backoff = TerminalLink.backoff
    static let failuresBeforeOffline = 3
    static let offlineRetry: Duration = .seconds(8)
    static let pollInterval: Duration = .seconds(5)
    /// Long enough to answer a notification from the lock screen without reconnecting.
    static let backgroundGrace: Duration = .seconds(30)

    /// Newest state only; a slow reader skips intermediate ones. Single consumer: LiveServices.
    public nonisolated let snapshots: AsyncStream<HostSnapshot>
    private let sink: AsyncStream<HostSnapshot>.Continuation
    private let record: HostRecord
    private let factory: SourceFactory
    private let path: any NetworkPathSource
    private let clock: any LinkClock
    private let probe: Probe
    private let now: @Sendable () -> Date

    private var state: HostSnapshot
    private var published: HostSnapshot?
    private var source: (any SessionSource)?
    private var generation = 0
    private var lastSeq = 0
    private var hasEvents = false
    private var isStarted = false
    private var isStopped = false
    private var isForeground = true
    /// Retrying cannot help until the user acts; only `refresh()` clears it.
    private var isHeld = false
    private var syncingGeneration: Int?
    private var syncAgain = false
    private var held: [EventEnvelope] = []

    private var attempt: Task<Void, Never>?
    /// Tags the current run of attempts. A cancelled run may still be scheduled after its
    /// replacement started, so it must not touch `attempt` once this has moved on.
    private var attemptGeneration = 0
    /// Counts trips to the background, so an attempt can tell it was interrupted by one.
    private var backgroundings = 0
    private var feed: Task<Void, Never>?
    private var backgroundClose: Task<Void, Never>?
    private var pathTask: Task<Void, Never>?

    public init(record: HostRecord, factory: @escaping SourceFactory, path: any NetworkPathSource, clock: any LinkClock = SystemClock()) {
        self.init(record: record, factory: factory, path: path, clock: clock, probe: Self.wakeProbe, now: { Date() })
    }

    init(
        record: HostRecord, factory: @escaping SourceFactory, path: any NetworkPathSource, clock: any LinkClock,
        probe: @escaping Probe, now: @escaping @Sendable () -> Date
    ) {
        self.record = record
        self.factory = factory
        self.path = path
        self.clock = clock
        self.probe = probe
        self.now = now
        state = HostSnapshot(hostID: record.id)
        (snapshots, sink) = AsyncStream<HostSnapshot>.makeStream(bufferingPolicy: .bufferingNewest(1))
    }

    private static let wakeProbe: Probe = { record, clock, onWaking in
        let target = record.profile.jumps.first ?? record.profile.endpoint
        try await WakeProbe.reach(
            host: target.host, port: target.port, clock: clock, onWaking: { _ in onWaking() },
            attempt: { await WakeProbe.tcpConnect(host: target.host, port: target.port, timeout: .seconds(2)) }
        )
    }

    // MARK: - Public API

    public func start() async {
        guard !isStarted, !isStopped else { return }
        isStarted = true
        let changes = path.changes
        pathTask = Task { [weak self] in
            for await _ in changes { await self?.pathChanged() }
        }
        publish()
        beginAttempts()
    }

    public func stop() async {
        guard !isStopped else { return }
        isStopped = true
        cancelAttempts()
        backgroundClose?.cancel()
        pathTask?.cancel()
        retire()
        sink.finish()
    }

    /// Subscribes again with the new foreground flag. In the background the connection
    /// closes after `backgroundGrace` and nothing reconnects; back in the foreground it does.
    /// May be called before `start()`.
    public func setForeground(_ on: Bool) async {
        isForeground = on
        if !on { backgroundings += 1 }
        guard isStarted, !isStopped else { return }
        backgroundClose?.cancel()
        backgroundClose = nil
        if !on {
            backgroundClose = Task { [clock] in
                do { try await clock.sleep(for: Self.backgroundGrace) } catch { return }
                self.closeForBackground()
            }
        }
        if let source {
            let tag = generation
            await source.setForeground(on)
            await resync(tag)
        } else if on, attempt == nil, !isHeld {
            beginAttempts()
        }
    }

    /// Takes a new snapshot, or after a sign-in or install, tries to connect again.
    public func refresh() async {
        guard isStarted, !isStopped else { return }
        if source != nil {
            await resync(generation)
            return
        }
        isHeld = false
        beginAttempts()
    }

    public func perform(_ op: SessionOp) async throws -> Session? {
        guard let source else {
            if isStarted, !isStopped, isForeground, !isHeld, attempt == nil { beginAttempts() }
            throw HelperError.closed
        }
        let tag = generation
        let result: Session?
        do {
            result = try await source.call(op)
        } catch let error as HelperError where error == .closed {
            connectionLost(tag)
            throw error
        }
        // With events the change comes back as an event; applying it here as well could
        // overwrite a newer event with this older result.
        if !hasEvents, tag == generation {
            applyLocally(op, result: result)
            publish()
        }
        return result
    }

    // MARK: - Connecting

    private func beginAttempts() {
        cancelAttempts()
        let run = attemptGeneration
        attempt = Task { await self.runAttempts(run) }
    }

    private func cancelAttempts() {
        attempt?.cancel()
        attempt = nil
        attemptGeneration += 1
    }

    private func isCurrent(_ run: Int) -> Bool {
        run == attemptGeneration && !isStopped && !Task.isCancelled
    }

    private func endAttempts(_ run: Int) {
        if run == attemptGeneration { attempt = nil }
    }

    /// Runs only in the foreground. In the background a key unlock can be refused by the
    /// system rather than the user, which must not read as needing sign-in.
    private func runAttempts(_ run: Int) async {
        var failures = 0
        while isCurrent(run), isForeground {
            let delay = failures >= Self.failuresBeforeOffline
                ? Self.offlineRetry : Self.backoff[min(failures, Self.backoff.count - 1)]
            do { try await clock.sleep(for: delay) } catch { return }
            guard isCurrent(run) else { return }
            guard isForeground else { break }
            if !isOffline { setReach(.connecting) }
            let backgroundingsAtStart = backgroundings
            do {
                let connected = try await connect()
                guard isCurrent(run) else {
                    await connected.source.close()
                    return
                }
                endAttempts(run)
                await adopt(connected)
                return
            } catch let failure {
                guard isCurrent(run) else { return }
                guard isForeground else { break }
                if let helper = failure.helper { state.helper = helper }
                // A trip to the background during the attempt can refuse a key unlock on
                // its own; that is an ordinary failure, not a reason to wait for sign-in.
                if backgroundingsAtStart == backgroundings,
                   let reach = heldReach(for: failure.error, helper: failure.helper) {
                    endAttempts(run)
                    isHeld = true
                    setReach(reach)
                    return
                }
                failures += 1
                if failures >= Self.failuresBeforeOffline {
                    setReach(.offline(failure.error.localizedDescription))
                }
            }
        }
        endAttempts(run)
    }

    private struct Connected {
        let source: any SessionSource
        let helper: HelperPresence
        let capabilities: Set<String>
        let snapshot: EventSnapshot
        /// The flag the snapshot subscribed with; the app may have moved on since.
        let foreground: Bool
    }

    /// Carries what this attempt's `open` reported, so a superseded attempt cannot
    /// colour the outcome of the current one.
    private struct AttemptFailure: Error {
        let error: any Error
        let helper: HelperPresence?
    }

    private func connect() async throws(AttemptFailure) -> Connected {
        do {
            try await probe(record, clock) { [weak self] in
                Task { await self?.markWaking() }
            }
        } catch {
            throw AttemptFailure(error: error, helper: nil)
        }
        let source: any SessionSource
        do {
            source = try await factory(record)
        } catch {
            throw AttemptFailure(error: error, helper: nil)
        }
        var helper: HelperPresence?
        do {
            let (presence, capabilities) = try await source.open()
            helper = presence
            let foreground = isForeground
            await source.setForeground(foreground)
            let snapshot = try await source.snapshot()
            return Connected(
                source: source, helper: presence, capabilities: capabilities, snapshot: snapshot, foreground: foreground
            )
        } catch {
            await source.close()
            throw AttemptFailure(error: error, helper: helper)
        }
    }

    private func adopt(_ connected: Connected) async {
        // Only one run of attempts adopts at a time; closing any source still here keeps
        // that true even if a bug lets two runs overlap.
        if source != nil { retire() }
        generation += 1
        let tag = generation
        source = connected.source
        hasEvents = connected.capabilities.contains(Capability.events)
        state.helper = connected.helper
        state.capabilities = connected.capabilities
        replace(with: connected.snapshot)
        publish()
        let events = connected.source.events
        feed = hasEvents
            ? Task { await self.consume(events, generation: tag) }
            : Task { await self.poll(generation: tag) }
        if connected.foreground != isForeground {
            await connected.source.setForeground(isForeground)
            await resync(tag)
        }
    }

    /// Detaches the current source and closes it. Its late events and errors are ignored.
    private func retire() {
        generation += 1
        feed?.cancel()
        feed = nil
        held = []
        syncAgain = false
        if let old = source {
            source = nil
            Task { await old.close() }
        }
    }

    private func connectionLost(_ tag: Int) {
        guard tag == generation, !isStopped else { return }
        reconnect()
    }

    private func pathChanged() {
        guard isStarted, !isStopped, !isHeld, source != nil || attempt != nil else { return }
        reconnect()
    }

    /// In the background the dead source is only dropped; `setForeground(true)` reconnects.
    private func reconnect() {
        retire()
        if isForeground {
            beginAttempts()
        } else {
            cancelAttempts()
        }
    }

    private func closeForBackground() {
        guard !isForeground, !isStopped else { return }
        cancelAttempts()
        retire()
    }

    /// Errors only the user can resolve. A helper that is missing or too old cannot
    /// give a snapshot until it is installed or updated, so retrying is pointless too.
    private func heldReach(for error: any Error, helper: HelperPresence?) -> HostSnapshot.Reach? {
        switch error {
        case SSHError.authFailed, SSHError.cancelled, SSHError.hostKeyRejected:
            return .authNeeded
        case SSHError.hostKeyChanged:
            return .failed(error.localizedDescription)
        case KeyError.missing:
            return .keyMissing
        default:
            break
        }
        switch helper {
        case .missing?, .outdated?: return .failed(error.localizedDescription)
        case .unknown?, .present?, nil: return nil
        }
    }

    private func markWaking() {
        guard attempt != nil, state.reach == .connecting else { return }
        setReach(.waking)
    }

    // MARK: - Events and snapshots

    private func consume(_ events: AsyncStream<EventEnvelope>, generation tag: Int) async {
        for await event in events {
            guard tag == generation else { return }
            await receive(event, generation: tag)
        }
        connectionLost(tag)
    }

    private func receive(_ event: EventEnvelope, generation tag: Int) async {
        guard tag == generation else { return }
        state.lastSeen = now()
        if syncingGeneration == tag {
            held.append(event)
            return
        }
        if ingest(event) {
            publish()
        } else {
            await resync(tag)
        }
    }

    /// False on a seq gap. An event at or below the last seq is already in the state.
    private func ingest(_ event: EventEnvelope) -> Bool {
        guard event.seq > lastSeq else { return true }
        guard event.seq == lastSeq + 1 else { return false }
        state.apply(event)
        lastSeq = event.seq
        return true
    }

    private func resync(_ tag: Int) async {
        guard tag == generation, let source else { return }
        if syncingGeneration == tag {
            syncAgain = true
            return
        }
        syncingGeneration = tag
        defer { if syncingGeneration == tag { syncingGeneration = nil } }
        repeat {
            syncAgain = false
            let snapshot: EventSnapshot
            do {
                snapshot = try await source.snapshot()
            } catch {
                connectionLost(tag)
                return
            }
            guard tag == generation else { return }
            replace(with: snapshot)
            let backlog = held
            held = []
            for event in backlog where !ingest(event) {
                syncAgain = true
                break
            }
            publish()
        } while syncAgain && tag == generation
    }

    private func poll(generation tag: Int) async {
        while tag == generation, !Task.isCancelled {
            do { try await clock.sleep(for: Self.pollInterval) } catch { return }
            if isForeground { await resync(tag) }
        }
    }

    private func replace(with snapshot: EventSnapshot) {
        state.sessions = snapshot.sessions
        state.approvals = snapshot.approvals
        state.lastSeen = now()
        state.reach = .online
        lastSeq = snapshot.seq
    }

    private func applyLocally(_ op: SessionOp, result: Session?) {
        switch op {
        case .kill(let id): state.sessions.removeAll { $0.id == id }
        case .answer(let requestId, _): state.removeApproval(requestId)
        case .create, .rename, .setMute: if let result { state.upsert(result) }
        }
    }

    // MARK: - Publishing

    private var isOffline: Bool {
        if case .offline = state.reach { true } else { false }
    }

    private func setReach(_ reach: HostSnapshot.Reach) {
        state.reach = reach
        publish()
    }

    private func publish() {
        guard state != published else { return }
        published = state
        sink.yield(state)
    }
}
