import Observation
import SwiftUI
import TogoKit
import UIKit

/// The running terminal for each open tab, shared by every window. It belongs to the app
/// rather than a workspace, so going back to the sessions list and returning finds recent tabs
/// still attached, and a tab listed in two windows has one connection. The terminal view is in
/// the window that last asked for it; the others say where it is. Tabs on screen in any window
/// stay attached; past `limit` the others go, the one shown longest ago first, and attach again
/// when they are selected.
@MainActor @Observable final class TerminalPool {
    /// Each attached terminal holds a connection and a full scrollback buffer. This counts every
    /// attached tab; tabs on screen stay attached even past it, and the rest fill what is left.
    static let limit = 4

    private struct Entry {
        let handle: TerminalHandle
        let transport: TransportPreference?
        let feed: Task<Void, Never>
    }

    private var entries: [TabID: Entry] = [:]
    /// The window each attached terminal is shown in.
    private var hosts: [TabID: WindowID] = [:]
    /// Each window's store, in the order the windows arrived.
    @ObservationIgnored private var windows: [(id: WindowID, store: AppStore)] = []
    /// What each window had selected when it last synced, so a new selection counts as use.
    @ObservationIgnored private var selections: [WindowID: TabID] = [:]
    /// Where each terminal's link last was, for a window that lists the tab later.
    @ObservationIgnored private var phases: [TabID: LinkPhase] = [:]
    /// The transport every window has for each tab: the one last picked in any of them.
    @ObservationIgnored private var transports: [TabID: TransportPreference?] = [:]
    /// Each window's transports as of its last sync, so a new pick there can be told apart.
    @ObservationIgnored private var seenTransports: [WindowID: [TabID: TransportPreference?]] = [:]
    /// Tabs that have attached before. Attaching one of these again, after it was let go past
    /// the limit, is not the user opening it, so it may not ask a sign-in question.
    @ObservationIgnored private var attachedBefore: Set<TabID> = []
    /// Open tabs, most recently shown first.
    @ObservationIgnored private var recency: [TabID] = []

    var attached: Set<TabID> { Set(entries.keys) }

    func handle(for id: TabID) -> TerminalHandle? {
        entries[id]?.handle
    }

    /// The window showing the tab's terminal, nil when it is not attached.
    func host(of id: TabID) -> WindowID? {
        hosts[id]
    }

    /// The store of a window, for what a terminal there needs from its tab: the layer over it,
    /// a jump to a line, the machine to read history from.
    func appStore(in window: WindowID) -> AppStore? {
        store(of: window)
    }

    /// True when the tab's terminal should be on screen in `window`. A terminal no window
    /// holds yet is drawn only where a single window lists it, never in two at once.
    func shows(_ id: TabID, in window: WindowID) -> Bool {
        if let host = hosts[id] { return host == window }
        return windows.filter { $0.store.workspace.tabs.contains { $0.id == id } }.count <= 1
    }

    /// For a view with one window, such as a preview.
    func sync(with store: AppStore) {
        sync(window: .single, with: store)
    }

    /// Brings the pool in line with the window's tabs: tabs no window lists let go, recent tabs
    /// attach, and a tab whose transport the user changed reconnects.
    func sync(window: WindowID, with store: AppStore) {
        if let index = windows.firstIndex(where: { $0.id == window }) {
            windows[index].store = store
        } else {
            windows.append((window, store))
        }
        if let selected = store.workspace.selected, selections[window] != selected {
            selections[window] = selected
            moveToFront(selected)
        }
        settle()
    }

    /// The window's scene went away. Its tabs stay attached while another window lists them.
    func release(window: WindowID) {
        windows.removeAll { $0.id == window }
        selections[window] = nil
        seenTransports[window] = nil
        settle()
    }

    /// Shows the tab's terminal in `window`; wherever it was says it is elsewhere.
    func show(_ id: TabID, in window: WindowID) {
        guard let store = store(of: window), store.workspace.tabs.contains(where: { $0.id == id }) else { return }
        hosts[id] = window
        moveToFront(id)
        settle()
    }

    /// Zen in `window` hides the Compose and Dictate keys on the terminals that window shows,
    /// and on no other. A terminal that moves to another window takes that window's setting.
    func zenChanged(_ on: Bool, in window: WindowID) {
        for (id, entry) in entries where hosts[id] == window {
            entry.handle.hidesComposeKeys = on
        }
    }

    /// Every attached tab on the machine reconnects, for example with an edited address.
    func restartTabs(on hostID: HostID) {
        for id in entries.keys where id.hostID == hostID {
            if let (tab, store) = listing(id) {
                restart(tab, store: store, userStarted: false)
            }
        }
    }

    /// Try again and Sign in again are the user's own action, so their attach may ask. The
    /// terminal comes back in the window that showed it, wherever the restart came from.
    func restart(_ tab: WorkspaceTab, store: AppStore, userStarted: Bool = true) {
        stop(tab.id, keepHost: true)
        start(tab, services: store.services, userStarted: userStarted)
        bind(tab.id)
    }

    // MARK: Private

