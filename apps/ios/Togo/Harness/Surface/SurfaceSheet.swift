import SwiftUI
import TogoKit

struct SurfacePresenter: ViewModifier {
    let store: HarnessSessionStore?
    let visible: Bool

    func body(content: Content) -> some View {
        if let store {
            content
                .presentSheet("surface.sheet", isPresented: Binding(
                    get: { store.showsSurface && visible }, set: { store.showsSurface = $0 }
                )) { SurfaceSheet(store: store) }
                .onChange(of: visible) { _, shown in if !shown { store.showsSurface = false } }
                .onChange(of: store.conversation) { store.showsSurface = false }
        } else { content }
    }
}

struct SurfaceSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: HarnessSessionStore
    @State private var session: SurfaceSession
    private var drafts: SurfaceDrafts { store.surfaceDrafts }

    init(store: HarnessSessionStore) {
        self.store = store
        _session = State(initialValue: SurfaceSession(id: store.subject.sessionID) {
            try await store.services.surfaceConnection(store.subject)
        })
    }

    private var options: SurfaceOptions {
        SurfaceOptions(kinds: SurfaceNodeView.kinds.sorted(), cols: 80, dark: colorScheme == .dark, reduceMotion: reduceMotion)
    }

    var body: some View {
        NavigationStack {
            Group {
                switch session.phase {
                case .loading: ProgressView(SurfaceCopy.loading)
                case .unavailable: unavailable
                case .suspended: ProgressView(SurfaceCopy.loading)
                case .active: scene
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            .navigationTitle(SurfaceCopy.interface)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(SurfaceCopy.close) { dismiss() }
                }
                ToolbarItem(placement: .primaryAction) {
                    Button(SurfaceCopy.terminal, systemImage: "terminal", action: terminal)
                }
            }
        }
        .togoControls()
        .presentationDetents([.large])
        .task(id: Run(active: scenePhase == .active, options: options)) {
            guard scenePhase == .active else { return }
            await session.run(options: options)
        }
        .onChange(of: session.generation) { _, generation in drafts.bind(generation) }
        .onChange(of: session.scene) { _, next in if let next { drafts.reconcile(next) } }
    }

    private struct Run: Hashable {
        var active: Bool
        var options: SurfaceOptions
    }

    @ViewBuilder private var scene: some View {
        if let scene = session.scene, let generation = session.generation {
            VStack(spacing: 0) {
                if let notice = session.notice {
                    Text(notice == .unconfirmed ? SurfaceCopy.unconfirmed : SurfaceCopy.unavailable)
                        .font(.footnote).foregroundStyle(PrimitiveTint.warn).padding()
                        .accessibilityIdentifier("surface.notice")
                } else if session.pending != nil {
                    ProgressView(SurfaceCopy.waiting).padding()
                }
                ScrollView {
                    VStack(alignment: .leading, spacing: 20) {
                        if scene.views.allSatisfy(\.closed) { Text(SurfaceCopy.empty) }
                        ForEach(scene.views.filter { !$0.closed }) { view in
                            SurfaceNodeView(node: SurfaceValues.presentedNode(view.document.root),
                                context: SurfaceContext(session: session, drafts: drafts, view: view, generation: generation))
                                .disabled(!view.listen || view.document.suspended)
                                .id("\(generation)/\(view.id)/\(view.instance)")
                        }
                    }
                    .padding()
                }
            }
            .background(SurfaceDisplayProbe(stamp: .init(generation: generation, revision: scene.revision)) { stamp in
                Task { await session.displayed(revision: stamp.revision, generation: stamp.generation) }
            })
        } else { unavailable }
    }

    private var unavailable: some View {
        ContentUnavailableView {
            Label(SurfaceCopy.interface, systemImage: "terminal")
        } description: {
            Text(SurfaceCopy.unavailable)
        } actions: {
            Button(SurfaceCopy.terminal, action: terminal)
        }
    }

    private func terminal() {
        store.layer = .terminal
        dismiss()
    }
}
