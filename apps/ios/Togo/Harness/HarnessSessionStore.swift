import Foundation
import Observation
import SwiftUI
import TogoKit
import UIKit

/// One session's native harness state: what the adapter can do, the conversation, mission
/// control and the composer's queue. One store per open tab; the chat, the pill, mission
/// control and the primitives read it.
@MainActor @Observable final class HarnessSessionStore {
    /// Where the conversation stands.
    enum Phase: Equatable {
        /// Asking the helper what serves the session.
        case describing
        /// Opening the conversation.
        case opening
        case live
        /// The connection dropped; trying again.
        case reconnecting
        /// No adapter, no chat, or its source failed: the terminal only.
        case terminalOnly
    }

    let subject: HarnessSubject
    @ObservationIgnored let services: any HarnessServices

    /// nil until described; a descriptor with a nil adapter means terminal only.
    private(set) var descriptor: HarnessDescriptor?
    private(set) var phase: Phase = .describing
    /// Not observed as a whole: a streamed note changes it several times a second, and views
    /// read the narrower `timeline`, `mission` and `working`, which say when they change.
    @ObservationIgnored private(set) var feed = HarnessFeedState() {
        didSet { mirrorFeed() }
    }
    /// The main conversation folded into rows, rebuilt on every change.
    private(set) var rows: [ChatRow] = [] {
        didSet {
            let next = ChatGaps.gaps(rows)
            if next != gaps { gaps = next }
        }
    }
    /// The pauses before rows, worked out once per change rather than in every chat body.
    private(set) var gaps: [String: TimeInterval] = [:]
    /// The live working row's state: shown from a send until the turn ends, nil while idle.
    /// Kept up to date by `refreshWorking`, so the chat and its announcements read a value.
    private(set) var liveWork: WorkingState?
    /// The harness's own queued prompts, for the composer's count; set only when they change,
    /// so streamed text does not redraw the composer.
    private(set) var queuedItems: [HarnessItem] = []
    @ObservationIgnored private var mirroredMission: HarnessMission?
    @ObservationIgnored private var mirroredWorking = false
    /// Prompts the phone holds: waiting for the turn to end, on their way, or failed.
    private(set) var queue = PromptQueue()
    private(set) var loadingOlder = false
    /// The layer the user wants for this tab. Setting it is the user's choice, which the
    /// adapter's default never overrides afterwards.
    var layer: SessionLayer {
        get { chosenLayer }
        set {
            chosenLayer = newValue
            layerChosen = true
        }
    }
    private var chosenLayer: SessionLayer = .terminal
    /// The session is expected to have a chat (a cached descriptor, a mission on its roster row
    /// or a known adapter agent), so until describe answers it shows plain ink, not the terminal.
    var expectsChat: Bool { expectsChatFixed || expectsChatLive() }
    @ObservationIgnored private let expectsChatFixed: Bool
    /// Asked on every read: a session the roster lists after its tab opened (a just-created
    /// one) starts expecting its chat then, rather than staying on the terminal.
    @ObservationIgnored private let expectsChatLive: () -> Bool
    /// Describe failed at least once since the last success: the terminal shows meanwhile.
    private(set) var describeFailed = false
    /// Describe has not answered within `pendingTimeout`: the loader gives way to the terminal,
    /// a clear state, and the chat still takes over if it comes.
    private(set) var waitExpired = false
    /// The first `.opened` has been applied: what shows from here on arrives, what showed
    /// before it was simply there (first load is a cut).
    private(set) var hasLoaded = false
    /// The composer's height under this tab's chat, measured where the composer is drawn.
    var composerClearance: CGFloat = 0
    /// The composer's / or @ menu is up: a tap on the chat puts it away.
    var composerMenuOpen = false
    /// A scroll the chat should make: to a needs card ("Go to request", the capsule).
    var scrollRequest: ChatScrollRequest?
    /// Bumped on every prompt this phone sends, so the chat scrolls to it.
    private(set) var sends = 0
    /// A subagent whose own conversation is shown (its agent item id), or nil for the main one.
    var focusedAgent: String?
    /// Set by "Show in terminal"; the terminal layer consumes and clears it.
    var pendingJump: TerminalJump?
    /// The pill asked for mission control.
    var showsMission = false
    /// Why the native layers are not showing, when they fell back to the terminal.
    private(set) var fallback: String?
    /// When this phone last sent a prompt the helper has not reported work for yet.
    private(set) var awaitingSince: Date?
    /// When the phone first saw the current stretch of work, for adapters without turn items.
    private(set) var localWorkSince: Date?
    /// The last mode the session reported, kept while later updates leave it out.
    private(set) var rememberedMode: String?
    /// When this phone saw each request answered, for the turn's stopwatch (`TurnClock`).
    private(set) var resolvedAt: [String: Date] = [:]
    @ObservationIgnored private var ledger = ResolutionLedger()
    /// Lines this phone adds to the chat after a change it made ("Effort high").
    var notes: [SessionNote] = []
    /// The Undo the composer offers after a restart, until the next change or send.
    var undo: SessionUndo?
    /// A line the composer takes into its field, focused ("With instructions…").
    var draftRequest: DraftRequest?
    /// The harness's commands as the palette last read them: the state chip and "With
    /// instructions…" find their choices and lines here.
    var commandList: [HarnessCompletion] = []
    /// Changes held for the end of the turn ("After this reply").
    @ObservationIgnored var deferred: [DeferredAction] = []

