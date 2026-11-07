import SwiftUI
import TogoKit

/// Draws a screen with the primitive it names, or the terminal hand-off for one this build does
/// not know. It redraws from the screen an action's result carries, says when the copy is
/// stale, and always offers the terminal in the bar. The host supplies the navigation stack.
struct PrimitiveScreenView: View {
    let screen: HarnessScreen
    let context: PrimitiveContext
    /// The screen an action's result carried, newer than `screen` until the host reads again.
    @State private var latest: HarnessScreen?
    @State private var actions = PrimitiveActions()

    var body: some View {
        let shown = PrimitiveScreenLogic.newest(screen, latest)
        primitive(shown)
            .safeAreaInset(edge: .top, spacing: 0) {
                if shown.stale { staleLine(shown) }
            }
            .navigationTitle(shown.title)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button(PrimitiveCopy.terminal, systemImage: "terminal") { context.showTerminal() }
                }
                if !shown.actions.isEmpty {
                    ToolbarItemGroup(placement: .bottomBar) {
                        ForEach(shown.actions) { ref in
                            Button(ref.label, systemImage: PrimitiveActionSymbol.of(ref), role: ref.role == .destructive ? .destructive : nil) {
                                Task { await actions.trigger(ref, context: local) }
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
            PickerPrimitive(picker: picker, context: local)
        case .settings(let settings):
            SettingsPrimitive(settings: settings, context: local)
        case .checklist(let checklist):
            ChecklistPrimitive(checklist: checklist, context: local)
        case .liveList(let list):
            LiveListPrimitive(list: list, context: local)
        case .detail(let detail):
            DetailCardPrimitive(detail: detail, context: local)
        case .confirm(let confirm):
            ConfirmPrimitive(confirm: confirm, context: local)
        case .status(let status):
            StatusPrimitive(status: status, context: local)
        case .unknown:
            TerminalHandOff(context: context)
        }
    }

    private func staleLine(_ screen: HarnessScreen) -> some View {
        Text(PrimitiveCopy.stale(screen.fetchedAt.date.formatted(.relative(presentation: .named))))
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Layout.margin)
            .padding(.vertical, 6)
            .background(Color.togoInk)
            .accessibilityAddTraits(.updatesFrequently)
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
