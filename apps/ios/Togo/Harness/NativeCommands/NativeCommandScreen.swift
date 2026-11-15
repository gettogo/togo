import SwiftUI
import TogoKit

/// A command opens the same native screen system as the model picker.
/// The program supplies data and actions; shared primitives own the UI.
struct NativeCommandScreen: View {
    @Environment(\.dismiss) private var dismiss
    let store: HarnessSessionStore
    let model: ChatSurfaceInteraction

    var body: some View {
        NativeCommandNavigation(title: model.title, close: {
            store.dismissNativeCommand()
        }, terminal: {
            store.retireInteraction()
            store.layer = .terminal
            dismiss()
        }) { page.disabled(store.nativeInteractions.dismissing == model.id) }
        .modifier(NativeRuntimeConfirmation(store: store, model: model))
        .background {
            if !model.retired, let scene = model.session.scene, let generation = model.session.generation {
                SurfaceDisplayProbe(stamp: .init(generation: generation, revision: scene.revision)) { stamp in
                    Task { await model.session.displayed(revision: stamp.revision, generation: stamp.generation) }
                }
            }
        }
    }

    @ViewBuilder private var page: some View {
        if model.retired {
            // The host is dismissing. A stale control is not a new failure.
            Color.clear
        } else if model.session.notice != nil || model.session.phase == .unavailable {
            StateView(kind: .error, title: PrimitiveCopy.couldNotLoad,
                message: model.session.notice == .unconfirmed ? SurfaceCopy.unconfirmed : SurfaceCopy.unavailable)
        } else if model.controlsReady, let block = model.blocks.last {
            let context = NativeCommandContext(model: model, block: block, store: store)
            if block.node.kind == "prefs" {
                NativePreferencesPages(context: context)
            } else {
                mappedPage(context)
            }
        } else {
            StateView(kind: .loading, title: PrimitiveCopy.loading, message: "")
        }
    }

    @ViewBuilder private func mappedPage(_ context: NativeCommandContext) -> some View {
        let result = Result { try context.screen() }
        switch result {
        case .success(let screen):
            PrimitiveScreenView(screen: screen, context: context.primitive())
        case .failure(let error):
            let failure = NativeCommandFailure(error)
            StateView(kind: .error, title: PrimitiveCopy.couldNotLoad, message: failure.message)
                .task(id: failure.category) { failure.log() }
        }
    }
}

/// Shared chrome for native command content, independent of its transport.
struct NativeCommandNavigation<Content: View>: View {
    let title: String
    let close: () -> Void
    let terminal: () -> Void
    @ViewBuilder var content: () -> Content

    var body: some View {
        NavigationStack {
            content()
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button(PrimitiveCopy.cancel, systemImage: "xmark", role: .cancel, action: close)
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        Button(PrimitiveCopy.terminal, systemImage: "terminal", action: terminal)
                    }
                }
        }
        .background(Color.togoInk, ignoresSafeAreaEdges: .all)
        .presentationBackground(Color.togoInk)
    }
}

/// Uses the same adaptive presentation policy as API-backed command screens.
/// A runtime command does not insert controls or layout into the transcript.
struct NativeCommandPresentation: ViewModifier {
    @Environment(\.scenePhase) private var scenePhase
    let store: HarnessSessionStore
    let showing: Bool

    private enum Request: Identifiable {
        case surface(ChatSurfaceInteraction)
        case terminal(TerminalNativeInteraction)
        var id: UUID {
            switch self {
            case .surface(let model): model.id
            case .terminal(let model): model.id
            }
        }
    }

    private var request: Request? {
        if showing, let terminal = store.terminalInteraction, terminal.screen != nil,
           terminal.phase == .active || terminal.failure != nil {
            return .terminal(terminal)
        }
        guard showing, let model = store.interaction, !model.retired,
              model.expectsScreen || !model.blocks.isEmpty || model.session.notice != nil else { return nil }
        return .surface(model)
    }

    func body(content: Content) -> some View {
        content.harnessPresent(item: Binding(get: { request }, set: { value in
            if value == nil, let current = request {
                switch current {
                case .surface: store.dismissNativeCommand()
                case .terminal: store.dismissTerminalCommand(handbackOnFailure: true)
                }
            }
        }), as: .browse, name: "composer.nativeCommand") { request in
            switch request {
            case .surface(let model): NativeCommandScreen(store: store, model: model)
            case .terminal(let model): TerminalNativeScreen(store: store, model: model)
            }
        }
        .onChange(of: showing) { _, shown in if !shown { store.retireTerminalInteraction() } }
        .onChange(of: scenePhase) { _, phase in if phase != .active { store.retireTerminalInteraction() } }
        .task(id: LateMenu(active: showing && scenePhase == .active, request: store.readyTerminalMenu,
                          available: store.canAttachTerminalMenu, conversation: store.conversation)) {
            guard showing, scenePhase == .active, let request = store.readyTerminalMenu else { return }
            await store.attachReadyTerminalMenu(request)
        }
        .task(id: Attachment(active: showing && scenePhase == .active,
            available: store.has(HarnessCapability.terminalNative) && store.phase == .live,
            conversation: store.conversation)) {
                guard showing, scenePhase == .active, store.phase == .live else { return }
                await store.attachTerminalMenu()
            }
    }

    private struct Attachment: Hashable {
        let active: Bool
        let available: Bool
        let conversation: UUID
    }

    private struct LateMenu: Hashable {
        let active: Bool
        let request: UUID?
        let available: Bool
        let conversation: UUID
    }
}
