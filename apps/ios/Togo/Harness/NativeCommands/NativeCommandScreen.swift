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
            dismiss()
        }, terminal: {
            store.retireInteraction()
            store.layer = .terminal
            dismiss()
        }) { page }
        .modifier(NativeRuntimeConfirmation(store: store, model: model))
        .background {
            if let scene = model.session.scene, let generation = model.session.generation {
                SurfaceDisplayProbe(stamp: .init(generation: generation, revision: scene.revision)) { stamp in
                    Task { await model.session.displayed(revision: stamp.revision, generation: stamp.generation) }
                }
            }
        }
    }

    @ViewBuilder private var page: some View {
        if model.session.notice != nil || model.session.phase == .unavailable {
            StateView(kind: .error, title: PrimitiveCopy.couldNotLoad,
                message: model.session.notice == .unconfirmed ? SurfaceCopy.unconfirmed : SurfaceCopy.unavailable)
        } else if let block = model.blocks.last {
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
    let store: HarnessSessionStore
    let showing: Bool

    private struct Request: Identifiable {
        let model: ChatSurfaceInteraction
        var id: UUID { model.id }
    }

    private var request: Request? {
        guard showing, let model = store.interaction, !model.retired,
              model.expectsScreen || !model.blocks.isEmpty || model.session.notice != nil else { return nil }
        return Request(model: model)
    }

    func body(content: Content) -> some View {
        content.harnessPresent(item: Binding(get: { request }, set: { value in
            if value == nil, request != nil { store.dismissNativeCommand() }
        }), as: .browse, name: "composer.nativeCommand") { request in
            NativeCommandScreen(store: store, model: request.model)
        }
    }
}
