import SwiftUI
import TogoKit

/// One pane of the workspace: a tab's terminal, under its own header while two panes are on
/// screen. A tap anywhere in it gives it focus, and a tab chip dropped on it opens there.
struct PaneView: View {
    @Environment(\.colorScheme) private var colorScheme
    let tab: WorkspaceTab?
    let pane: Pane
    let store: AppStore
    let terminals: TerminalPool
    let window: WindowID
    /// Two panes are on screen, so this one names its tab and has its own menu.
    let header: Bool

    @State private var targeted = false

    init(
        tab: WorkspaceTab?, pane: Pane, store: AppStore, terminals: TerminalPool, window: WindowID,
        header: Bool = false
    ) {
        self.tab = tab
        self.pane = pane
        self.store = store
        self.terminals = terminals
        self.window = window
        self.header = header
    }

    var body: some View {
        VStack(spacing: 0) {
            if header, let tab {
                PaneHeader(tab: tab, pane: pane, store: store)
            }
            pager
        }
        .overlay {
            if targeted {
                Rectangle()
                    .strokeBorder(Color.togoSnow.opacity(0.6), lineWidth: 2)
                    .allowsHitTesting(false)
            }
        }
        // motion: cause → effect — the pane outlines as the person drags a tab over it.
        .motion(.fade, value: targeted)
        .simultaneousGesture(TapGesture().onEnded { Self.tapped(pane, store: store) })
        .dropDestination(for: TabID.self) { ids, _ in
            guard Self.accept(ids, into: pane, store: store), let id = ids.first else { return false }
            // A tab dragged from another window comes to this one.
            terminals.show(id, in: window)
            return true
        } isTargeted: { targeted = $0 }
    }

    // MARK: Link

    /// While a link reconnects the terminal stays on screen, dimmed, so it is still readable.
    static func terminalOpacity(for link: LinkPhase) -> Double {
        link.isReconnecting ? 0.55 : 1
    }

    /// Dimming eases in and out slowly, and lands at once under Reduce Motion.
    static let dimRole = MotionRole.dim

    // MARK: Actions

    static func tapped(_ pane: Pane, store: AppStore) {
        store.focus(pane)
    }

    /// Puts the first dropped tab in `pane`, opening it in this window first if it is not.
    static func accept(_ ids: [TabID], into pane: Pane, store: AppStore) -> Bool {
        guard let id = ids.first else { return false }
        if store.workspace.tabs.contains(where: { $0.id == id }) {
            store.place(id, in: pane)
        } else if pane == .secondary {
            store.openBeside(id)
        } else {
            store.open(hostID: id.hostID, sessionID: id.sessionID)
            store.focus(.primary)
        }
        return true
    }

    // MARK: Pager

    /// The pane shows one tab at a time. A one-finger drag on the terminal is the terminal's
    /// (scrolling, selection, mouse drags), so the pane has no sliding pager that could stop
    /// between two sessions mid-swipe; tabs change from the tab strip, the sidebar, or the
    /// terminal's two-finger swipe, each a whole step that lands at once. The terminal sizes
    /// itself to the keyboard and the home indicator, so the pane ignores both and the grid is
    /// resized once, by the terminal.
    private var pager: some View {
        ZStack {
            if let tab {
                page(tab)
                    .id(tab.id)
                    // motion: continuity — a session switch is a cut: the next tab's terminal
                    // lands at once at its final size, so no grid is ever resized mid-transition.
                    // The tab strip's chip and the tick mark the change.
                    .transition(.identity)
            }
        }
        .ignoresSafeArea(.container, edges: .bottom)
        .ignoresSafeArea(.keyboard)
    }

    private func page(_ tab: WorkspaceTab) -> some View {
        let overlay = ConnectingOverlay.Kind(
            link: tab.link, machine: store.machine(for: tab.hostID), name: store.machineName(for: tab.hostID),
            listed: !store.services.hostsLoaded || store.services.hosts.contains { $0.id == tab.hostID }
        )
        let reconnecting = tab.link.isReconnecting
        return ZStack {
            // The theme's own background, so a dimmed or not yet drawn terminal shows its
            // canvas rather than the app's (a dark theme dimmed over light ink read as grey).
            TerminalBackdrop.resolve(store.services.settings, system: colorScheme).background
            TerminalSlot(tab: tab.id, terminals: terminals)
                .id(tab.id)
                .opacity(Self.terminalOpacity(for: tab.link))
                // motion: continuity — a terminal has one place at a time, so it lands at once and never fades.
                .transition(.identity)
            ConnectingOverlayHost(
                kind: overlay, addMachine: { store.showSheet(.addMachine) },
                editMachine: { store.showSheet(.editMachine(tab.hostID)) }
            ) { retry(tab) }
        }
        // motion: state change — the pane dims while its link reconnects.
        .motion(Self.dimRole, value: reconnecting)
        .haptic(.connect, trigger: tab.link.isLive) { $0 }
    }

    private func retry(_ tab: WorkspaceTab) {
        terminals.restart(tab, store: store)
        Task { await store.services.refresh() }
    }
}

/// The tab each pane shows, for the tick a pane plays when it settles on a tab.
struct PaneTabs: Equatable {
    var primary: TabID?
    var secondary: TabID?

    init(_ workspace: WorkspaceState) {
        primary = workspace.selected
        secondary = workspace.secondary
    }

    init(primary: TabID?, secondary: TabID?) {
        self.primary = primary
        self.secondary = secondary
    }

    /// A pane settled on another tab. Swapping the panes is one settle, and closing the second
    /// pane is none.
    static func settles(from old: PaneTabs, to new: PaneTabs) -> Bool {
        guard old != new else { return false }
        return !(new.secondary == nil && new.primary == old.primary)
    }
}

private extension LinkPhase {
    var isLive: Bool {
        if case .live = self { true } else { false }
    }

    var isReconnecting: Bool {
        if case .reconnecting = self { true } else { false }
    }
}
