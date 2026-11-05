import Observation
import SwiftUI
import TogoKit
import UIKit

/// The running terminal for each open tab. It belongs to the app frame rather than the
/// workspace, so going back to the sessions list and returning finds recent tabs still
/// attached. Past `limit`, the tab used longest ago lets go of its connection and scrollback;
/// the tab stays and attaches again when it is selected.
@MainActor @Observable final class TerminalPool {
    /// Each attached terminal holds a connection and a full scrollback buffer.
    static let limit = 4

    private struct Entry {
        let handle: TerminalHandle
        let transport: TransportPreference?
        let feed: Task<Void, Never>
    }

    private var entries: [TabID: Entry] = [:]
    /// Tabs that have attached before. Attaching one of these again, after it was let go past
    /// the limit, is not the user opening it, so it may not ask a sign-in question.
    @ObservationIgnored private var attachedBefore: Set<TabID> = []
    /// Open tabs, most recently selected first.
    @ObservationIgnored private var recency: [TabID] = []

    var attached: Set<TabID> { Set(entries.keys) }

    func handle(for id: TabID) -> TerminalHandle? {
        entries[id]?.handle
    }

    /// Brings the pool in line with the store's tabs: closed tabs and the ones past the limit
    /// let go, recent tabs attach, and a tab whose transport the user changed reconnects.
    func sync(with store: AppStore) {
        let tabs = store.workspace.tabs
        let open = Set(tabs.map(\.id))
        recency.removeAll { !open.contains($0) }
        attachedBefore.formIntersection(open)
        for tab in tabs where !recency.contains(tab.id) {
            recency.append(tab.id)
        }
        if let selected = store.workspace.selected, let index = recency.firstIndex(of: selected) {
            recency.insert(recency.remove(at: index), at: 0)
        }
        let kept = Set(recency.prefix(Self.limit))
        for id in entries.keys where !kept.contains(id) {
            stop(id)
        }
        for tab in tabs where kept.contains(tab.id) {
            if let entry = entries[tab.id] {
                // Only the user picks a tab's transport.
                if entry.transport != tab.transport {
                    restart(tab, store: store)
                }
            } else {
                store.setLink(.connecting, for: tab.id)
                start(tab, store: store, userStarted: !attachedBefore.contains(tab.id))
            }
        }
    }

    /// Every attached tab on the machine reconnects, for example with an edited address.
    func restartTabs(on hostID: HostID, store: AppStore) {
        for tab in store.workspace.tabs where tab.hostID == hostID && entries[tab.id] != nil {
            restart(tab, store: store, userStarted: false)
        }
    }

    /// Try again and Sign in again are the user's own action, so their attach may ask.
    func restart(_ tab: WorkspaceTab, store: AppStore, userStarted: Bool = true) {
        stop(tab.id)
        store.setLink(.connecting, for: tab.id)
        start(tab, store: store, userStarted: userStarted)
    }

    private func start(_ tab: WorkspaceTab, store: AppStore, userStarted: Bool) {
        let id = tab.id
        attachedBefore.insert(id)
        let handle = store.services.terminal(
            hostID: tab.hostID, sessionID: tab.sessionID, transport: tab.transport, userStarted: userStarted
        )
        handle.onShortcut = { [weak store] in store?.perform($0) }
        handle.onSwipe = { [weak store] in store?.selectTab(offset: $0) }
        handle.onEdgeSwipe = { [weak store] in store?.workspaceOpen = false }
        handle.onFontSize = { [weak store] size in store?.services.settings.fontSize = size }
        handle.onCompose = { [weak store] in store?.toggleCompose(id) }
        handle.onPasteImage = { [weak store] in store?.pasteImage($0, into: id) }
        let phases = handle.phases
        let feed = Task { [weak store] in
            for await phase in phases {
                store?.setLink(phase, for: id)
            }
        }
        entries[id] = Entry(handle: handle, transport: tab.transport, feed: feed)
    }

    private func stop(_ id: TabID) {
        guard let entry = entries.removeValue(forKey: id) else { return }
        entry.feed.cancel()
        entry.handle.stop()
    }
}

/// One tab's terminal view. The pager can build a page again before it tears the old one
/// down, and a controller has one parent, so the engine joins whichever container is on screen
/// and leaves when that container is dismantled.
struct TabTerminalView: UIViewControllerRepresentable {
    let handle: TerminalHandle

    func makeUIViewController(context: Context) -> TerminalContainer {
        TerminalContainer()
    }

    func updateUIViewController(_ container: TerminalContainer, context: Context) {
        container.show(handle.engineView)
    }

    static func dismantleUIViewController(_ container: TerminalContainer, coordinator: ()) {
        container.release()
    }
}

final class TerminalContainer: UIViewController {
    private var wanted: UIViewController?

    func show(_ child: UIViewController) {
        if let wanted, wanted !== child {
            release()
        }
        wanted = child
        if viewIfLoaded?.window != nil {
            attach()
        }
    }

    func release() {
        if let wanted, wanted.parent === self {
            Self.detach(wanted)
        }
        wanted = nil
    }

    override func viewIsAppearing(_ animated: Bool) {
        super.viewIsAppearing(animated)
        attach()
    }

    private func attach() {
        guard let child = wanted, child.parent !== self else { return }
        if child.parent != nil {
            Self.detach(child)
        }
        addChild(child)
        child.view.frame = view.bounds
        child.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(child.view)
        child.didMove(toParent: self)
    }

    private static func detach(_ child: UIViewController) {
        child.willMove(toParent: nil)
        child.view.removeFromSuperview()
        child.removeFromParent()
    }
}
