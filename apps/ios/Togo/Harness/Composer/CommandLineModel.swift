import Foundation
import os
import Observation
import TogoKit

/// The smart command line's state: the draft, the / and @ menus, history and favourites, the
/// mode chip and the prompts queued while the agent works.
@MainActor @Observable final class CommandLineModel {
    /// What the menu above the field lists.
    enum Menu: Equatable {
        case none
        case commands([HarnessCompletion])
        case files([HarnessCompletion])

        var items: [HarnessCompletion] {
            switch self {
            case .none: []
            case .commands(let items), .files(let items): items
            }
        }
    }

    /// Everything the model asks of the session, so tests can stand in for it.
    struct Routes {
        var has: @MainActor (String) -> Bool
        var working: @MainActor () -> Bool
        var mode: @MainActor () -> String
        var modes: @MainActor () -> [HarnessChoice]
        /// Queued prompts the conversation shows, oldest first.
        var queued: @MainActor () -> [HarnessItem]
        /// Names the harness, so history and favourites stay with it.
        var scope: @MainActor () -> String
        var complete: @MainActor (HarnessCompleteParams.Kind, String) async throws -> [HarnessCompletion]
        /// Sends a prompt; the user item when the route returns one.
        var send: @MainActor (String, HarnessSendParams.Mode) async throws -> HarnessItem?
        var interrupt: @MainActor () async throws -> HarnessActionResult
        var run: @MainActor (String, String?, [String: JSONValue]?) async throws -> HarnessActionResult
        /// A prompt sent now reaches the session: the agent is idle, or the harness queues.
        var canSendNow: @MainActor () -> Bool = { true }
        /// Prompts the phone holds before the helper has them, oldest first.
        var pending: @MainActor () -> [PendingPrompt] = { [] }
        var cancelPending: @MainActor (UUID) -> Void = { _ in }
        var retryPending: @MainActor (UUID) -> Void = { _ in }
        /// Sends a prompt with image paths in it, as one message.
        var sendImages: (@MainActor (String, HarnessSendParams.Mode, [String]) async throws -> HarnessItem?)?
        /// Writes a picture to the session's folder on the machine; nil where it cannot take one.
        var upload: ComposerAttachments.Upload?

        /// Pictures go through `fs.upload` where the machine's helper serves it.
        @MainActor static func uploadRoute(_ store: HarnessSessionStore) -> ComposerAttachments.Upload? {
            guard store.subject.capabilities.contains(Capability.upload) else { return nil }
            let services = store.services
            let subject = store.subject
            return { name, data in try await services.upload(subject, name: name, data: data) }
        }

        @MainActor static func store(_ store: HarnessSessionStore) -> Routes {
            Routes(
                has: { store.has($0) },
                working: { store.working },
                mode: { store.currentMode },
                modes: { store.descriptor?.modes ?? [] },
                queued: {
                    store.timeline.conversation().filter { item in
                        if case let .user(user) = item.body { user.queued } else { false }
                    }
                },
                scope: { store.descriptor?.adapter?.id ?? "" },
                complete: { kind, query in try await store.services.complete(store.subject, kind: kind, query: query) },
                send: { text, mode in
                    try await store.send(text, mode: mode)
                    return nil
                },
                interrupt: { try await store.interrupt() },
                run: { action, target, args in try await store.run(action: action, target: target, args: args) },
                canSendNow: { store.canSendNow },
                pending: { store.queue.prompts },
                cancelPending: { store.cancelQueued($0) },
                retryPending: { store.retryQueued($0) },
                sendImages: { text, mode, images in
                    try await store.send(text, mode: mode, images: images)
                    return nil
                },
                upload: uploadRoute(store)
            )
        }
    }

    static let debounce: Duration = .milliseconds(150)

    var draft = ""
    private(set) var menu: Menu = .none
    /// The menu row the keyboard is on.
    var highlight = 0
    /// A screen a completion opened, shown as a sheet.
    var presentedScreen: String?
    /// The full command list is open.
    var paletteOpen = false
    private static let log = Logger(subsystem: "app.togo", category: "composer")
    /// The command list could not be read; the palette offers to try again.
    private(set) var paletteFailed = false
    /// Every command, for the palette; loaded when it opens.
    private(set) var palette: [HarnessCompletion] = []
    private(set) var sending = false
    /// One quiet line about the last thing that did not work, or what an action said.
    private(set) var notice: String?
    /// Prompts this phone queued that the conversation does not show yet.
    private(set) var pendingQueued: [HarnessItem] = []
    /// A mode this phone set, with the mode it replaced: it holds until the session reports
    /// another mode than that one.
    private var modeOverride: (id: String, over: String)?
    private var historyIndex: Int?
    private var cache: (kind: HarnessCompleteParams.Kind, query: String, items: [HarnessCompletion])?

    @ObservationIgnored let store: HarnessSessionStore
    @ObservationIgnored let memory: CommandMemory
    @ObservationIgnored var routes: Routes
    /// Pictures waiting to go with the next prompt.
    let attachments: ComposerAttachments

