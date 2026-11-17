import Foundation
import Observation
import SwiftUI
import TogoKit
import UIKit

/// One store per open tab: the conversation, native interfaces, mission control
/// and composer queue share this session's state.
@MainActor @Observable final class HarnessSessionStore {
    let subject: HarnessSubject
    @ObservationIgnored let services: any HarnessServices
    /// nil until described; a descriptor with a nil adapter means terminal only.
    private(set) var descriptor: HarnessDescriptor?
    private(set) var phase: Phase = .describing {
        didSet {
            if phase == .live { liveWork?.pausedAt = nil }
            else if liveWork?.pausedAt == nil { liveWork?.pausedAt = .now }
        }
    }
    /// Not observed as a whole: a streamed note changes it several times a second, and views
    /// read the narrower `timeline`, `mission` and `working`, which say when they change.
    @ObservationIgnored var feed = HarnessFeedState() {
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
    var queue = PromptQueue()
    var loadingOlder = false
    /// The layer the user wants for this tab. Setting it is the user's choice, which the
    /// adapter's default never overrides afterwards.
    var layer: SessionLayer {
        get { chosenLayer }
        set {
            if newValue != .chat { nativeInteractions.terminalWatch.cancel() }
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
    /// Where the reader is in this session's chat. Kept with the session rather than the view,
    /// so a fold, unfold or window resize that rebuilds the workspace returns to the same place.
    @ObservationIgnored let chatScroll = ChatScrollModel()
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
    let nativeInteractions = NativeChatState()
    @ObservationIgnored let surfaceDrafts = SurfaceDrafts()
    /// Why the native layers are not showing, when they fell back to the terminal.
    private(set) var fallback: String?
    /// When this phone last sent a prompt the helper has not reported work for yet.
    private(set) var awaitingSince: Date?
    /// When the phone first saw the current stretch of work, for adapters without turn items.
    private(set) var localWorkSince: Date?
    /// The last mode the session reported, kept while later updates leave it out.
    private(set) var rememberedMode: String? {
        didSet { if rememberedMode != oldValue { modeRevision &+= 1 } }
    }
    /// Invalidates optimistic choices even when the view missed an intermediate mode.
    private(set) var modeRevision: UInt64 = 0
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
    var commandRevision = UUID()
    /// Changes held for the end of the turn ("After this reply").
    @ObservationIgnored var deferred: [DeferredAction] = []
    @ObservationIgnored var deferredTask: Task<Void, Never>?

    @ObservationIgnored private(set) var conversation = UUID()
    @ObservationIgnored private var changingConversation = false

    @ObservationIgnored private var layerChosen = false
    /// The helper said "working" since the last send.
    @ObservationIgnored private var sawWorking = false
    @ObservationIgnored private var graceTask: Task<Void, Never>?
    var isRunning: Bool { runTask != nil }
    @ObservationIgnored private var runID: UUID?
    @ObservationIgnored private var runTask: Task<Void, Never>?
    /// Delivers held prompts after a change; cancelled with the feed.
    @ObservationIgnored var pumpTask: Task<Void, Never>?
    @ObservationIgnored var pumpToken: UUID?
    @ObservationIgnored private var needsDescribe = true
    @ObservationIgnored let retryFloor: Duration
    @ObservationIgnored let retryCeiling: Duration
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
        return MissionNeedsPresentation.state(mirroredMission)
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
        let presented = MissionNeedsPresentation.snapshot(feed.mission.mission, in: feed.timeline)
        if presented != mirroredMission {
            mirroredMission = presented
            withMutation(keyPath: \.mission) {}
        }
        let nowWorking = feed.mission.summary.working
        if nowWorking != mirroredWorking {
            withMutation(keyPath: \.working) { mirroredWorking = nowWorking }
        }
    }
    private func updateLiveWork(_ conversation: [HarnessItem], now: Date = .now) {
        var next = WorkingLogic.state(
            items: conversation, working: working, awaitingSince: awaitingSince,
            localSince: localWorkSince, now: now, needsYou: feed.mission.summary.needsYou,
            resolvedAt: resolvedAt
        )
        if phase != .live { next?.pausedAt = liveWork?.pausedAt ?? now }
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

    /// Retires work owned by the old conversation before another await can run.
    private func invalidateConversation() {
        guard !changingConversation else { return }
        changingConversation = true
        retireNativeInteractions(pumpQueue: false)
        nativeInteractions.requests.reset()
        commandList = []
        commandRevision = UUID()
        conversation = UUID()
        phase = .opening
        pumpTask?.cancel()
        pumpTask = nil
        pumpToken = nil
        queue.conversationChanged()
        let changeWasInFlight = deferredTask != nil
        let canceledChanges = changeWasInFlight || !deferred.isEmpty
        deferredTask?.cancel()
        deferredTask = nil
        deferred = []
        notes = []
        if canceledChanges {
            notes.append(SessionNote(id: "note-\(UUID().uuidString)", text: changeWasInFlight ? ChatCopy.changeUnconfirmed : ChatCopy.changesCanceled,
                                     code: "info", after: nil, at: .now))
        }
        undo = nil
        draftRequest = nil
        focusedAgent = nil
        pendingJump = nil
        scrollRequest = nil
        chatScroll.prepareReload(ids: Set(rows.map(\.id)))
        ledger = ResolutionLedger()
        resolvedAt = [:]
        awaitingSince = nil
        localWorkSince = nil
        sawWorking = false
        liveWork = nil
        loadingOlder = false
        rememberedMode = nil
        modeRevision &+= 1
        graceTask?.cancel()
        graceTask = nil
    }

    // MARK: Feed

    /// Runs `start` until `stop`. Calling it again while it runs does nothing. The task holds
    /// the store only for one step at a time, so a store nobody keeps goes, and its loop with it.
    func launch() {
        guard runTask == nil else { return }
        let id = UUID()
        runID = id
        needsDescribe = true
        runTask = Task { [weak self] in
            await HarnessSessionStore.run(weak: self)
            guard self?.runID == id else { return }
            self?.runTask = nil
            self?.runID = nil
        }
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

    /// One pass of the loop: describe if needed, follow the feed, wait before the next pass.
    /// False when the loop should end.
    func step(delay: inout Duration, reopenDelay: inout Duration) async -> Bool {
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
            nativeInteractions.terminalWatch.cancel()
            phase = .reconnecting
            return await pause(&delay)
        case .refused:
            endNative(ChatCopy.fallback)
            return false
        }
    }

    func stop() {
        if phase == .live { phase = .opening }
        retireNativeInteractions(pumpQueue: false)
        runTask?.cancel()
        runTask = nil
        runID = nil
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
            let requestedAfter = feed.openAfter
            for try await element in services.feed(subject, after: requestedAfter) {
                switch element {
                case .opened(let incoming):
                    if feed.isLoaded, feed.timeline.epoch != incoming.epoch {
                        invalidateConversation()
                        if requestedAfter > 0 {
                            feed.reopenFromStart()
                            return .reopen
                        }
                    }
                    let result = try await completeOpen(incoming)
                    try Task.checkCancellation()
                    opened = true
                    changingConversation = false
                    let outcome = feed.load(result)
                    phase = .live
                    changed(arrivals: false)
                    chatScroll.finishReload(ids: Set(rows.map(\.id)))
                    hasLoaded = true
                    if outcome == .reopen { return .reopen }
                case .note(let note):
                    if apply(note) == .reopen {
                        phase = .opening
                        if note.kind == .reset, note.reason == "adapter" { needsDescribe = true }
                        return .reopen
                    }
                }
            }
            return .dropped(opened: opened)
        } catch let error as HelperError where error.isFallback {
            return .refused
        } catch HelperError.unsupported {
            return .refused
        } catch {
            return .dropped(opened: opened)
        }
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
        retireNativeInteractions(pumpQueue: false)
        if descriptor == nil { descriptor = HarnessDescriptor() }
        fallback = reason
        phase = .terminalOnly
    }

    /// Applies a feed note. A note that brings items the chat has not seen applies in the
    /// arrival transaction, so new rows, a subagent's thread and the live line move together;
    /// updates to items already shown (streamed text) land without animation.
    private func apply(_ note: HarnessNotification) -> HarnessApply {
        if let outcome = applyNativeNotification(note) { return outcome }
        let replacement = note.kind == .reset && !["history", "overflow"].contains(note.reason ?? "")
        if feed.isLoaded, replacement || note.epoch != feed.timeline.epoch { invalidateConversation() }
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
    func changed(arrivals: Bool = true) {
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
        updateNativeQuestion()
        pump()
    }

    /// Rows and queued prompts arrive and leave on `resize`; under Reduce Motion a fade.
    static var reduceMotion: Bool { UIAccessibility.isReduceMotionEnabled }

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
}