    private func settle() {
        agreeOnTransports()
        var tabs: [TabID: WorkspaceTab] = [:]
        for window in windows {
            for tab in window.store.workspace.tabs where tabs[tab.id] == nil {
                tabs[tab.id] = tab
            }
        }
        recency.removeAll { tabs[$0] == nil }
        attachedBefore.formIntersection(tabs.keys)
        phases = phases.filter { tabs[$0.key] != nil }
        for window in windows {
            for tab in window.store.workspace.tabs where !recency.contains(tab.id) {
                recency.append(tab.id)
            }
        }
        let pinned = Set(windows.flatMap { [$0.store.workspace.selected, $0.store.workspace.secondary].compactMap { $0 } })
        let rest = recency.filter { !pinned.contains($0) }.prefix(max(0, Self.limit - pinned.count))
        let kept = pinned.union(rest)
        for id in entries.keys where !kept.contains(id) {
            stop(id)
        }
        guard let services = windows.first?.store.services else { return }
        for id in recency where kept.contains(id) {
            guard let tab = listing(id)?.tab else { continue }
            if let entry = entries[id] {
                // Only the user picks a tab's transport.
                if entry.transport != tab.transport {
                    restart(tab, store: listing(id)?.store ?? windows[0].store, userStarted: true)
                }
            } else {
                start(tab, services: services, userStarted: !attachedBefore.contains(id))
            }
        }
        placeTerminals(pinned: pinned)
        for window in windows {
            for tab in window.store.workspace.tabs {
                if let phase = phases[tab.id] { window.store.setLink(phase, for: tab.id) }
            }
        }
    }

    /// A transport picked in one window applies to the tab in every window, so moving its
    /// terminal between windows never reconnects it.
    private func agreeOnTransports() {
        for window in windows {
            let seen = seenTransports[window.id] ?? [:]
            for tab in window.store.workspace.tabs {
                if let last = seen[tab.id] {
                    if last != tab.transport { transports[tab.id] = tab.transport }
                } else if transports[tab.id] == nil {
                    transports[tab.id] = tab.transport
                }
            }
        }
        let listed = Set(windows.flatMap { $0.store.workspace.tabs.map(\.id) })
        transports = transports.filter { listed.contains($0.key) }
        for window in windows {
            let store = window.store
            for index in store.workspace.tabs.indices {
                let id = store.workspace.tabs[index].id
                if let agreed = transports[id], store.workspace.tabs[index].transport != agreed {
                    store.workspace.tabs[index].transport = agreed
                }
            }
            seenTransports[window.id] = Dictionary(
                store.workspace.tabs.map { ($0.id, $0.transport) }, uniquingKeysWith: { first, _ in first }
            )
        }
    }

    /// Each attached terminal stays where it is while that window lists it. One that has no
    /// window goes to the first window with it on screen, else the first that lists it.
    private func placeTerminals(pinned: Set<TabID>) {
        for id in entries.keys {
            if let window = hosts[id], store(of: window)?.workspace.tabs.contains(where: { $0.id == id }) == true {
                bind(id)
                continue
            }
            let onScreen = windows.first { [$0.store.workspace.selected, $0.store.workspace.secondary].contains(id) }
            let listing = windows.first { $0.store.workspace.tabs.contains { $0.id == id } }
            hosts[id] = (onScreen ?? listing)?.id
            bind(id)
        }
        for id in hosts.keys where entries[id] == nil {
            hosts[id] = nil
        }
    }

    /// The tab as the window showing it has it, and that window's store; else the first that lists it.
    private func listing(_ id: TabID) -> (tab: WorkspaceTab, store: AppStore)? {
        let shown = hosts[id].flatMap(store(of:)).flatMap { store in
            store.workspace.tabs.first { $0.id == id }.map { ($0, store) }
        }
        if let shown { return shown }
        for window in windows {
            if let tab = window.store.workspace.tabs.first(where: { $0.id == id }) { return (tab, window.store) }
        }
        return nil
    }

    private func store(of window: WindowID) -> AppStore? {
        windows.first { $0.id == window }?.store
    }

    private func moveToFront(_ id: TabID) {
        recency.removeAll { $0 == id }
        recency.insert(id, at: 0)
    }

    private func start(_ tab: WorkspaceTab, services: any AppServices, userStarted: Bool) {
        let id = tab.id
        attachedBefore.insert(id)
        phases[id] = .connecting
        for window in windows {
            window.store.setLink(.connecting, for: id)
        }
        let handle = services.terminal(
            hostID: tab.hostID, sessionID: tab.sessionID, transport: tab.transport, userStarted: userStarted
        )
        handle.onFontSize = { [weak services] size in services?.settings.fontSize = size }
        let origin = LinkCenter.Origin(hostID: tab.hostID, sessionID: tab.sessionID)
        handle.onOpenURL = { LinkCenter.shared.open($0, from: origin) }
        handle.onShowLinks = { [weak handle] in LinkCenter.shared.showLinks(handle?.links?() ?? [], from: origin) }
        let stream = handle.phases
        let feed = Task { [weak self] in
            for await phase in stream {
                self?.report(phase, for: id)
            }
        }
        entries[id] = Entry(handle: handle, transport: tab.transport, feed: feed)
    }

