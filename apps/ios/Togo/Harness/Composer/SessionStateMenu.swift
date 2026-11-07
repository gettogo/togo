import SwiftUI
import TogoKit

/// The session's state as the composer shows it: model, effort and mode are session state, not
/// commands (audit 3 §1.2). One chip `[Opus · high · Manual ⌃⌄]` in the composer and the
/// palette's Session section read the same rows. A segment the harness doesn't report is left
/// out, and the bare word "Mode" never shows.
enum StateChip {
    /// One setting the session has: a short list picked in place, a long list on its own screen,
    /// or the mode.
    struct Row: Equatable, Identifiable {
        enum Control: Equatable {
            case choices([HarnessCompletion.Choice], action: String)
            case screen(String)
            case mode
        }

        var id: String
        /// "Model", "Effort", "Mode".
        var title: String
        var control: Control
        /// The command it stands for, so filtering for "/effort" finds the row.
        var command: HarnessCompletion?
    }

    /// The Session rows: each composer screen the descriptor lists (Model, Effort), as a menu
    /// when a command carries its choices and as a screen otherwise, then the mode. A command
    /// whose choices are the modes is the Mode row, listed once.
    static func rows(
        screens: [HarnessDescriptor.ScreenRef], commands: [HarnessCompletion], modes: [HarnessChoice], hasMode: Bool
    ) -> [Row] {
        var out: [Row] = []
        for screen in screens where screen.section == "composer" {
            let command = commands.first { $0.screen == screen.id }
            if let command, case let .choose(choices, action) = CommandKind.dispatch(command) {
                out.append(Row(id: screen.id, title: screen.title, control: .choices(choices, action: action), command: command))
            } else {
                out.append(Row(id: screen.id, title: screen.title, control: .screen(screen.id), command: command))
            }
        }
        if hasMode, !modes.isEmpty {
            out.append(Row(id: "mode", title: PrimitiveCopy.mode, control: .mode, command: modeCommand(commands, modes: modes)))
        }
        return out
    }

    /// The command whose choices are the session's modes (aider /chat-mode).
    static func modeCommand(_ commands: [HarnessCompletion], modes: [HarnessChoice]) -> HarnessCompletion? {
        let ids = Set(modes.map(\.id))
        return commands.first { command in
            guard let choices = command.choices, !choices.isEmpty else { return false }
            return Set(choices.map(\.id)).isSubset(of: ids)
        }
    }

    /// The commands the Session rows stand for, left out of the other sections.
    static func folded(_ rows: [Row]) -> Set<String> {
        Set(rows.compactMap { $0.command.map(CommandKind.line) })
    }

    /// What the harness says a row's setting is now: the status block's model or effort, by
    /// the row's name.
    static func reported(_ row: Row, status: HarnessStatus?) -> String? {
        let key = "\(row.id) \(row.title)".lowercased()
        if key.contains("effort") { return status?.effort }
        if key.contains("model") { return status?.model }
        return nil
    }

    /// A row's current value in words: the reported one, matched to a choice's label when it
    /// is one, else the choice the helper marked.
    static func value(_ row: Row, status: HarnessStatus?, modeLabel: String?) -> String? {
        switch row.control {
        case .mode:
            return modeLabel
        case .choices(let choices, _):
            let reported = self.reported(row, status: status)
            if let match = CommandKind.current(choices, reported: reported) { return match.label }
            return reported.flatMap { $0.isEmpty ? nil : $0 }
        case .screen:
            return reported(row, status: status).flatMap { $0.isEmpty ? nil : $0 }
        }
    }

    /// "Opus · high · Manual": the values the harness reports, in row order. nil while the
    /// session is still being described (a neutral placeholder shows) or when nothing is known.
    static func title(rows: [Row], status: HarnessStatus?, modeLabel: String?, describing: Bool) -> String? {
        guard !describing else { return nil }
        let parts = rows.compactMap { value($0, status: status, modeLabel: modeLabel) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }
}

/// The state chip: the session's model, effort and mode in one capsule, and a menu with a Model
/// submenu (or "Model…" for a long list) and Effort and Mode inline. It stays in the composer
/// on every device, never in the Duo's vertical bar.
struct SessionStateChip: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let model: CommandLineModel

    var body: some View {
        let rows = model.sessionRows
        let title = model.stateTitle
        Menu {
            ForEach(rows) { row in
                rowMenu(row)
            }
        } label: {
            // While the session is described, a neutral placeholder: the dial alone, no word.
            Label {
                if let title {
                    Text(title)
                        // trunc-ok: two lines in a full-width row of its own; a long model name wraps inside the chip rather than losing its end.
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)
                        // motion: state change — the words morph in place when the harness reports a new value.
                        .contentTransition(reduceMotion ? .opacity : .interpolate)
                        .animation(Motion.animation(.respond, reduce: reduceMotion), value: title)
                }
            } icon: {
                Image(systemName: "dial.medium")
            }
            .labelStyle(.titleAndIcon)
        }
        .menuStyle(.button)
        .buttonStyle(ComposerChipStyle())
        .accessibilityLabel(ComposerCopy.sessionState)
        .accessibilityValue(title ?? "")
        .task { await model.loadCommandsOnce() }
    }

    @ViewBuilder private func rowMenu(_ row: StateChip.Row) -> some View {
        switch row.control {
        case .mode:
            Picker(row.title, selection: Binding(get: { model.mode }, set: { id in Task { await model.setMode(id) } })) {
                ForEach(model.modes) { Text($0.label).tag($0.id) }
            }
            .pickerStyle(.inline)
        case let .choices(choices, _):
            let picker = Picker(row.title, selection: Binding(
                get: { model.currentChoice(row)?.id ?? "" },
                set: { id in
                    guard let choice = choices.first(where: { $0.id == id }) else { return }
                    Task { await model.pick(choice, row: row) }
                }
            )) {
                ForEach(choices) { Text($0.label).tag($0.id) }
            }
            if row.id == "model" || row.title == PrimitiveCopy.model {
                Menu {
                    picker.pickerStyle(.inline)
                } label: {
                    Text(row.title)
                    if let value = StateChip.value(row, status: model.status, modeLabel: nil) { Text(value) }
                }
            } else {
                picker.pickerStyle(.inline)
            }
        case .screen(let id):
            Button {
                model.presentedScreen = id
            } label: {
                Text(ComposerCopy.opensList(row.title))
                if let value = StateChip.value(row, status: model.status, modeLabel: nil) { Text(value) }
            }
        }
    }
}