    init(
        store: HarnessSessionStore, memory: CommandMemory = .shared, routes: Routes? = nil,
        attachments: ComposerAttachments? = nil
    ) {
        self.store = store
        self.memory = memory
        let routes = routes ?? .store(store)
        self.routes = routes
        self.attachments = attachments ?? ComposerAttachments(upload: routes.upload)
    }

    // MARK: Session state

    /// The mode chip's current mode id.
    var mode: String {
        let reported = routes.mode()
        guard let modeOverride, modeOverride.over == reported else { return reported }
        return modeOverride.id
    }
    var modes: [HarnessChoice] { routes.modes() }
    var modeLabel: String? { ModeMemory.label(for: mode, in: modes) }
    /// The agent is working, so a send queues and Interrupt shows.
    var working: Bool { routes.working() }
    var showsModeChip: Bool { routes.has(HarnessCapability.mode) && !modes.isEmpty }
    var canInterrupt: Bool { working && routes.has(HarnessCapability.interrupt) }
    var scope: String { routes.scope() }

    /// Queued prompts, oldest first: the conversation's, then this phone's not yet shown there.
    var queued: [HarnessItem] {
        let shown = routes.queued()
        let ids = Set(shown.map(\.id))
        return shown + pendingQueued.filter { !ids.contains($0.id) }
    }

    /// Something to send, and every picture already on the machine.
    var canSend: Bool {
        guard !sending, !attachments.isUploading else { return false }
        if attachments.isEmpty { return CommandLineLogic.outgoing(draft) != nil }
        return attachments.isReady
    }

    /// A prompt sent now reaches the session; otherwise Send holds it on the phone.
    var canSendNow: Bool { routes.canSendNow() }

    /// Prompts this phone holds until the session can take them, or that failed to go.
    var pending: [PendingPrompt] { routes.pending() }

    /// How many prompts wait, the harness's and the phone's. The chat shows the prompts; the
    /// composer shows only this.
    var queuedCount: Int { queued.count + pending.count }

    func cancelPending(_ id: UUID) { routes.cancelPending(id) }

    func retryPending(_ id: UUID) { routes.retryPending(id) }

    // MARK: Menus

    /// Updates the menu for the draft: a leading "/" lists commands, an "@" word lists files.
    /// The held list filters at once; the harness is asked after a short pause. Run it from a
    /// task keyed on the draft so a newer keystroke cancels the older ask.
    func draftChanged() async {
        if let index = historyIndex, memory.history(scope).indices.contains(index), memory.history(scope)[index] != draft {
            historyIndex = nil
        }
        guard let trigger = CommandLineLogic.trigger(in: draft), allows(trigger) else {
            menu = .none
            return
        }
        if let cache, cache.kind == trigger.kind, trigger.query.lowercased().hasPrefix(cache.query.lowercased()) {
            show(CommandLineLogic.filter(cache.items, query: trigger.query), for: trigger)
        }
        try? await Task.sleep(for: Self.debounce)
        guard !Task.isCancelled else { return }
        let items: [HarnessCompletion]
        do {
            items = try await routes.complete(trigger.kind, trigger.query)
        } catch {
            return
        }
        guard !Task.isCancelled, CommandLineLogic.trigger(in: draft) == trigger else { return }
        cache = (trigger.kind, trigger.query, items)
        show(items, for: trigger)
    }

    private func allows(_ trigger: CommandLineLogic.Trigger) -> Bool {
        switch trigger {
        case .command: routes.has(HarnessCapability.commands)
        case .file: routes.has(HarnessCapability.files)
        }
    }

    private func show(_ items: [HarnessCompletion], for trigger: CommandLineLogic.Trigger) {
        switch trigger {
        case .command:
            let ordered = CommandLineLogic.ordered(items, favourites: memory.favourites(scope), recents: memory.recentCommands(scope))
            menu = ordered.isEmpty ? .none : .commands(ordered)
        case .file:
            menu = items.isEmpty ? .none : .files(items)
        }
        highlight = min(highlight, max(0, menu.items.count - 1))
    }

    func closeMenu() {
        menu = .none
        highlight = 0
    }

    /// Moves the keyboard's place in the menu, wrapping at either end.
    func moveHighlight(_ step: Int) {
        let count = menu.items.count
        guard count > 0 else { return }
        highlight = ((highlight + step) % count + count) % count
    }

    var highlighted: HarnessCompletion? {
        let items = menu.items
        return items.indices.contains(highlight) ? items[highlight] : nil
    }

    /// A completion with a screen opens it; any other lands in the draft in place of what
    /// called the menu.
    func choose(_ completion: HarnessCompletion) {
        let trigger = CommandLineLogic.trigger(in: draft)
        closeMenu()
        cache = nil
        if let screen = completion.screen, !screen.isEmpty {
            if case .command = trigger { draft = "" }
            memory.record(CommandMemory.commandWord(completion.insert.isEmpty ? completion.title : completion.insert), scope: scope)
            presentedScreen = screen
            return
        }
        guard let trigger else {
            draft += completion.insert
            return
        }
        draft = CommandLineLogic.applying(completion, to: draft, trigger: trigger)
    }

