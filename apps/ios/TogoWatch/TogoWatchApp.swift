import SwiftUI

@main
struct TogoWatchApp: App {
    @WKApplicationDelegateAdaptor private var delegate: WatchAppDelegate

    var body: some Scene {
        WindowGroup {
            #if DEBUG
            if let fixture = WatchFixtures.name {
                WatchFixtures.root(fixture)
            } else {
                WatchRootView(store: delegate.store)
            }
            #else
            WatchRootView(store: delegate.store)
            #endif
        }
    }
}

/// Waiting first, Sessions a swipe below.
struct WatchRootView: View {
    let store: WatchStore
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        TabView {
            NavigationStack {
                RequestsView(store: store)
            }
            NavigationStack {
                SessionsView(store: store)
            }
        }
        .tabViewStyle(.verticalPage)
        .onChange(of: scenePhase) { _, state in
            if state == .active { store.reload() }
        }
    }
}