    private func report(_ phase: LinkPhase, for id: TabID) {
        guard entries[id] != nil else { return }
        phases[id] = phase
        for window in windows {
            window.store.setLink(phase, for: id)
        }
    }

    /// Gestures and keys on a terminal act on the window that shows it.
    private func bind(_ id: TabID) {
        guard let handle = entries[id]?.handle,
              let store = hosts[id].flatMap(store(of:)) ?? listing(id)?.store else { return }
        handle.onShortcut = { [weak store] in store?.perform($0) }
        handle.onSwipe = { [weak store] in store?.step($0, from: id) }
        handle.onEdgeSwipe = { [weak store] in store?.edgeSwipe() }
        handle.onCompose = { [weak store] in store?.toggleCompose(id) }
        handle.onDictate = { [weak store] in store?.dictate(id) }
        handle.hidesComposeKeys = store.zen
        handle.onPasteImage = { [weak store] in store?.pasteImage($0, into: id) }
    }

    private func stop(_ id: TabID, keepHost: Bool = false) {
        guard let entry = entries.removeValue(forKey: id) else { return }
        if !keepHost { hosts[id] = nil }
        phases[id] = nil
        entry.feed.cancel()
        entry.handle.stop()
    }
}

/// A tab's terminal in this window, or, while another window shows it, a note saying so with
/// a way to bring it here.
struct TerminalSlot: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.windowID) private var window
    let tab: TabID
    let terminals: TerminalPool
    /// "Show in terminal" landed on a line the phone does not hold.
    @State private var capture: CaptureRequest?

    var body: some View {
        if let handle = terminals.handle(for: tab) {
            if terminals.shows(tab, in: window) {
                let store = terminals.appStore(in: window)
                let harness = store?.harness(for: tab)
                let covered = harness?.shownLayer == .chat
                // The size SwiftUI gives the slot, so every change to it fits the grid again.
                GeometryReader { proxy in
                    TabTerminalView(
                        handle: handle, fit: proxy.size, covered: covered,
                        jump: covered ? nil : harness?.pendingJump.flatMap { $0.sessionID == tab.sessionID ? $0 : nil },
                        onJump: { jump, landed in
                            if harness?.pendingJump == jump { harness?.pendingJump = nil }
                            if !landed {
                                capture = CaptureRequest(hostID: tab.hostID, sessionID: tab.sessionID, needle: jump.needle)
                            }
                        }
                    )
                }
                // The terminal takes its frame in one step whatever moves around it, such as the
                // sidebar or a fold, so its grid is resized once rather than on every frame.
                .transaction { $0.animation = nil }
                // Under the chat the terminal is out of reach: VoiceOver passes it by.
                .accessibilityHidden(covered)
                .sheet(item: $capture) { request in
                    if let services = store?.services {
                        CaptureSheet(request: request, services: services)
                    }
                }
            } else {
                ElsewhereNotice {
                    // The notice fades from over the terminal arriving under it.
                    withAnimation(Motion.animation(.fade, reduce: reduceMotion)) { terminals.show(tab, in: window) }
                }
                .transition(.opacity)
            }
        }
    }
}

/// One tab's terminal view. The pager can build a page again before it tears the old one
/// down, and a controller has one parent, so the engine joins whichever container is on screen
/// and leaves when that container is dismantled.
struct TabTerminalView: UIViewControllerRepresentable {
    let handle: TerminalHandle
    /// The space the terminal has; a new one fits its grid again.
    var fit: CGSize = .zero
    /// The chat covers the terminal: it takes no touches, no keyboard and no VoiceOver focus.
    var covered = false
    /// A line to scroll to and mark, once.
    var jump: TerminalJump?
    /// The jump was handled: `landed` false when the phone does not hold the line.
    var onJump: (TerminalJump, _ landed: Bool) -> Void = { _, _ in }

    final class Coordinator {
        var handled: TerminalJump?
    }

    func makeCoordinator() -> Coordinator {
        Coordinator()
    }

    func makeUIViewController(context: Context) -> TerminalContainer {
        TerminalContainer()
    }

    func updateUIViewController(_ container: TerminalContainer, context: Context) {
        container.show(handle.engineView)
        container.setCovered(covered)
        handle.engine?.isCovered = covered
        container.refit(for: fit)
        guard let jump, context.coordinator.handled != jump else {
            if jump == nil { context.coordinator.handled = nil }
            return
        }
        context.coordinator.handled = jump
        let engine = handle.engine
        let onJump = self.onJump
        // After this pass, so the terminal is laid out and on screen as the chat fades away.
        Task { @MainActor [weak container] in
            guard let container, let engine else {
                onJump(jump, false)
                return
            }
            onJump(jump, TerminalJumper.perform(jump, engine: engine, container: container))
        }
    }

    static func dismantleUIViewController(_ container: TerminalContainer, coordinator: Coordinator) {
        container.release()
    }
}