    /// Return: takes the highlighted completion while the menu is open, unless the command is
    /// already typed in full; otherwise sends.
    func returnPressed() async {
        if menu != .none, let highlighted, !CommandLineLogic.returnSends(draft, highlighted: highlighted) {
            choose(highlighted)
            return
        }
        await submit()
    }

    // MARK: History and favourites

    /// Up and down through what was sent, while the field is empty or showing a past line.
    func recall(older: Bool) {
        let history = memory.history(scope)
        guard !history.isEmpty, draft.isEmpty || historyIndex != nil else { return }
        let next = older ? (historyIndex.map { $0 + 1 } ?? 0) : (historyIndex ?? 0) - 1
        if next < 0 {
            historyIndex = nil
            draft = ""
        } else if next < history.count {
            historyIndex = next
            draft = history[next]
        }
    }

    func isFavourite(_ completion: HarnessCompletion) -> Bool {
        memory.isFavourite(completion.insert.isEmpty ? completion.title : completion.insert, scope: scope)
    }

    func toggleFavourite(_ completion: HarnessCompletion) {
        memory.toggleFavourite(completion.insert.isEmpty ? completion.title : completion.insert, scope: scope)
    }

    /// Loads every command for the palette.
    func loadPalette() async {
        guard routes.has(HarnessCapability.commands) else { return }
        paletteFailed = false
        do {
            palette = try await routes.complete(.command, "")
        } catch {
            Self.log.error("command list failed: \(String(describing: error), privacy: .public)")
            // A spinner that never ends reads as a hang: say so and offer to try again.
            paletteFailed = palette.isEmpty
        }
    }

    // MARK: Sending

    /// Sends the draft: now while the agent is idle, queued while it works. A refusal for
    /// being busy queues instead; any other failure puts the words back.
    /// With pictures attached, the prompt carries them all, in one message.
    func submit() async {
        guard canSend else { return }
        let text = CommandLineLogic.outgoing(draft) ?? ""
        let images = attachments.paths
        guard !text.isEmpty || !images.isEmpty else { return }
        sending = true
        defer { sending = false }
        draft = ""
        closeMenu()
        historyIndex = nil
        notice = nil
        if !text.isEmpty { memory.record(text, scope: scope) }
        let mode = CommandLineLogic.sendMode(working: working || !canSendNow)
        do {
            try await send(text, mode: mode, images: images)
            attachments.clear()
        } catch HelperError.conflict("busy") where mode == .now {
            do {
                try await send(text, mode: .queue, images: images)
                attachments.clear()
            } catch {
                restore(text, error)
            }
        } catch {
            restore(text, error)
        }
    }

    /// Sends a whole line from the palette, such as "/compact focus on tests".
    func send(line: String) async {
        draft = line
        await submit()
    }

    private func send(_ text: String, mode: HarnessSendParams.Mode, images: [String] = []) async throws {
        let sent: HarnessItem?
        if images.isEmpty {
            sent = try await routes.send(text, mode)
        } else {
            guard let sendImages = routes.sendImages else { throw HarnessUnavailable() }
            sent = try await sendImages(text, mode, images)
        }
        guard let item = sent else { return }
        if case let .user(user) = item.body, user.queued { pendingQueued.append(item) }
    }

    /// The words go back in the field; the pictures stay attached.
    private func restore(_ text: String, _ error: any Error) {
        if draft.isEmpty { draft = text }
        notice = ComposerCopy.notSent
    }

    /// Stop: a stop the helper could not finish says so, rather than leaving Stop looking dead.
    func interrupt() async {
        guard routes.has(HarnessCapability.interrupt) else { return }
        do {
            let result = try await routes.interrupt()
            switch result.result {
            case .failed: notice = result.message ?? ComposerCopy.notInterrupted
            case .fallback: notice = ComposerCopy.notInterrupted
            default: break
            }
        } catch {
            notice = ComposerCopy.notInterrupted
        }
    }

    /// Takes a queued prompt back before its turn.
    func cancelQueued(_ item: HarnessItem) async {
        do {
            let result = try await routes.run("stop", item.id, nil)
            if result.result != .failed {
                pendingQueued.removeAll { $0.id == item.id }
            } else {
                notice = result.message ?? ComposerCopy.notCancelled
            }
        } catch {
            notice = ComposerCopy.notCancelled
        }
    }

    func setMode(_ id: String) async {
        guard id != mode, modes.contains(where: { $0.id == id }) else { return }
        do {
            let result = try await routes.run("set_mode", nil, ["id": .string(id)])
            switch result.result {
            case .failed, .fallback:
                notice = result.message ?? ComposerCopy.modeNotChanged
            default:
                modeOverride = (id, routes.mode())
                notice = nil
            }
        } catch {
            notice = ComposerCopy.modeNotChanged
        }
    }

    func clearNotice() {
        notice = nil
    }
}
