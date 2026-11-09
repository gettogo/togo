import SwiftUI
import TogoPush

@main
struct TogoWatchApp: App {
    @WKApplicationDelegateAdaptor private var delegate: WatchAppDelegate

    var body: some Scene {
        WindowGroup {
            #if DEBUG
            if let fixture = WatchFixtures.name {
                WatchFixtures.root(fixture)
            } else {
                WatchRootView(store: delegate.store, router: delegate.router)
            }
            #else
            WatchRootView(store: delegate.store, router: delegate.router)
            #endif
        }
    }
}

/// Waiting first, Sessions a swipe below, in one stack so a pushed Reply covers both pages.
struct WatchRootView: View {
    @Bindable var store: WatchStore
    @Bindable var router: WatchRouter
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        NavigationStack(path: $router.path) {
            TabView(selection: $router.page) {
                RequestsView(store: store, focus: router.focus)
                    .tag(WatchRouter.Page.waiting)
                SessionsView(store: store)
                    .tag(WatchRouter.Page.sessions)
            }
            .tabViewStyle(.verticalPage)
            .watchRoutes(store: store)
        }
        .sheet(item: $store.openReply) { route in
            NavigationStack {
                ReplyView(model: store.replyModel(route.target), title: route.title)
            }
        }
        // A complication's tap.
        .onOpenURL { url in
            if let link = WatchLink(url: url) { open(link) }
        }
        // The control, when it ran in this process.
        .onReceive(NotificationCenter.default.publisher(for: WatchLinkInbox.posted)) { _ in takeLink() }
        .onChange(of: scenePhase) { _, state in
            guard state == .active else { return }
            store.reload()
            // The control, when it ran before the app came up.
            takeLink()
        }
    }

    private func takeLink() {
        if let link = WatchLinkInbox.take(from: UserDefaults(suiteName: SharedGroup.watch)) { open(link) }
    }

    private func open(_ link: WatchLink) {
        store.openReply = nil
        router.open(link)
    }
}