    @ObservationIgnored private var layerChosen = false
    /// The helper said "working" since the last send.
    @ObservationIgnored private var sawWorking = false
    @ObservationIgnored private var graceTask: Task<Void, Never>?
    @ObservationIgnored private var runTask: Task<Void, Never>?
    /// Delivers held prompts after a change; cancelled with the feed.
    @ObservationIgnored private var pumpTask: Task<Void, Never>?
    @ObservationIgnored private var pumpToken: UUID?
    @ObservationIgnored private var needsDescribe = true
    @ObservationIgnored private let retryFloor: Duration
    @ObservationIgnored private let retryCeiling: Duration
    @ObservationIgnored private let pendingTimeout: Duration
    @ObservationIgnored private var waitTask: Task<Void, Never>?

    init(
        subject: HarnessSubject, services: any HarnessServices,
        retry: (floor: Duration, ceiling: Duration) = (.milliseconds(500), .seconds(10)),
        expectsChat: Bool = false, expectsChatLive: @escaping () -> Bool = { false },
        cached: HarnessDescriptor? = nil, pendingTimeout: Duration = .seconds(8)
    ) {
        self.subject = subject
        self.services = services
        retryFloor = retry.floor
        retryCeiling = retry.ceiling
        self.pendingTimeout = pendingTimeout
        expectsChatFixed = expectsChat || cached?.adapter != nil
        self.expectsChatLive = expectsChatLive
        // A descriptor this tab already had is adopted at once, so a reopened tab shows its chat
        // on the first frame; describe still runs in the background and replaces it.
        if let cached, cached.adapter != nil, subject.available { adopt(cached) }
    }

    /// The conversation as the feed holds it.
    var timeline: HarnessTimeline {
        access(keyPath: \.timeline)
        return feed.timeline
    }

    var mission: MissionState {
        access(keyPath: \.mission)
        return feed.mission
    }

    /// A turn is running, so a prompt waits and Interrupt shows.
    var working: Bool {
        access(keyPath: \.working)
        return mirroredWorking
    }

    /// Tells readers of the narrow views what changed: the timeline always, mission control and
    /// working only when they did.
    private func mirrorFeed() {
        withMutation(keyPath: \.timeline) {}
        if feed.mission.mission != mirroredMission {
            mirroredMission = feed.mission.mission
            withMutation(keyPath: \.mission) {}
        }
        let nowWorking = feed.mission.summary.working
        if nowWorking != mirroredWorking {
            withMutation(keyPath: \.working) { mirroredWorking = nowWorking }
        }
    }

    private func updateLiveWork(_ conversation: [HarnessItem], now: Date = .now) {
        let next = WorkingLogic.state(
            items: conversation, working: working, awaitingSince: awaitingSince,
            localSince: localWorkSince, now: now, needsYou: feed.mission.summary.needsYou,
            resolvedAt: resolvedAt
        )
        if next != liveWork { liveWork = next }
    }

