import SwiftUI
import TogoKit

/// Ordinary native screens from the generic terminal reader. No harness or
/// command name selects a widget, action or key in this view.
struct TerminalNativeScreen: View {
    @Environment(\.dismiss) private var dismiss
    let store: HarnessSessionStore
    let model: TerminalNativeInteraction

    var body: some View {
        NativeCommandNavigation(title: model.screen?.title ?? "", close: {
            store.dismissTerminalCommand()
        }, terminal: {
            store.retireTerminalInteraction()
            store.layer = .terminal
            dismiss()
        }) {
            VStack(spacing: 0) {
                if model.phase == .ended, let failure = model.failure {
                    StateView(kind: .error, title: PrimitiveCopy.couldNotLoad, message: failure,
                        action: .init(SurfaceCopy.open) { Task { await store.reopenTerminalMenu() } })
                        .disabled(model.closing)
                } else if let failure = model.failure {
                    Text(verbatim: failure).font(.footnote).foregroundStyle(PrimitiveTint.fail)
                        .frame(maxWidth: .infinity, alignment: .leading).padding()
                }
                if model.phase != .ended, let screen = model.screen {
                    PrimitiveScreenView(screen: screen, context: context)
                        .id(screen.id)
                        .disabled(model.busy)
                } else if model.phase != .ended {
                    StateView(kind: .loading, title: PrimitiveCopy.loading, message: "")
                }
            }
        }
        .interactiveDismissDisabled(model.busy)
    }

    private var context: PrimitiveContext {
        let panel = model.screen?.interaction
        let sourceQuery = model.screen?.picker?.query
        var context = PrimitiveContext(run: { action, target, args in
            if action.hasPrefix("terminal.openLink.") {
                let result = model.openPanelLink(action, args: args) { url in
                    LinkCenter.shared.open(url, from: .init(hostID: store.subject.hostID,
                        sessionID: store.subject.sessionID))
                }
                // The browser is the feedback; neither “Sent” nor “Done”
                // describes authentication that is still waiting there.
                return result.result == .typed ? nil : result
            }
            let verb = action.hasPrefix("terminal.key.") ? "terminal.key" : action
            return await model.run(verb, target: target, args: args)
        }, openScreen: { _ in }, open: { _ in }, showTerminal: {
            store.retireTerminalInteraction()
            store.layer = .terminal
        })
        context.showsTerminal = false
        context.searchScreen = { query in
            let changed = sourceQuery != query && !(sourceQuery == nil && query.isEmpty)
            return try await model.read(query: changed ? query : nil, expected: panel, before: changed ? sourceQuery : nil)
        }
        context.readScreen = { try? await model.read(expected: panel) }
        context.agent = store.agentName
        context.subject = store.subject
        // The interaction ends only when the helper observes completion.
        // Applied with another screen is progress and keeps this host open.
        return context
    }
}
