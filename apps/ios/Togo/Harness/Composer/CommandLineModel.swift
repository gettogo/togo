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

    static let debounce: Duration = .milliseconds(150)

    var draft = ""
    private(set) var menu: Menu = .none
    /// The menu row the keyboard is on.
    var highlight = 0
    /// A screen a completion opened, shown as a sheet.
    var presentedScreen: String?
    /// The full command list is open.
    var paletteOpen = false
    /// A screen the palette handed over to: it opens in its own sheet once the palette has
    /// gone, so only one sheet is ever up (`AdaptivePresentation.opening`).
    var handedOverScreen: String?
    private static let log = Logger(subsystem: "app.togo", category: "composer")
    /// The command list could not be read; the palette offers to try again.
    private(set) var paletteFailed = false
    /// Every command, for the palette; loaded when it opens.
    private(set) var palette: [HarnessCompletion] = []
    /// What the palette's search starts with when it opens: a choice command typed in full.
    var paletteSeed: String?
    private(set) var sending = false
    /// One quiet line about the last thing that did not work, or what an action said.
    var notice: String?
    /// What the notice offers: Open terminal after a failed stop, Try again after a failed send.
    var noticeAction: NoticeAction?
    /// An args command waiting for its argument ("/add "): Return stays off until it is there.
    var pendingArgs: String?
    /// Bumped when the field should take the keyboard (an args command, "With instructions…").
    private(set) var focusRequests = 0
    /// Runs session changes through `ActionGate`; its `asking` is the dialog the composer or the
    /// palette shows.
    let gate = PrimitiveActions()
    /// The line that failed to go, for Try again.
    var failedLine: String?
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
    var status: HarnessStatus? { routes.status() }
    /// The agent is working, so a send queues and Interrupt shows.
    var working: Bool { routes.working() }
    var showsModeChip: Bool { routes.has(HarnessCapability.mode) && !modes.isEmpty }
    /// The Session rows the chip and the palette show.
    var sessionRows: [StateChip.Row] {
        StateChip.rows(screens: routes.screens(), commands: palette, modes: modes, hasMode: showsModeChip)
    }
    /// "Opus · high · Manual"; nil for the placeholder.
    var stateTitle: String? {
        StateChip.title(rows: sessionRows, status: status, modeLabel: modeLabel, describing: routes.describing())
    }
    var showsStateChip: Bool { !sessionRows.isEmpty }
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
        if let pendingArgs, !CommandKind.argumentGiven(draft: draft, command: pendingArgs) { return false }
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

    /// A command does its job by kind, as in the palette (`CommandKind`): a list opens its
    /// screen, words go to the field, a run runs. A choice lands in the draft, its values
    /// under it in the menu. A file replaces the word that called the menu.
    func choose(_ completion: HarnessCompletion) {
        let trigger = CommandLineLogic.trigger(in: draft)
        if case .command = trigger, let runs = takeFromMenu(completion) {
            if runs { Task { await self.perform(completion) } }
            return
        }
        closeMenu()
        cache = nil
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
        if menu != .none, let highlighted, case .command = CommandLineLogic.trigger(in: draft), let runs = takeFromMenu(highlighted) {
            // Typed in full: the command still does its job by kind (a destructive one asks).
            if runs { await perform(highlighted) }
            return
        }
        await submit()
    }

    /// A command from the `/` menu does its job by kind, as in the palette. nil for a choice,
    /// which the caller inserts (its values show under it); false when it opened its screen;
    /// true when the caller should `perform` it.
    private func takeFromMenu(_ completion: HarnessCompletion) -> Bool? {
        let dispatch = CommandKind.dispatch(completion)
        if case .choose = dispatch { return nil }
        closeMenu()
        cache = nil
        draft = ""
        switch dispatch {
        case .push(let screen), .open(.screen(let screen)):
            memory.record(CommandMemory.commandWord(CommandKind.line(completion)), scope: scope)
            presentedScreen = screen
            return false
        case let .compose(line, files):
            memory.record(CommandMemory.commandWord(line), scope: scope)
            pendingArgs = line
            compose(files ? line + "@" : line)
            return false
        default:
            return true
        }
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
            store.commandList = palette
        } catch {
            Self.log.error("command list failed: \(String(describing: error), privacy: .public)")
            // A spinner that never ends reads as a hang: say so and offer to try again.
            paletteFailed = palette.isEmpty
        }
    }

    // MARK: Sending

    /// Sends the draft: now while the agent is idle, queued while it works. A refusal for
    /// being busy queues instead; any other failure puts the words back.
    /// With pictures attached, the prompt carries them all, in one message. A draft that is a
    /// command does what picking it in the palette does (`routeTyped`); `asText` sends it as
    /// typed, for "Send as text" and for lines a command already resolved.
    func submit(asText: Bool = false) async {
        guard canSend else { return }
        if !asText, await routeTyped() { return }
        let text = CommandLineLogic.outgoing(draft) ?? ""
        let images = attachments.paths
        guard !text.isEmpty || !images.isEmpty else { return }
        sending = true
        defer { sending = false }
        draft = ""
        closeMenu()
        historyIndex = nil
        notice = nil
        noticeAction = nil
        pendingArgs = nil
        store.undo = nil
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
        await submit(asText: true)
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
        noticeAction = .retry
    }

    /// Stop: a stop the helper could not finish says so, rather than leaving Stop looking dead.
    func interrupt() async {
        guard routes.has(HarnessCapability.interrupt) else { return }
        do {
            let result = try await routes.interrupt()
            switch result.result {
            case .failed: stopFailed(result.message)
            case .fallback: stopFailed(nil)
            default: break
            }
        } catch {
            stopFailed(nil)
        }
    }

    private func stopFailed(_ message: String?) {
        notice = message ?? ComposerCopy.notInterrupted
        noticeAction = .openTerminal
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

    /// Sets the mode through `ActionGate`: a mode change that restarts the agent asks while it
    /// works. The chip keeps the new mode until the session reports another.
    func setMode(_ id: String) async {
        guard id != mode, let choice = modes.first(where: { $0.id == id }) else { return }
        let previous = modes.first { $0.id == mode }
        let over = routes.mode()
        let change = GatedChange(
            action: "set_mode", args: ["id": .string(id)], confirm: nil, setting: PrimitiveCopy.mode, value: choice.label,
            previous: previous.map { ($0.label, ["id": .string($0.id)]) }
        )
        await gate.change(change, context: gateContext) { [weak self] result in
            guard let self else { return }
            switch result?.result {
            case .failed?, .fallback?, nil:
                if result != nil { self.notice = result?.message ?? ComposerCopy.modeNotChanged }
            default:
                self.modeOverride = (id, over)
                self.notice = nil
                self.paletteOpen = false
            }
        }
    }

    /// The mode after the current one, wrapping: Shift+Tab steps through the modes as it does
    /// at the desk. nil when the session has no modes to step through.
    var nextMode: String? {
        guard showsModeChip, modes.count > 1 else { return nil }
        let index = modes.firstIndex { $0.id == mode } ?? -1
        return modes[(index + 1) % modes.count].id
    }

    /// Shift+Tab in the field: the next mode, through the chip's own verified path.
    func cycleMode() async {
        guard let next = nextMode else { return }
        await setMode(next)
    }

    func clearNotice() {
        notice = nil
        noticeAction = nil
    }

    /// The field takes the keyboard.
    func requestFocus() {
        focusRequests += 1
    }
}
