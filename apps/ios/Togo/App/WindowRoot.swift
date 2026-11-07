import SwiftUI
import UIKit

/// One window: its own store, made when the window first appears from what the system saved
/// for it, over the services and terminals every window shares.
struct WindowRoot: View {
    @Environment(\.scenePhase) private var scenePhase
    /// The window's tabs as JSON, kept by the system with the window.
    @SceneStorage("window") private var saved: Data?
    @State private var window = WindowID()
    @State private var store: AppStore?
    /// The one owner of this window's sheets, covers, popovers and questions.
    @State private var presentations = PresentationCoordinator()
    private let composition: AppComposition
    /// The tab this window was opened for, if any.
    private let opening: TabID?

    init(composition: AppComposition, opening: TabID?) {
        self.composition = composition
        self.opening = opening
    }

    var body: some View {
        Group {
            if let store {
                content(store)
            } else {
                Color.togoInk.ignoresSafeArea()
            }
        }
        .presentationProbe()
        .environment(\.presentationCoordinator, presentations)
        .environment(\.windowID, window)
        .onAppear(perform: makeStore)
        .onChange(of: scenePhase, initial: true) { _, phase in
            if phase == .active { composition.activate(window) }
            // The window's harness feeds stop in the background, from any screen.
            store?.harnessTabs.setActive(phase != .background)
        }
        .onOpenURL { url in
            if let link = DeepLink(url: url) { composition.open(link) }
        }
        .background { SceneEnd { [composition, window] in composition.release(window: window) } }
    }

    private func content(_ store: AppStore) -> some View {
        RootView(
            store: store, terminals: composition.terminals, onboarding: composition.onboarding,
            tutorial: composition.tutorial
        )
        .onChange(of: WindowRestoration(store), initial: true) { _, value in saved = value.data }
        // Whichever window last put a tab on screen shows its terminal.
        .onChange(of: store.workspace.selected) { _, id in show(id) }
        .onChange(of: store.workspace.secondary) { _, id in show(id) }
        .onChange(of: store.workspaceOpen) { _, open in
            if open { show(store.workspace.selected) }
        }
    }

    private func makeStore() {
        guard store == nil else { return }
        let restoring = WindowRestoration(data: saved)
        let made = composition.makeStore(window: window, restoring: restoring)
        if restoring == nil, let opening {
            made.open(hostID: opening.hostID, sessionID: opening.sessionID)
            composition.terminals.sync(window: window, with: made)
            composition.terminals.show(opening, in: window)
        }
        store = made
        // The phase may have turned active before the store existed.
        if scenePhase == .active { composition.activate(window) }
    }

    private func show(_ id: TabID?) {
        if let id { composition.terminals.show(id, in: window) }
    }
}

/// Runs `action` when the window's scene disconnects: the user closed the window, or the
/// system let it go. Views can stay off screen while the scene lives, so their disappearing
/// says nothing.
private struct SceneEnd: UIViewRepresentable {
    let action: @MainActor () -> Void

    func makeUIView(context: Context) -> Watcher {
        Watcher()
    }

    func updateUIView(_ view: Watcher, context: Context) {
        view.action = action
    }

    final class Watcher: UIView {
        var action: @MainActor () -> Void = {}
        private weak var scene: UIScene?

        override func didMoveToWindow() {
            super.didMoveToWindow()
            // A full-screen cover takes the view out of its window for a while; it is still the same scene.
            guard let current = window?.windowScene, current !== scene else { return }
            if let scene {
                NotificationCenter.default.removeObserver(self, name: UIScene.didDisconnectNotification, object: scene)
            }
            scene = current
            NotificationCenter.default.addObserver(
                self, selector: #selector(disconnected), name: UIScene.didDisconnectNotification, object: current
            )
        }

        @objc private func disconnected() {
            action()
        }
    }
}
