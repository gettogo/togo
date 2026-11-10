import Foundation
import TogoKit

/// A command typed into the field and sent does what picking it in the palette does (S7): a
/// choice or a list opens its native screen, a command with its words goes as a command, a run
/// runs (a destructive one asks), a terminal command switches to the terminal. "/xyz" the agent
/// does not list is sent as typed: the harness judges it.
extension CommandLineModel {
    /// What Send did with a "/" draft. True when it was a command or held as an unknown one;
    /// false when the draft goes as words.
    func routeTyped() async -> Bool {
        guard attachments.isEmpty, let text = CommandLineLogic.outgoing(draft),
              CommandLineLogic.commandWord(text) != nil, routes.has(HarnessCapability.commands) else { return false }
        let commands = await routingCommands()
        // Without the list nothing can be told apart: the line goes as typed, as before.
        guard !commands.isEmpty else { return false }
        switch CommandLineLogic.typed(text, commands: commands) {
        case .text, .unknown:
            // A "/word" the list lacks still goes: the list can miss personal commands and
            // skills, and the harness itself says when a command does not exist.
            return false
        case let .command(command, args):
            await runTyped(command, args: args, text: text)
            return true
        }
    }

    /// The command list to match against: the palette's, the session's, or read now.
    func routingCommands() async -> [HarnessCompletion] {
        if !palette.isEmpty { return palette }
        if !store.commandList.isEmpty { return store.commandList }
        await loadPalette()
        return palette
    }

    private func runTyped(_ command: HarnessCompletion, args: String, text: String) async {
        let dispatch = CommandKind.dispatch(command)
        switch dispatch {
        case let .choose(choices, _):
            if !args.isEmpty, let choice = Self.choice(args, in: choices) {
                taken(text)
                await pick(choice, of: command, setting: setting(command))
                return
            }
            taken(text)
            record(command)
            if let screen = command.screen, !screen.isEmpty {
                presentedScreen = screen
            } else {
                // Its values show in place in the palette, the command already found.
                paletteSeed = CommandKind.line(command)
                paletteOpen = true
            }
        case .push(let screen), .open(.screen(let screen)):
            taken(text)
            record(command)
            presentedScreen = screen
        case .compose:
            if CommandKind.argumentGiven(draft: text, command: CommandKind.line(command)), !args.isEmpty {
                await send(line: text)
            } else {
                taken(text)
                await perform(command, typed: true)
            }
        case .run:
            if args.isEmpty {
                taken(text)
                await perform(command, typed: true)
            } else {
                // "/compact focus on tests": the words go with the command, typed.
                await send(line: text)
            }
        case .terminal:
            if args.isEmpty {
                taken(text)
                await perform(command, typed: true)
            } else {
                routes.showTerminal()
                await send(line: text)
            }
        case .open(.mission):
            taken(text)
            await perform(command, typed: true)
        }
    }

    /// The field lets go of a command that is now doing its job.
    private func taken(_ text: String) {
        memory.record(text, scope: scope)
        draft = ""
        closeMenu()
        pendingArgs = nil
        clearNotice()
    }

    private func record(_ command: HarnessCompletion) {
        memory.record(CommandMemory.commandWord(CommandKind.line(command)), scope: scope)
    }

    /// "sonnet" in "/model sonnet": a choice by its id or its label.
    static func choice(_ args: String, in choices: [HarnessCompletion.Choice]) -> HarnessCompletion.Choice? {
        let wanted = args.lowercased()
        return choices.first { $0.id.lowercased() == wanted || $0.label.lowercased() == wanted }
    }

    /// "Model" for /model, from its screen's title, else the command's own name, as the palette
    /// names it.
    func setting(_ command: HarnessCompletion) -> String {
        if let screen = command.screen, let title = store.descriptor?.screens.first(where: { $0.id == screen })?.title {
            return title
        }
        return command.screen ?? String(CommandKind.line(command).drop { $0 == "/" })
    }
}
