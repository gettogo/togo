import SwiftUI
import TogoKit

/// What a window shows at its size.
enum ShellLayout: Equatable, Sendable {
    /// Compact: the sessions list in a stack, the workspace as a cover over it.
    case stack(coverShown: Bool)
    /// Regular: the session sidebar beside the workspace, the board or the empty detail.
    case split(detail: Detail)

    enum Detail: Equatable, Sendable {
        case workspace, board, empty
    }

    /// Reads window size and size class only. The window's state is the same either way, so a
    /// Stage Manager resize, Split View or fold only changes where it is shown. In a compact
    /// window the board takes the list's place under the cover.
    static func resolve(
        size: CGSize, sizeClass: UserInterfaceSizeClass?, workspaceOpen: Bool, hasTabs: Bool, boardShown: Bool = false
    ) -> ShellLayout {
        guard PaneLayout.showsSidebar(size: size, sizeClass: sizeClass) else {
            return .stack(coverShown: workspaceOpen)
        }
        if boardShown { return .split(detail: .board) }
        return .split(detail: workspaceOpen && hasTabs ? .workspace : .empty)
    }

    var isSplit: Bool {
        if case .split = self { true } else { false }
    }
}

/// Whether the sidebar shows, and the ⌘K requests for its search.
struct SidebarState: Equatable, Sendable {
    /// The user's last choice; nil follows `prefersHidden`.
    var hidden: Bool?
    /// Two panes in a window too narrow for them and a sidebar hide it by default.
    var prefersHidden = false
    /// Bumped by ⌘K beside the sidebar; the sidebar's search takes focus when it changes.
    var searchRequests = 0

    var isHidden: Bool { hidden ?? prefersHidden }

    mutating func toggle() {
        hidden = !isHidden
    }
}

extension AppStore {
    /// Set by the window as it crosses between compact and regular. Tabs, selection, panes and
    /// `workspaceOpen` stay as they are.
    func setWorkspaceInline(_ inline: Bool) {
        guard workspaceInline != inline else { return }
        workspaceInline = inline
    }

    /// A change in the default hands the choice back to it, so opening a second pane in a narrow
    /// window hides the sidebar even after the user showed it once.
    func updateSidebarDefault(size: CGSize) {
        let prefers = PaneLayout.prefersSidebarHidden(size: size, hasSecondary: workspace.secondary != nil)
        guard prefers != sidebar.prefersHidden else { return }
        sidebar.prefersHidden = prefers
        sidebar.hidden = nil
    }
}

