import SwiftUI
import TogoKit

/// Draws a screen with the primitive it names, or the terminal hand-off for one this build does
/// not know. It redraws from the screen an action's result carries, says in the bar's subtitle
/// when the copy is stale ("Updated 2 min ago"), and always offers the terminal in the bar. A
/// Confirm with no questions is never a page: it asks in the system dialog. The host supplies
/// the navigation stack.
struct PrimitiveScreenView: View {
    let screen: HarnessScreen
    let context: PrimitiveContext
    /// The screen an action's result carried, newer than `screen` until the host reads again.
    @State private var latest: HarnessScreen?
    @State private var actions = PrimitiveActions()

    var body: some View {
        let shown = PrimitiveScreenLogic.newest(screen, latest)
        primitive(shown)
            .navigationTitle(shown.title)
            // A background state: instant, in the bar, not a band over the content.
            .navigationSubtitle(PrimitiveScreenLogic.subtitle(shown) ?? "")
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button(PrimitiveCopy.terminal, systemImage: "terminal") { context.showTerminal() }
                }
                if !shown.actions.isEmpty {
                    ToolbarItemGroup(placement: .bottomBar) {
                        ForEach(shown.actions) { ref in
                            Button(role: ref.role == .destructive ? .destructive : nil) {
                                Task { await actions.trigger(ref, place: .label, context: local) }
                            } label: {
                                Label {
                                    OutcomeLabel(title: ref.label, outcome: actions.outcome(ref.id), busy: actions.isRunning(ref.id))
                                } icon: {
                                    Image(systemName: PrimitiveActionSymbol.of(ref))
                                }
                            }
                            .disabled(actions.isRunning(ref.id))
                        }
                    }
                }
            }
            .primitiveActions(actions, context: local)
    }

    /// The host's context, with each result's screen kept so the primitive redraws from it.
    private var local: PrimitiveContext {
        var context = self.context
        let outer = self.context.run
        let id = screen.id
        context.run = { action, target, args in
            let result = await outer(action, target, args)
            if let next = result?.screen, next.id == id { latest = next }
            return result
        }
        return context
    }

    @ViewBuilder private func primitive(_ screen: HarnessScreen) -> some View {
        switch PrimitiveScreenLogic.payload(screen) {
        case .picker(let picker):
            PickerPrimitive(picker: picker, title: screen.title, context: local)
        case .settings(let settings):
            SettingsPrimitive(settings: settings, context: local)
        case .checklist(let checklist):
            ChecklistPrimitive(checklist: checklist, context: local)
        case .liveList(let list):
            LiveListPrimitive(list: list, context: local)
        case .detail(let detail):
            DetailCardPrimitive(detail: detail, context: local)
        case .confirm(let confirm) where confirm.questions.isEmpty:
            ConfirmDialogScreen(screen: screen, confirm: confirm, context: local)
        case .confirm(let confirm):
            ConfirmPrimitive(confirm: confirm, context: local)
        case .status(let status):
            StatusPrimitive(status: status, context: local)
        case .unknown:
            TerminalHandOff(context: context)
        }
    }
}

enum PrimitiveScreenLogic {
    /// The screen's one filled payload, by its primitive. A primitive this build does not know,
    /// or a payload missing for the one named, is `.unknown`.
    enum Payload {
        case picker(HarnessScreen.Picker)
        case settings(HarnessScreen.Settings)
        case checklist(HarnessScreen.Checklist)
        case liveList(HarnessScreen.LiveList)
        case detail(HarnessScreen.Detail)
        case confirm(HarnessScreen.Confirm)
        case status(HarnessStatus)
        case unknown
    }

    static func payload(_ screen: HarnessScreen) -> Payload {
        switch screen.primitive {
        case .picker: screen.picker.map(Payload.picker) ?? .unknown
        case .settings: screen.settings.map(Payload.settings) ?? .unknown
        case .checklist: screen.checklist.map(Payload.checklist) ?? .unknown
        case .liveList: screen.liveList.map(Payload.liveList) ?? .unknown
        case .detail: screen.detail.map(Payload.detail) ?? .unknown
        case .confirm: screen.confirm.map(Payload.confirm) ?? .unknown
        case .status: screen.status.map(Payload.status) ?? .unknown
        default: .unknown
        }
    }

    /// The bar's subtitle: "Updated 2 min ago" for a stale copy, nil when it is fresh.
    static func subtitle(_ screen: HarnessScreen) -> String? {
        guard screen.stale else { return nil }
        return PrimitiveCopy.updated(screen.fetchedAt.date.formatted(.relative(presentation: .named, unitsStyle: .abbreviated)))
    }

    /// The dialog a Confirm with no questions asks with: its message, its submit as the button.
    static func dialog(_ screen: HarnessScreen, _ confirm: HarnessScreen.Confirm) -> ActionGate.Dialog {
        let message = confirm.message?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return ActionGate.Dialog(
            title: message.isEmpty ? PrimitiveCopy.asking(screen.title) : message,
            confirmLabel: screen.title.isEmpty ? PrimitiveCopy.confirm : screen.title,
            destructive: confirm.destructive, offersAfterReply: false, instructions: nil
        )
    }

    /// The later of the host's screen and the one an action returned, by when each was read.
    static func newest(_ host: HarnessScreen, _ fromAction: HarnessScreen?) -> HarnessScreen {
        guard let fromAction, fromAction.id == host.id, fromAction.fetchedAt > host.fetchedAt else { return host }
        return fromAction
    }
}

/// What a screen shows when this build cannot draw it: the terminal, one tap away.
struct TerminalHandOff: View {
    let context: PrimitiveContext
    var title = PrimitiveCopy.unknownTitle
    var message = PrimitiveCopy.unknownMessage

    var body: some View {
        StateView(kind: .empty, title: title, message: message, action: .init(PrimitiveCopy.openTerminal) {
            context.showTerminal()
        })
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.togoInk)
    }
}

/// A Confirm with no questions: never a page with one button. It asks in the system dialog as
/// it opens; the answer closes the host (the chat says what happened), and a host that keeps
/// its pages shows the outcome where the question was.
private struct ConfirmDialogScreen: View {
    let screen: HarnessScreen
    let confirm: HarnessScreen.Confirm
    let context: PrimitiveContext
    @State private var actions = PrimitiveActions()
    @State private var asked = false

    var body: some View {
        Group {
            if let outcome = actions.outcome(confirm.submit) {
                StateView(kind: outcome.isFailure ? .error : .empty, title: outcome.title, message: outcome.detail ?? "")
            } else if asked, actions.asking == nil, !actions.isBusy {
                // Dismissed without an answer: the question, quietly; Back or Done leaves.
                StateView(kind: .empty, title: PrimitiveScreenLogic.dialog(screen, confirm).title, message: "")
            } else {
                Color.togoInk
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.togoInk)
        .actionConfirmation($actions.asking)
        .onAppear {
            guard !asked else { return }
            asked = true
            let submit = confirm.submit
            actions.asking = ActionGateRequest(
                dialog: PrimitiveScreenLogic.dialog(screen, confirm),
                run: {
                    let result = await actions.perform(submit, place: .control, context: context)
                    if PickerModel.closes(result) { context.settled?() }
                },
                // Cancel: nothing to stay for.
                cancel: { context.settled?() }
            )
        }
    }
}
