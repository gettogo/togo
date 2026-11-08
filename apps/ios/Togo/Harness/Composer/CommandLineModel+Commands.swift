import Foundation
import TogoKit

/// What a composer notice offers besides its words.
enum NoticeAction: Equatable {
    /// A stop that did not land: the terminal has Esc.
    case openTerminal
    /// A prompt that did not go: its words are back in the field.
    case retry
    /// "/xyz" is not one of the agent's commands: the words stay in the field until sent as text.
    case sendAsText
}

/// Running commands by kind, picking session values and the gate's context. The palette, the
/// inline `/` menu and the state chip all come through here.
extension CommandLineModel {
    /// What the palette does after a command was chosen.
    enum Outcome: Equatable {
        /// Push this screen in the palette's stack.
        case push(String)
        /// The job is done or on its way: close the palette.
        case close
        /// A dialog asks first, or a menu shows in place: keep the palette.
        case stay
    }

    /// The context `ActionGate` changes run in: the composer's routes, the session's notes.
    var gateContext: PrimitiveContext {
        let routes = self.routes
        let store = self.store
        var context = PrimitiveContext(
            run: { action, target, args in
                do {
                    return try await routes.run(action, target, args)
                } catch is CancellationError {
                    return nil
                } catch {
                    return HarnessActionResult(result: .failed, message: ServiceErrors.message(error))
                }
            },
            openScreen: { _ in }, open: { _ in }, showTerminal: { routes.showTerminal() }
        )
        context.working = { routes.working() }
        context.agent = routes.agent()
        context.compose = { [weak self] line in self?.compose(line) }
        context.instructionsLine = { store.instructionsLine(for: $0) }
        context.runAfterTurn = { action, target, args, note in
            store.runAfterTurn(action: action, target: target, args: args, note: note)
        }
        context.note = { text, undo in store.note(text, code: "model", undo: undo) }
        return context
    }

    /// When the state chip reads the command list again: once described, then whenever a turn
    /// ends or the harness reports another model, effort or mode. nil while a turn runs (a
    /// change mid-turn shows at its end).
    var chipRefresh: ChipRefresh? {
        if working { return nil }
        return ChipRefresh(
            describing: routes.describing(), model: status?.model, effort: status?.effort, mode: mode
        )
    }

    /// Reads the command list for the chip's choices: the first time, and on each change of
    /// `chipRefresh` (the choices' `selected` flag is what a harness without a status block
    /// reports its model and effort with).
    func refreshChip() async {
        guard chipRefresh != nil || palette.isEmpty else { return }
        await loadPalette()
    }

    /// Puts a line in the field with the cursor after it ("/compact ", "/add @").
    func compose(_ line: String) {
        paletteOpen = false
        draft = line
        requestFocus()
    }

    /// Does what the command is for, by kind (`CommandKind`).
    @discardableResult
    func perform(_ command: HarnessCompletion) async -> Outcome {
        memory.record(CommandMemory.commandWord(CommandKind.line(command)), scope: scope)
        switch CommandKind.dispatch(command) {
        case .choose:
            return .stay
        case .push(let screen), .open(.screen(let screen)):
            return .push(screen)
        case .open(.mission):
            paletteOpen = false
            routes.showMission()
            return .close
        case let .compose(line, files):
            pendingArgs = line
            compose(files ? line + "@" : line)
            return .close
        case .terminal(let line):
            paletteOpen = false
            // The pane shows the terminal as the command goes: its result, or its dialog, is there.
            routes.showTerminal()
            await send(line: line)
            return .close
        case .run(let run):
            return await runCommand(run)
        }
    }

    /// Runs a run-kind command. A destructive one, one with a confirm line, or one that takes
    /// optional words through an action ("With instructions…") asks first in a system dialog.
    private func runCommand(_ run: CommandKind.Run) async -> Outcome {
        let ref = run.action.flatMap(routes.actionRef)
        let label = ref?.label ?? Self.label(run.line)
        var confirm = ref?.confirm
        if confirm == nil, run.action != nil, run.instructions != nil {
            confirm = run.detail.map(PrimitiveCopy.asking) ?? PrimitiveCopy.asking(label)
        }
        let decision = ActionGate.decide(
            confirm: confirm, effect: nil, working: working, label: label, agent: routes.agent(),
            destructive: run.destructive, instructions: run.instructions
        )
        guard case .confirm(var dialog) = decision else {
            paletteOpen = false
            await execute(run)
            return .close
        }
        if dialog.title == PrimitiveCopy.asking(label), let detail = run.detail { dialog.title = PrimitiveCopy.asking(detail) }
        gate.asking = ActionGateRequest(
            dialog: dialog,
            run: { [weak self] in
                self?.paletteOpen = false
                await self?.execute(run)
            },
            instructions: { [weak self] line in self?.compose(line) }
        )
        return .stay
    }

