import Foundation
import TogoKit

/// What a command does when it is chosen, from its kind (audit 3 §1.2). The row is the
/// command: one tap does its job, or opens the one native control the job needs. The palette
/// and the inline `/` menu both dispatch through here, so they agree for every kind.
enum CommandKind {
    enum Dispatch: Equatable {
        /// A menu in place with the current value checked; a pick runs `action`.
        case choose([HarnessCompletion.Choice], action: String)
        /// Push the searchable picker screen.
        case push(screen: String)
        /// To the composer with the cursor after it; `files` opens the @ menu at once.
        case compose(line: String, files: Bool)
        /// Runs: through `action` when it has one, else typed. A destructive one asks first.
        case run(Run)
        /// Typed, and the pane switches to the terminal, where its result is.
        case terminal(line: String)
        /// A native home instead of typing: mission control, or a screen.
        case open(Target)
    }

    struct Run: Equatable {
        /// "/clear", what is typed when there is no action.
        var line: String
        var action: String?
        var destructive: Bool
        /// "/compact ", when the command takes optional words ("With instructions…").
        var instructions: String?
        /// "Start a new conversation", for the dialog's title.
        var detail: String?
    }

    enum Target: Equatable {
        /// Mission control: `status` or `changes`.
        case mission(String)
        case screen(String)
    }

    /// The command's kind, derived for older helpers that do not send one.
    static func kind(of command: HarnessCompletion) -> HarnessCompletion.Kind {
        if let kind = command.kind { return kind }
        if command.opens?.isEmpty == false { return .run }
        if command.choices?.isEmpty == false, command.action != nil { return .choice }
        if command.screen?.isEmpty == false { return .picker }
        return requiresArgument(command.hint) ? .args : .run
    }

    static func dispatch(_ command: HarnessCompletion) -> Dispatch {
        let line = self.line(command)
        if let opens = command.opens, !opens.isEmpty { return .open(target(opens)) }
        switch kind(of: command) {
        case .choice:
            if let choices = command.choices, !choices.isEmpty, let action = command.action { return .choose(choices, action: action) }
            if let screen = command.screen, !screen.isEmpty { return .push(screen: screen) }
        case .picker:
            if let screen = command.screen, !screen.isEmpty { return .push(screen: screen) }
        case .terminal:
            return .terminal(line: line)
        case .args:
            return .compose(line: line + " ", files: takesFile(command.hint))
        default:
            break
        }
        if requiresArgument(command.hint) { return .compose(line: line + " ", files: takesFile(command.hint)) }
        return .run(Run(
            line: line, action: command.action, destructive: command.role == .destructive,
            instructions: takesOptionalArgument(command.hint) ? line + " " : nil, detail: command.detail
        ))
    }

    /// "/compact": the command word, without a trailing space.
    static func line(_ command: HarnessCompletion) -> String {
        (command.insert.isEmpty ? command.title : command.insert).trimmingCharacters(in: .whitespaces)
    }

    static func target(_ opens: String) -> Target {
        switch opens {
        case "status", "changes": .mission(opens)
        default: .screen(opens)
        }
    }

    /// "<file>": the command needs words before it can go.
    static func requiresArgument(_ hint: String?) -> Bool {
        hint?.trimmingCharacters(in: .whitespaces).hasPrefix("<") ?? false
    }

    /// "[instructions]": the command takes optional words.
    static func takesOptionalArgument(_ hint: String?) -> Bool {
        hint?.trimmingCharacters(in: .whitespaces).hasPrefix("[") ?? false
    }

    /// "<file>" or "<files…>": the argument is a path, so the @ menu opens with the cursor.
    static func takesFile(_ hint: String?) -> Bool {
        guard let hint = hint?.lowercased() else { return false }
        return hint.contains("file") || hint.contains("path")
    }

    /// Whether an args command in the draft has its argument: "/add src/a.go" does, "/add",
    /// "/add " and "/add @" do not. A draft that is not this command is not held back.
    static func argumentGiven(draft: String, command: String) -> Bool {
        let word = command.trimmingCharacters(in: .whitespaces)
        guard !word.isEmpty, draft.hasPrefix(word) else { return true }
        let rest = draft.dropFirst(word.count)
        guard rest.isEmpty || rest.first?.isWhitespace == true else { return true }
        let argument = rest.trimmingCharacters(in: .whitespacesAndNewlines)
        return !argument.isEmpty && argument != "@"
    }

    /// The trailing glyph that says what a row does: a menu, a list, the cursor, the terminal.
    /// nil for a command that simply runs.
    static func glyph(_ command: HarnessCompletion) -> String? {
        switch dispatch(command) {
        case .choose: "chevron.up.chevron.down"
        case .push, .open: "chevron.forward"
        case .compose: "text.cursor"
        case .terminal: "terminal"
        case .run: nil
        }
    }

    /// The current value of a choice command, for the row's trailing text.
    static func current(_ choices: [HarnessCompletion.Choice], reported: String? = nil) -> HarnessCompletion.Choice? {
        if let reported = reported?.lowercased(), !reported.isEmpty,
           let match = choices.first(where: { $0.id.lowercased() == reported || $0.label.lowercased() == reported }) {
            return match
        }
        return choices.first { $0.selected == true }
    }
}
