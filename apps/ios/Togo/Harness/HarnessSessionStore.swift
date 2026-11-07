import Foundation
import Observation
import TogoKit

/// The three layers of a session. Chat is the default where the harness has an adapter; the
/// terminal is the source of truth, one tap away.
enum SessionLayer: String, Hashable, Sendable {
    case chat
    case terminal
}

/// Where "Show in terminal" should land: the terminal scrolls `linesFromBottom` up and marks
/// the line holding `needle`.
struct TerminalJump: Hashable, Sendable {
    var sessionID: String
    var linesFromBottom: Int
    var needle: String?
}

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
    private(set) var feed = HarnessFeedState()
    /// The main conversation folded into rows, rebuilt on every change.
    private(set) var rows: [ChatRow] = []
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
    /// A subagent whose own conversation is shown (its agent item id), or nil for the main one.
    var focusedAgent: String?
    /// Set by "Show in terminal"; the terminal layer consumes and clears it.
    var pendingJump: TerminalJump?
    /// The pill asked for mission control.
    var showsMission = false
    /// Why the native layers are not showing, when they fell back to the terminal.
    private(set) var fallback: String?

    @ObservationIgnored private var layerChosen = false
    @ObservationIgnored private var runTask: Task<Void, Never>?
    /// Delivers held prompts after a change; cancelled with the feed.
    @ObservationIgnored private var pumpTask: Task<Void, Never>?
    @ObservationIgnored private var pumpToken: UUID?
    @ObservationIgnored private var needsDescribe = true
    @ObservationIgnored private let retryFloor: Duration
    @ObservationIgnored private let retryCeiling: Duration

    init(
        subject: HarnessSubject, services: any HarnessServices,
        retry: (floor: Duration, ceiling: Duration) = (.milliseconds(500), .seconds(10))
    ) {
        self.subject = subject
        self.services = services
        retryFloor = retry.floor
        retryCeiling = retry.ceiling
    }

    var hasAdapter: Bool { descriptor?.adapter != nil }

    func has(_ capability: String) -> Bool { descriptor?.has(capability) ?? false }

    var timeline: HarnessTimeline { feed.timeline }

    var mission: MissionState { feed.mission }

    /// A turn is running, so a prompt waits and Interrupt shows.
    var working: Bool { feed.mission.summary.working }

    /// The chat layer can show: an adapter serves the session with a conversation.
    var chatAvailable: Bool { hasAdapter && has(HarnessCapability.chat) && phase != .terminalOnly }

    /// The layer on screen: the terminal whenever the chat cannot show.
    var shownLayer: SessionLayer { chatAvailable ? layer : .terminal }

    // MARK: Feed

    /// Runs `start` until `stop`. Calling it again while it runs does nothing. The task holds
    /// the store only for one step at a time, so a store nobody keeps goes, and its loop with it.
    func launch() {
        guard runTask == nil else { return }
        runTask = Task { [weak self] in await HarnessSessionStore.run(weak: self) }
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
            } catch {
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
                    changed()
                    if outcome == .reopen { return .reopen }
                case .note(let note):
                    if feed.apply(note) == .reopen {
                        if note.kind == .reset, note.reason == "adapter" { needsDescribe = true }
                        return .reopen
                    }
                    changed()
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

    private func adopt(_ descriptor: HarnessDescriptor) {
        self.descriptor = descriptor
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

    private func changed() {
        rows = SubagentThread.mainRows(feed.timeline)
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
        rows = SubagentThread.mainRows(feed.timeline)
    }

    // MARK: Composer

    struct QueueFull: Error {}

    /// Sends a prompt. `.queue` (the default) holds it on the phone while the agent works on a
    /// harness that cannot queue, and sends it when the turn ends; `.now` goes at once and
    /// throws when the helper refuses it.
    func send(_ text: String, mode: HarnessSendParams.Mode = .queue) async throws {
        if mode == .now {
            let item = try await services.send(subject, text: text, mode: .now)
            feed.insertSent(item)
            changed()
            return
        }
        guard queue.add(text) != nil else {
            if queue.isFull { throw QueueFull() }
            return
        }
        await deliverNext()
    }

    func interrupt() async throws {
        _ = try await services.interrupt(subject)
    }

    func cancelQueued(_ id: UUID) {
        queue.cancel(id)
    }

    /// Takes back a prompt the harness holds in its own queue: `stop` aimed at the queued user
    /// item, which the engine drops from the send queue. False when the helper refused.
    @discardableResult
    func cancelQueued(item: HarnessItem) async -> Bool {
        guard let result = try? await run(action: "stop", target: item.id) else { return false }
        return result.result != .failed
    }

    func retryQueued(_ id: UUID) {
        queue.retry(id)
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
            let item = try await services.send(subject, text: prompt.text, mode: .queue)
            queue.sent(prompt.id)
            feed.insertSent(item)
            rows = SubagentThread.mainRows(feed.timeline)
        } catch HelperError.conflict(let reason) where reason == "busy" {
            // The turn is still going; the next change of the mission tries again.
            queue.requeue(prompt.id)
            return
        } catch is CancellationError {
            // Stopped on the way: it waits for the next launch rather than reading as failed.
            queue.requeue(prompt.id)
            return
        } catch {
            queue.failed(prompt.id, message: ServiceErrors.message(error))
            return
        }
        await deliverNext()
    }

    // MARK: Actions

    func run(action: String, target: String? = nil, args: [String: JSONValue]? = nil) async throws -> HarnessActionResult {
        try await services.act(subject, action: action, target: target, args: args)
    }

    func screen(_ id: String, query: [String: String]? = nil) async throws -> HarnessScreen {
        try await services.screen(subject, id: id, query: query)
    }

    /// Answers a question item: option ids, or typed text, per question id.
    func answer(_ item: HarnessItem, answers: [String: [String]]) async throws -> HarnessActionResult {
        let value = JSONValue.object(answers.mapValues { .array($0.map(JSONValue.string)) })
        return try await run(action: "answer", target: item.id, args: ["answers": value])
    }

    /// Resolves an item's anchor and sets `pendingJump`, switching to the terminal layer.
    func showInTerminal(_ item: HarnessItem) async {
        await showInTerminal(anchor: item.anchor)
    }
}