    /// "Compact" from "/compact".
    private static func label(_ line: String) -> String {
        let word = line.drop { $0 == "/" }
        return word.prefix(1).uppercased() + word.dropFirst()
    }

    private func execute(_ run: CommandKind.Run) async {
        guard let action = run.action else {
            await send(line: run.line)
            return
        }
        let result = await gateContext.run(action, nil, nil)
        switch result?.result {
        case .failed?:
            notice = result?.message ?? PrimitiveCopy.failed
        case .fallback?:
            routes.showTerminal()
        default:
            break
        }
    }

    /// "Add instructions…": the command in the field, the cursor after it.
    func addInstructions(_ command: HarnessCompletion) {
        compose(CommandKind.line(command) + " ")
    }

    // MARK: Session values

    /// The value a row has now: the one the harness reports, else the one the helper marked.
    func currentChoice(_ row: StateChip.Row) -> HarnessCompletion.Choice? {
        guard case let .choices(choices, _) = row.control else { return nil }
        return CommandKind.current(choices, reported: StateChip.reported(row, status: status))
    }

    /// Picks a value from a choice row or command: through `ActionGate`, so a change that
    /// restarts the agent asks while it works. A pick that lands closes the palette; the chat
    /// says what changed.
    func pick(_ choice: HarnessCompletion.Choice, row: StateChip.Row) async {
        guard case let .choices(choices, action) = row.control else { return }
        let current = CommandKind.current(choices, reported: StateChip.reported(row, status: status))
        guard current?.id != choice.id else {
            paletteOpen = false
            return
        }
        let screen = row.command?.screen ?? row.id
        let confirm = working ? await routes.confirmFor(screen) : nil
        let change = GatedChange(
            action: action, args: Self.choiceArgs(choice.id), confirm: confirm, setting: row.title, value: choice.label,
            previous: current.map { ($0.label, Self.choiceArgs($0.id)) }
        )
        await gate.change(change, context: gateContext) { [weak self] result in
            self?.picked(result, setting: row.title)
        }
    }

    /// A choice command's pick from the palette or the inline menu.
    func pick(_ choice: HarnessCompletion.Choice, of command: HarnessCompletion, setting: String) async {
        guard case let .choose(choices, action) = CommandKind.dispatch(command) else { return }
        let row = StateChip.Row(id: command.screen ?? CommandKind.line(command), title: setting,
                                control: .choices(choices, action: action), command: command)
        await pick(choice, row: row)
    }

    /// Templates name a picked value `.id` (pickers) or `.value` (settings fields): both go.
    static func choiceArgs(_ id: String) -> [String: JSONValue] {
        ["id": .string(id), "value": .string(id)]
    }

    private func picked(_ result: HarnessActionResult?, setting: String) {
        guard let result else { return }
        switch result.result {
        case .failed, .fallback:
            notice = result.message ?? ComposerCopy.notChanged(setting)
        default:
            paletteOpen = false
            notice = nil
            // The choices' check moves with the next read.
            Task { await self.loadPalette() }
        }
    }

    /// Closes the palette for a screen that wants its own sheet; `paletteClosed` opens it.
    func handOver(to screen: String) {
        handedOverScreen = screen
        paletteOpen = false
    }

    /// The palette has gone: a screen it handed over opens now.
    func paletteClosed() {
        guard let screen = handedOverScreen else { return }
        handedOverScreen = nil
        presentedScreen = screen
    }
}

/// The state that makes the chip read its choices again (`CommandLineModel.chipRefresh`).
struct ChipRefresh: Hashable, Sendable {
    var describing: Bool
    var model: String?
    var effort: String?
    var mode: String
}