    /// A prompt just went: the working row shows at once, before the helper reports the turn.
    func markSent(at now: Date = .now) {
        sends += 1
        awaitingSince = now
        sawWorking = working
        if localWorkSince == nil { localWorkSince = now }
        updateLiveWork(feed.timeline.conversation(), now: now)
        graceTask?.cancel()
        graceTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(WorkingLogic.sendGrace + 0.5))
            guard !Task.isCancelled else { return }
            self?.refreshWorking()
        }
    }

    /// Clears the sent prompt's hold once the turn it started is over, and tracks since when
    /// the agent works.
    func refreshWorking(now: Date = .now) {
        refreshWorking(feed.timeline.conversation(), now: now)
    }

    private func refreshWorking(_ conversation: [HarnessItem], now: Date = .now) {
        if working { sawWorking = true }
        if let since = awaitingSince, WorkingLogic.awaitingEnded(
            awaitingSince: since, wasWorking: sawWorking, working: working,
            items: conversation, now: now
        ) {
            awaitingSince = nil
            graceTask?.cancel()
        }
        if working || awaitingSince != nil {
            if localWorkSince == nil { localWorkSince = now }
        } else {
            localWorkSince = nil
        }
        updateLiveWork(conversation, now: now)
        if !working, !deferred.isEmpty { runDeferred() }
    }

    // MARK: Feed

    /// Runs `start` until `stop`. Calling it again while it runs does nothing. The task holds
    /// the store only for one step at a time, so a store nobody keeps goes, and its loop with it.
    func launch() {
        guard runTask == nil else { return }
        runTask = Task { [weak self] in await HarnessSessionStore.run(weak: self) }
        startWaitClock()
    }

    /// Bounds the loader: a session still undescribed after `pendingTimeout` shows its terminal.
    func startWaitClock() {
        guard waitTask == nil, awaitsChat else { return }
        let timeout = pendingTimeout
        waitTask = Task { [weak self] in
            try? await Task.sleep(for: timeout)
            guard !Task.isCancelled, let self, self.descriptor == nil else { return }
            self.waitExpired = true
        }
    }

    /// Describes the session, opens its feed and applies it until `stop` or cancellation,
    /// reopening with the cursor after a drop or a `.reopen`.
    func start() async {
        await Self.run(weak: self)
    }

    /// One store's loop. Each pass takes the store afresh and ends once it is gone, so the loop
    /// never keeps a closed tab's store, and its connections, alive.
    private static func run(weak store: HarnessSessionStore?) async {
        weak let weakStore = store
        var delay = weakStore?.retryFloor ?? .zero
        var reopenDelay = delay
        while !Task.isCancelled {
            guard let store = weakStore, await store.step(delay: &delay, reopenDelay: &reopenDelay) else { return }
        }
    }

    /// One pass of the loop: describe if needed, follow the feed, wait before the next pass.
    /// False when the loop should end.
    private func step(delay: inout Duration, reopenDelay: inout Duration) async -> Bool {
        guard subject.available else {
            endNative(nil)
            return false
        }
        if needsDescribe {
            phase = .describing
            do {
                adopt(try await services.describe(subject))
                needsDescribe = false
                describeFailed = false
            } catch {
                // The ink waits no longer: the terminal shows (a cut) while describe retries.
                describeFailed = true
                return await pause(&delay)
            }
        }
        guard hasAdapter, has(HarnessCapability.chat) else {
            endNative(nil)
            return false
        }
        let began = ContinuousClock.now
        switch await follow() {
        case .reopen:
            // A gap storm must not open a channel per gap: wait a little, longer each time,
            // unless the feed had been live a good while.
            delay = retryFloor
            if ContinuousClock.now - began > retryCeiling { reopenDelay = retryFloor }
            return await pause(&reopenDelay)
        case .dropped(let opened):
            if opened { delay = retryFloor }
            phase = .reconnecting
            return await pause(&delay)
        case .refused:
            endNative(ChatCopy.fallback)
            return false
        }
    }

    func stop() {
        runTask?.cancel()
        runTask = nil
        pumpTask?.cancel()
        pumpTask = nil
        graceTask?.cancel()
        waitTask?.cancel()
        waitTask = nil
    }

    private enum FollowEnd {
        case reopen
        case dropped(opened: Bool)
        case refused
    }

    private func follow() async -> FollowEnd {
        phase = .opening
        var opened = false
        do {
            for try await element in services.feed(subject, after: feed.openAfter) {
                switch element {
                case .opened(let result):
                    opened = true
                    phase = .live
                    let outcome = feed.load(result)
                    changed(arrivals: false)
                    hasLoaded = true
                    if outcome == .reopen { return .reopen }
                case .note(let note):
                    if apply(note) == .reopen {
                        if note.kind == .reset, note.reason == "adapter" { needsDescribe = true }
                        return .reopen
                    }
                }
            }
            return .dropped(opened: opened)
        } catch HelperError.refused {
            return .refused
        } catch HelperError.unsupported {
            return .refused
        } catch {
            return .dropped(opened: opened)
        }
    }

    /// Waits before trying again, doubling the wait up to the ceiling. False once cancelled.
    private func pause(_ delay: inout Duration) async -> Bool {
        do {
            try await Task.sleep(for: delay)
        } catch {
            return false
        }
        delay = min(delay * 2, retryCeiling)
        return !Task.isCancelled
    }

    /// The session's current mode id for the mode chip; empty only while truly unknown.
    var currentMode: String {
        ModeMemory.current(status: ModeMemory.status(mission.mission), remembered: rememberedMode, described: descriptor?.mode)
    }

    private func rememberMode() {
        let mode = currentMode
        if !mode.isEmpty, mode != rememberedMode { rememberedMode = mode }
    }

    private func adopt(_ descriptor: HarnessDescriptor) {
        self.descriptor = descriptor
        if !descriptor.mode.isEmpty { rememberedMode = descriptor.mode }
        // Chat is the default where it can show, until the user picks a layer.
        if descriptor.adapter != nil, descriptor.has(HarnessCapability.chat), !layerChosen {
            chosenLayer = .chat
        }
    }

    private func endNative(_ reason: String?) {
        if descriptor == nil { descriptor = HarnessDescriptor() }
        fallback = reason
        phase = .terminalOnly
    }

    /// Applies a feed note. A note that brings items the chat has not seen applies in the
    /// arrival transaction, so new rows, a subagent's thread and the live line move together;
    /// updates to items already shown (streamed text) land without animation.
    private func apply(_ note: HarnessNotification) -> HarnessApply {
        let arrives = feed.isLoaded && (note.items ?? []).contains { feed.timeline.item($0.id) == nil }
        guard arrives else {
            let outcome = feed.apply(note)
            if outcome != .reopen { changed() }
            return outcome
        }
        var outcome = HarnessApply.applied
        // motion: preventing a jump — whatever a note brings lands on one `resize` transaction, so its neighbours make room together.
        withAnimation(Motion.animation(.resize, reduce: Self.reduceMotion)) {
            outcome = feed.apply(note)
            if outcome != .reopen { changed(arrivals: false) }
        }
        return outcome
    }

    /// Rebuilds the rows after the feed changed. Rows that arrive while the chat is open reveal
    /// in one `resize` transaction keyed on the row ids only (spec §2.5): streamed text and
    /// other changes inside a row land without animation, and a fresh open never animates.
    private func changed(arrivals: Bool = true) {
        if ledger.observe(feed.timeline.items, now: .now) { resolvedAt = ledger.resolvedAt }
        // The main conversation once per change; the rows, the live line and the queue count
        // all read it.
        let conversation = feed.timeline.conversation()
        setRows(SubagentThread.mainRows(feed.timeline, conversation: conversation, resolvedAt: resolvedAt), arrivals: arrivals)
        let queued = conversation.filter { item in
            if case let .user(user) = item.body { user.queued } else { false }
        }
        if queued != queuedItems { queuedItems = queued }
        rememberMode()
        refreshWorking(conversation)
        pump()
    }

    // MARK: Older items

    func loadOlder() async {
        guard has(HarnessCapability.history), feed.timeline.hasOlder, !loadingOlder,
              let before = feed.timeline.oldestSeq else { return }
        loadingOlder = true
        defer { loadingOlder = false }
        guard let page = try? await services.history(subject, before: before) else { return }
        feed.prepend(page)
        // Rows from "Earlier" land without animation; the scroll keeps its place.
        setRows(SubagentThread.mainRows(feed.timeline, resolvedAt: resolvedAt), arrivals: false)
    }

    /// Rows and queued prompts arrive and leave on `resize`; under Reduce Motion a fade.
    private static var reduceMotion: Bool { UIAccessibility.isReduceMotionEnabled }

    func setRows(_ fed: [ChatRow], arrivals: Bool) {
        let next = SessionNote.merge(notes, into: fed)
        guard arrivals, !rows.isEmpty, rows.map(\.id) != next.map(\.id) else {
            rows = next
            return
        }
        // motion: preventing a jump — new rows make room on the same transaction as their neighbours.
        withAnimation(Motion.animation(.resize, reduce: Self.reduceMotion)) { rows = next }
    }

    /// The rows again from the feed, with this phone's notes in place: after a note is added.
    func refreshRows() {
        setRows(SubagentThread.mainRows(feed.timeline, resolvedAt: resolvedAt), arrivals: true)
    }

    /// Changes the phone's own queue in the arrival transaction, so a held prompt's row
    /// reveals and leaves like any other row.
    private func changeQueue(_ change: (inout PromptQueue) -> Void) {
        // motion: preventing a jump — a held prompt's row comes and goes while the rows around it make room.
        withAnimation(Motion.animation(.resize, reduce: Self.reduceMotion)) { change(&queue) }
    }

    // MARK: Composer

    struct QueueFull: Error {}

    /// Sends a prompt. `.queue` (the default) holds it on the phone while the agent works on a
    /// harness that cannot queue, and sends it when the turn ends; `.now` goes at once and
    /// throws when the helper refuses it.
    func send(_ text: String, mode: HarnessSendParams.Mode = .queue, images: [String] = []) async throws {
        if mode == .now {
            let item = try await services.send(subject, text: text, mode: .now, images: images)
            feed.insertSent(item)
            markSent()
            changed()
            return
        }
        var added: UUID?
        changeQueue { added = $0.add(text, images: images) }
        guard added != nil else {
            if queue.isFull { throw QueueFull() }
            return
        }
        await deliverNext()
    }

    /// Stops the whole turn; the helper presses until the session is idle and says whether it got there.
    @discardableResult
    func interrupt() async throws -> HarnessActionResult {
        try await services.interrupt(subject)
    }

    func cancelQueued(_ id: UUID) {
        changeQueue { _ = $0.cancel(id) }
    }

    /// Takes back a prompt the harness holds in its own queue: `stop` aimed at the queued user
    /// item, which the engine drops from the send queue. False when the helper refused.
    @discardableResult
    func cancelQueued(item: HarnessItem) async -> Bool {
        guard let result = try? await run(action: "stop", target: item.id) else { return false }
        return result.result != .failed
    }

    func retryQueued(_ id: UUID) {
        changeQueue { $0.retry(id) }
        pump()
    }

    /// True when a prompt sent now reaches the session: the agent is idle, or the harness holds
    /// prompts itself while it works.
    var canSendNow: Bool {
        feed.isLoaded && (!working || has(HarnessCapability.queue))
    }

    /// Sends what the queue holds once the session can take it. Only while the feed runs: a
    /// stopped store sends nothing until it is launched again.
    private func pump() {
        guard runTask != nil, pumpTask == nil, !queue.isEmpty else { return }
        let token = UUID()
        pumpToken = token
        pumpTask = Task { [weak self] in
            await self?.deliverNext()
            if self?.pumpToken == token { self?.pumpTask = nil }
        }
    }

    private func deliverNext() async {
        guard let prompt = queue.next(canSend: canSendNow) else { return }
        do {
            let item = try await services.send(subject, text: prompt.text, mode: .queue, images: prompt.images)
            // motion: continuity — one transaction: the held row leaves as the delivered prompt arrives in its place.
            withAnimation(Motion.animation(.resize, reduce: Self.reduceMotion)) {
                queue.sent(prompt.id)
                feed.insertSent(item)
                rows = SessionNote.merge(notes, into: SubagentThread.mainRows(feed.timeline, resolvedAt: resolvedAt))
            }
            markSent()
        } catch HelperError.conflict(let reason) where reason == "busy" {
            // The turn is still going; the next change of the mission tries again.
            queue.requeue(prompt.id)
            return
        } catch is CancellationError {
            // Stopped on the way: it waits for the next launch rather than reading as failed.
            queue.requeue(prompt.id)
            return
        } catch {
            changeQueue { $0.failed(prompt.id, message: ServiceErrors.message(error)) }
            return
        }
        await deliverNext()
    }
}
