import SwiftUI
import TogoKit

/// Confirm only when it costs (audit 3 §4.1). A change that restarts the agent runs at once
/// while it is idle, with Undo in the chat's notice; while it works it asks, because the
/// current reply stops. Anything else asks only when its action carries a confirm line or is
/// destructive. Pure, so every surface (pickers, settings, the palette, the state chip and
/// mission control) decides the same way.
enum ActionGate {
    /// The system dialog a change asks with.
    struct Dialog: Equatable, Sendable {
        var title: String
        /// The button that runs it: "Restart", "Compact now", the action's own label.
        var confirmLabel: String
        var destructive: Bool
        /// A relaunch while the agent works: "After this reply" holds it for the turn's end.
        var offersAfterReply: Bool
        /// "With instructions…" puts this line in the composer instead of running.
        var instructions: String?
    }

    enum Decision: Equatable, Sendable {
        case run
        case confirm(Dialog)
        /// A relaunch while idle: run it, then offer Undo back to `previous`.
        case runWithUndo(previous: String)
    }

    /// - Parameters:
    ///   - confirm: the action's confirm line, if any.
    ///   - effect: `.relaunch` when the change restarts the agent.
    ///   - working: the agent is in a turn.
    ///   - label: the action's label, for the confirm button.
    ///   - agent: the harness's name, for "Restart Claude now?".
    ///   - destructive: asks even without a confirm line (palette commands with role destructive).
    ///   - previous: the value a relaunch replaces, for Undo.
    ///   - instructions: the composer line "With instructions…" leaves, when the action takes
    ///     optional free text (compact's focus).
    static func decide(
        confirm: String?, effect: HarnessSelectConfirm.Effect?, working: Bool,
        label: String = "", agent: String = "", destructive: Bool = false,
        previous: String? = nil, instructions: String? = nil
    ) -> Decision {
        if effect == .relaunch {
            guard working else {
                if let previous, !previous.isEmpty { return .runWithUndo(previous: previous) }
                return .run
            }
            return .confirm(Dialog(
                title: PrimitiveCopy.restartNow(agent), confirmLabel: PrimitiveCopy.restart,
                destructive: false, offersAfterReply: true, instructions: nil
            ))
        }
        let text = confirm?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !text.isEmpty || destructive else { return .run }
        let title = text.isEmpty ? PrimitiveCopy.asking(label) : text
        let hasInstructions = instructions?.isEmpty == false
        return .confirm(Dialog(
            title: title,
            confirmLabel: hasInstructions ? PrimitiveCopy.runNow(label) : label,
            destructive: destructive, offersAfterReply: false,
            instructions: hasInstructions ? instructions : nil
        ))
    }

    /// The composer line for "With instructions…": a command that runs `action` and takes an
    /// optional argument (`[instructions]`), such as "/compact ". nil when none does.
    static func instructionsLine(for action: String, in commands: [HarnessCompletion]) -> String? {
        let match = commands.first { command in
            command.action == action && (command.hint?.trimmingCharacters(in: .whitespaces).hasPrefix("[") ?? false)
        }
        guard let match else { return nil }
        let word = (match.insert.isEmpty ? match.title : match.insert).trimmingCharacters(in: .whitespaces)
        return word.isEmpty ? nil : word + " "
    }
}

/// A change waiting on the person's answer in the system dialog, with what each button does.
struct ActionGateRequest: Identifiable {
    let id = UUID()
    var dialog: ActionGate.Dialog
    var run: @MainActor () async -> Void
    /// "After this reply": hold the change until the turn ends.
    var afterReply: (@MainActor () -> Void)?
    /// "With instructions…": the composer gets this line.
    var instructions: (@MainActor (String) -> Void)?
    /// Cancel was chosen.
    var cancel: (@MainActor () -> Void)?
}

extension View {
    /// Asks `request`'s question in a system `confirmationDialog`: an action sheet on compact
    /// widths, a popover from the control on regular ones. Clears the request as it closes.
    func actionConfirmation(_ request: Binding<ActionGateRequest?>) -> some View {
        coordinated(
            "action.confirm", .dialog,
            isPresented: Binding(get: { request.wrappedValue != nil }, set: { if !$0 { request.wrappedValue = nil } })
        ) { view, shown in
            view.confirmationDialog(
                request.wrappedValue?.dialog.title ?? "",
                isPresented: shown,
                titleVisibility: .visible,
                presenting: request.wrappedValue
            ) { pending in
                Button(pending.dialog.confirmLabel, role: pending.dialog.destructive ? .destructive : nil) {
                    Task { await pending.run() }
                }
                if pending.dialog.offersAfterReply, let afterReply = pending.afterReply {
                    Button(PrimitiveCopy.afterThisReply) { afterReply() }
                }
                if let line = pending.dialog.instructions, let instructions = pending.instructions {
                    Button(PrimitiveCopy.withInstructions) { instructions(line) }
                }
                Button(PrimitiveCopy.cancel, role: .cancel) { pending.cancel?() }
            }
        }
    }
}
