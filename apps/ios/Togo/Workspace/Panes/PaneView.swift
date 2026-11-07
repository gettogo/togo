import SwiftUI
import TogoKit

/// One pane of the workspace: a tab's terminal that pages through the open tabs with a
/// horizontal swipe, under its own header while two panes are on screen. A tap anywhere in it
/// gives it focus, and a tab chip dropped on it opens there.
struct PaneView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
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

    /// The tabs this pane pages through; the other pane's tab is not among them.
    private var pages: [WorkspaceTab] {
        store.pages(for: pane)
    }

    /// The terminal sizes itself to the keyboard and the home indicator, so the pager ignores
    /// both and the grid is resized once, by the terminal.
    @ViewBuilder private var pager: some View {
        Group {
            if reduceMotion {
                fadingPager
            } else {
                TabView(selection: selection) {
                    ForEach(pages) { tab in
                        page(tab)
                            .tag(Optional(tab.id))
                    }
                }
                .tabViewStyle(.page(indexDisplayMode: .never))
            }
        }
        .ignoresSafeArea(.container, edges: .bottom)
        .ignoresSafeArea(.keyboard)
    }

    /// Under Reduce Motion a horizontal swipe changes tabs without sliding: the tab on screen
    /// cross-fades into the next one.
    private var fadingPager: some View {
        ZStack {
            if let tab {
                page(tab)
                    .id(tab.id)
                    .transition(.opacity)
            }
        }
        .animation(Motion.animation(.tabSettle, reduce: true), value: tab?.id)
        .simultaneousGesture(
            DragGesture(minimumDistance: 24).onEnded { drag in
                let width = drag.translation.width
                guard abs(width) > 60, abs(width) > abs(drag.translation.height) * 2 else { return }
                step(width < 0 ? 1 : -1)
            }
        )
    }

    private func page(_ tab: WorkspaceTab) -> some View {
        let overlay = ConnectingOverlay.Kind(
            link: tab.link, machine: store.machine(for: tab.hostID), name: store.machineName(for: tab.hostID),
            listed: !store.services.hostsLoaded || store.services.hosts.contains { $0.id == tab.hostID }
        )
        let isShown = tab.id == self.tab?.id
        let reconnecting = tab.link.isReconnecting
        return ZStack {
            Color.togoInk
            if header && !isShown {
                // Beside another pane, a page waiting off screen draws its name only. Its
                // terminal may be on screen in the other pane's pager, and a terminal has one
                // place at a time.
                PagePlaceholder(label: store.chipLabel(for: tab))
            } else {
                TerminalSlot(tab: tab.id, terminals: terminals)
                    .id(tab.id)
                    .opacity(Self.terminalOpacity(for: tab.link))
            }
            ConnectingOverlayHost(kind: overlay, addMachine: { store.showSheet(.addMachine) }) { retry(tab) }
        }
        .motion(Self.dimRole, value: reconnecting)
        .haptic(.connect, trigger: tab.link.isLive) { $0 && isShown }
    }

    private var selection: Binding<TabID?> {
        Binding {
            tab?.id
        } set: { id in
            // The pager also writes back when its pages change under it, as on a fold or a swap;
            // only a move to another of this pane's pages is the user paging.
            guard let id, id != tab?.id, pages.contains(where: { $0.id == id }) else { return }
            show(id)
        }
    }

    private func show(_ id: TabID) {
        store.workspace.ended = nil
        if pane == .primary {
            store.select(id)
        } else {
            store.place(id, in: .secondary)
        }
    }

    /// Moves along this pane's tabs and stops at either end, as the sliding pager does.
    private func step(_ offset: Int) {
        guard let id = tab?.id else { return }
        store.step(offset, from: id)
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

/// A page beside the one on screen, while its terminal may be showing in the other pane.
private struct PagePlaceholder: View {
    let label: (bib: String?, name: String)

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            if let bib = label.bib {
                Text(verbatim: bib)
                    .font(.system(.body, design: .monospaced))
                    .monospacedDigit()
            }
            Text(verbatim: label.name)
                .font(.body)
        }
        .multilineTextAlignment(.center)
        .foregroundStyle(Color.togoMute)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.togoInk)
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
