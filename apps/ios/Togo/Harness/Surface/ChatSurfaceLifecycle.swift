import SwiftUI
import TogoKit

/// A chat pane owns the transport. A lazy row only renders its controls and
/// grants receipts for actual visible draws; scrolling cannot end the lease.
struct ChatSurfaceLifecycle: ViewModifier {
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.loopsPaused) private var loopsPaused
    let store: HarnessSessionStore

    private var options: SurfaceOptions {
        SurfaceOptions(kinds: SurfaceNodeView.kinds.sorted(), cols: 80, dark: colorScheme == .dark, reduceMotion: reduceMotion)
    }

    func body(content: Content) -> some View {
        content
            .task(id: Run(id: store.interaction?.id, active: scenePhase == .active && !loopsPaused, options: options)) {
                guard scenePhase == .active, !loopsPaused, let model = store.interaction else { return }
                await model.run(options: options)
            }
            .onChange(of: store.interaction?.session.generation) { _, generation in
                store.surfaceDrafts.bind(generation)
            }
            .onChange(of: store.interaction?.session.scene) { _, scene in
                guard let model = store.interaction, model.session.scene == scene else { return }
                if let scene { store.surfaceDrafts.reconcile(scene) }
                model.changed()
                store.updateNativeQuestion()
            }
            .onChange(of: store.interaction?.session.pending == nil) { store.interaction?.changed() }
    }

    private struct Run: Hashable { var id: UUID?; var active: Bool; var options: SurfaceOptions }
}