/// The window below the first-run screen: a navigation stack with the workspace as a cover in
/// a compact window, the session sidebar beside the workspace in a regular one.
struct AdaptiveRoot<Destination: View>: View {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.fold) private var fold
    @Bindable var store: AppStore
    let terminals: TerminalPool
    /// Rows are zoom sources in this namespace, so the cover grows out of the tapped row.
    var zoom: Namespace.ID?
    /// A fixed clock for snapshots; nil follows the time of day.
    var now: Date?
    /// What the board runs on; nil for the device's own.
    var board: BoardServices?
    @ViewBuilder let destination: (AppRoute) -> Destination
    /// The last tab on screen, so closing the cover shrinks into its row instead of nowhere.
    @State private var zoomSource: TabID?
    #if DEBUG
    @State private var debugToolsShown = false
    #endif

    var body: some View {
        GeometryReader { proxy in
            let layout = ShellLayout.resolve(
                size: proxy.size, sizeClass: sizeClass, workspaceOpen: store.workspaceOpen,
                hasTabs: !store.workspace.tabs.isEmpty, boardShown: store.boardShown
            )
            Group {
                switch layout {
                case .stack: stack
                case .split(let detail): split(detail)
                }
            }
            .onChange(of: layout.isSplit, initial: true) { _, split in store.setWorkspaceInline(split) }
            .onChange(of: SidebarInputs(size: proxy.size, hasSecondary: store.workspace.secondary != nil), initial: true) {
                _, inputs in store.updateSidebarDefault(size: inputs.size)
            }
            .onChange(of: panesFit(proxy.size), initial: true) { _, fit in store.panesFit = fit }
        }
        // Layout reads the window's size, bars and all; the content keeps its safe area.
        .ignoresSafeArea()
        .onChange(of: store.workspace.selected, initial: true) { _, id in
            if let id { zoomSource = id }
        }
    }

    /// Whether two panes would sit side by side in this window, with the sidebar out of the way.
    private func panesFit(_ size: CGSize) -> Bool {
        PaneLayout.arrange(size: size, sizeClass: sizeClass, fold: fold, hasSecondary: true, primaryFraction: 0.5) != .single
    }

    private struct SidebarInputs: Equatable {
        var size: CGSize
        var hasSecondary: Bool
    }

    // MARK: Compact

    private var stack: some View {
        NavigationStack(path: $store.path) {
            ControlCenterView(store: store, zoom: zoom, now: now, board: board)
                .navigationDestination(for: AppRoute.self, destination: destination)
                .modifier(DebugTools(shown: debugShown))
        }
        .fullScreenCover(isPresented: $store.workspaceOpen) {
            WorkspaceView(store: store, terminals: terminals)
                .modifier(WorkspaceEntrance(sourceID: zoomSource, namespace: zoom))
        }
        // Under Reduce Motion the cover neither zooms nor slides; the workspace fades in itself.
        .transaction(value: store.workspaceOpen) { if reduceMotion { $0.disablesAnimations = true } }
    }

    // MARK: Regular

    private func split(_ detail: ShellLayout.Detail) -> some View {
        NavigationSplitView(columnVisibility: columnVisibility) {
            SessionSidebar(store: store, terminals: terminals, now: now)
                .navigationSplitViewColumnWidth(sidebarWidth)
                .modifier(DebugTools(shown: debugShown))
        } detail: {
            NavigationStack(path: $store.path) {
                detailRoot(detail)
                    .navigationDestination(for: AppRoute.self, destination: destination)
            }
        }
        .navigationSplitViewStyle(.balanced)
        .motion(.sidebar, value: store.sidebar.isHidden)
    }

    @ViewBuilder private func detailRoot(_ detail: ShellLayout.Detail) -> some View {
        switch detail {
        case .workspace:
            // The workspace has its own header, which carries the button that shows the sidebar.
            WorkspaceView(store: store, terminals: terminals)
                .toolbar(.hidden, for: .navigationBar)
        case .board:
            BoardView(store: store, services: board)
        case .empty:
            EmptyDetail(newSession: store.services.hosts.isEmpty ? nil : { store.startNewSession() })
        }
    }

    /// Book posture gives the sidebar the leading page exactly.
    private var sidebarWidth: CGFloat {
        PaneLayout.sidebarWidth(fold: fold) ?? PaneLayout.sidebarIdealWidth
    }

    private var columnVisibility: Binding<NavigationSplitViewVisibility> {
        Binding {
            store.sidebar.isHidden ? .detailOnly : .all
        } set: { visibility in
            store.sidebar.hidden = visibility == .detailOnly
        }
    }

    private var debugShown: Binding<Bool> {
        #if DEBUG
        $debugToolsShown
        #else
        .constant(false)
        #endif
    }
}

/// The debug screens, from a toolbar button in debug builds.
private struct DebugTools: ViewModifier {
    @Binding var shown: Bool

    func body(content: Content) -> some View {
        #if DEBUG
        content
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button("Debug tools", systemImage: "ladybug") { shown = true }
                }
            }
            .fullScreenCover(isPresented: $shown) { DebugEntry.root() }
        #else
        content
        #endif
    }
}

/// Grows the workspace out of its row, or under Reduce Motion fades it in where it stands.
private struct WorkspaceEntrance: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var shown = false
    let sourceID: TabID?
    let namespace: Namespace.ID?

    func body(content: Content) -> some View {
        if reduceMotion {
            content
                .opacity(shown ? 1 : 0)
                .onAppear { withAnimation(Motion.animation(.rowGrow, reduce: true)) { shown = true } }
        } else if let namespace {
            content.navigationTransition(Motion.rowGrow(sourceID: sourceID, in: namespace))
        } else {
            content
        }
    }
}
