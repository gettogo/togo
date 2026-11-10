import SwiftUI
import TogoKit

/// What a window shows at its size.
enum ShellLayout: Equatable, Sendable {
    /// Compact: the split view collapsed to the roster, the workspace pushed over it.
    case stack(workspaceShown: Bool)
    /// Regular: the roster column beside the detail column.
    case split(detail: Detail)

    enum Detail: Equatable, Sendable {
        /// The workspace, or with no tab open the empty detail, beside the roster.
        case workspace, empty
    }

    /// Reads the size class only: it is what the split view collapses on. The window's state is
    /// the same either way, so a Stage Manager resize, Split View or fold only changes where it
    /// is shown.
    static func resolve(
        sizeClass: UserInterfaceSizeClass?, workspaceOpen: Bool, hasTabs: Bool
    ) -> ShellLayout {
        guard sizeClass == .regular else { return .stack(workspaceShown: workspaceOpen && hasTabs) }
        return .split(detail: workspaceOpen && hasTabs ? .workspace : .empty)
    }

    var isSplit: Bool {
        if case .split = self { true } else { false }
    }
}

extension AppStore {
    /// Set by the window as it crosses between compact and regular. Tabs, selection, panes and
    /// `workspaceOpen` stay as they are.
    func setWorkspaceInline(_ inline: Bool) {
        guard workspaceInline != inline else { return }
        workspaceInline = inline
    }
}

/// The window below the first-run screen: one `NavigationSplitView` for every size, so a fold,
/// unfold or resize only changes how the system shows it. The roster and the workspace are the
/// same views in both, so each keeps its state (scroll, search, find, alerts) across a fold.
/// Regular: the roster in the sidebar column, the workspace (or the empty detail) in the detail
/// column with the prompt bar at its foot. Compact: the split collapses to the roster with the
/// prompt bar at its foot, and the workspace column is pushed over it, zooming out of the
/// tapped line, with the system's Back first in its bar.
struct AdaptiveRoot<Destination: View>: View {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.fold) private var fold
    @Bindable var store: AppStore
    let terminals: TerminalPool
    /// Lines are zoom sources in this namespace, so the workspace grows out of the tapped line.
    var zoom: Namespace.ID?
    /// A fixed clock for snapshots; nil follows the time of day.
    var now: Date?
    /// The bar under the roster and the terminal. Snapshots pass one in a fixed mode.
    var bar: PromptBarModel?
    @ViewBuilder let destination: (AppRoute) -> Destination
    /// The last tab on screen, so going back shrinks into its line instead of nowhere.
    @State private var zoomSource: TabID?
    @State private var ownBar = PromptBarModel()
    /// The whole window, bars and all.
    @State private var windowSize: CGSize = .zero
    /// The roster's bar as drawn. It is simply there again when the workspace closes (it is
    /// the same bar the user left; the zoom back to the row is the motion), and goes at once
    /// when the workspace opens over it. It presents from the bottom only when its first
    /// request arrives, which `PromptBar` itself does.
    @State private var rosterBarShown: Bool?
    /// The compact detail column keeps the workspace a moment after it closed, so the column
    /// pops with the session in it rather than an empty one. Nil follows `workspaceOpen`.
    @State private var workspaceHeld: Bool?

    var body: some View {
        let layout = ShellLayout.resolve(
            sizeClass: sizeClass, workspaceOpen: store.workspaceOpen,
            hasTabs: !store.workspace.tabs.isEmpty
        )
        NavigationSplitView(columnVisibility: columns, preferredCompactColumn: compactColumn) {
            NavigationStack(path: rosterPath) {
                rosterLayer
                    .navigationDestination(for: AppRoute.self, destination: destination)
            }
        } detail: {
            // The detail's own stack, so the workspace's items are in a system bar that an iPhone
            // Duo stands on its side beside the status bar.
            NavigationStack(path: detailPath) {
                detailRoot(layout)
                    .navigationDestination(for: AppRoute.self, destination: destination)
            }
            .modifier(WorkspaceEntrance(sourceID: zoomSource, namespace: zoom))
            // Mission control's inspector column belongs to the split view, on the detail's
            // stack and not inside it, so the workspace's items stay this stack's bar items: in
            // the vertical bar on an iPhone Duo, across the top on iPad (workspace-chat.md).
            .modifier(MissionInspectorColumn(app: store))
        }
        .navigationSplitViewStyle(.balanced)
        // Presenters below decide by the room the window has (`AdaptivePresentation`).
        .environment(\.windowWidth, windowSize.width)
        .background {
            Color.clear
                .onGeometryChange(for: CGSize.self) { $0.size } action: { windowSize = $0 }
                .ignoresSafeArea()
        }
        .onChange(of: layout.isSplit, initial: true) { _, split in
            store.setWorkspaceInline(split)
            // A fold or size class change moves the terminal; ask again who has the keyboard.
            KeyboardWatch.shared.refresh()
        }
        .onChange(of: windowSize) { KeyboardWatch.shared.refresh() }
        .onChange(of: panesFit(windowSize), initial: true) { _, fit in store.panesFit = fit }
        .feedsPromptBar(promptModel, store: store, focused: typingInto)
        // Answered requests show their outcome in the bar for a moment, then leave, whatever
        // the window shows.
        .modifier(RequestCollapse(store: store))
        .onChange(of: store.workspace.selected, initial: true) { _, id in
            if let id { zoomSource = id }
        }
        .onChange(of: store.workspaceOpen) { _, open in
            // Both directions are cuts: the bar is not news when the user comes back to it.
            withTransaction(Transaction(animation: nil)) { rosterBarShown = !open }
            hold(open)
        }
    }

    /// Opening shows the workspace at once; in compact, closing lets the pop finish first.
    private func hold(_ open: Bool) {
        guard !open, !inline else {
            workspaceHeld = open
            return
        }
        Task { @MainActor in
            try? await Task.sleep(for: Self.popDuration)
            if !store.workspaceOpen { workspaceHeld = false }
        }
    }

    private static var popDuration: Duration { .milliseconds(500) }

    /// Regular width: the roster and the detail sit side by side.
    private var inline: Bool { sizeClass == .regular }

    private var promptModel: PromptBarModel { bar ?? ownBar }

    /// The terminal the bar types into: the focused pane's, while a terminal is on screen.
    private var typingInto: TabID? {
        store.workspaceOnScreen ? store.focusedTab?.id : nil
    }

    /// Whether two panes would sit side by side in this window, with the roster out of the way.
    private func panesFit(_ size: CGSize) -> Bool {
        PaneLayout.arrange(size: size, sizeClass: sizeClass, fold: fold, hasSecondary: true, primaryFraction: 0.5) != .single
    }

    // MARK: Navigation state

    /// The sidebar shows unless the user hid it. The app never hides it to make room, and
    /// nothing else follows from hiding it.
    private var columns: Binding<NavigationSplitViewVisibility> {
        Binding {
            store.sidebar.isHidden ? .detailOnly : .all
        } set: { visibility in
            guard inline else { return }
            let hidden = visibility == .detailOnly
            if store.sidebar.isHidden != hidden { store.sidebar.hidden = hidden }
        }
    }

    /// What is pushed sits over the roster in compact and over the detail in regular.
    private var rosterPath: Binding<[AppRoute]> {
        Binding { inline ? [] : store.path } set: { if !inline { store.path = $0 } }
    }

    private var detailPath: Binding<[AppRoute]> {
        Binding { inline ? store.path : [] } set: { if inline { store.path = $0 } }
    }

    /// Which column a collapsed split shows: the workspace while one is open, else the roster.
    /// The system's Back (or a swipe) writes `.sidebar`, which closes the workspace. Nothing
    /// else opens it from here: a collapse never brings up an empty detail.
    private var compactColumn: Binding<NavigationSplitViewColumn> {
        Binding {
            store.workspaceOpen && !store.workspace.tabs.isEmpty ? .detail : .sidebar
        } set: { column in
            guard !inline, column != .detail, store.workspaceOpen else { return }
            store.workspaceOpen = false
        }
    }

    /// The workspace is the first branch of the detail in both widths, so a fold keeps the
    /// same view: regular shows it while it is open, compact also while its column pops.
    private func showsWorkspace(_ layout: ShellLayout) -> Bool {
        switch layout {
        case .split(.workspace): true
        case .split: false
        case .stack(let shown): !store.workspace.tabs.isEmpty && (shown || workspaceHeld == true)
        }
    }

    // MARK: Columns

    /// The bar floats at the foot of whatever it sits under, which keeps its last line clear.
    /// Hidden, it takes no room at all.
    private var promptBar: some View {
        let shown = promptModel.stage != .hidden
        return PromptBar(model: promptModel, maxHeight: windowSize.height > 0 ? windowSize.height * 0.5 : nil)
            .padding(.horizontal, shown ? 12 : 0)
            .padding(.bottom, shown ? 14 : 0)
            .modifier(OffTheFold())
    }

    /// The workspace stacked over the bar rather than under it: the terminal reaches under the
    /// home indicator and the keyboard by itself, so a bar in its safe area would cover its last
    /// rows. Stacked, the terminal ends where the bar begins and its grid counts only the rows
    /// that show.
    private var workspaceOverBar: some View {
        VStack(spacing: 0) {
            WorkspaceView(store: store, terminals: terminals)
            promptBar
        }
        .background(Color.togoInk)
        // The session's items are the detail column's own bar: across the top on iPad and
        // iPhone, in the vertical bar on an iPhone Duo. Never hidden with the empty detail.
        .toolbarVisibility(.visible, for: .navigationBar)
    }

    /// The sidebar column, and the whole compact window. The same `RosterView` at every width,
    /// so a fold keeps its scroll, search and cursor.
    private var rosterLayer: some View {
        RosterView(store: store, terminals: terminals, zoom: zoom, wide: inline, now: now, launchTarget: true)
            // One live bar at a time: in regular it is the detail's; in compact, while the
            // terminal covers the roster, it is the cover's.
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if !inline, rosterBarShown ?? !store.workspaceOpen {
                    // motion: continuity — the same bar the user left, simply there (a cut)
                    promptBar
                        .transition(.identity)
                }
            }
    }

    @ViewBuilder private func detailRoot(_ layout: ShellLayout) -> some View {
        if showsWorkspace(layout) {
            // The workspace fills the detail stack's bar, which goes vertical on an iPhone Duo;
            // the roster column's bar stays across its top.
            workspaceOverBar
        } else {
            placeholder(layout)
        }
    }

    @ViewBuilder private func placeholder(_ layout: ShellLayout) -> some View {
        switch layout {
        case .stack:
            // Collapsed with no workspace open, this column is not on screen.
            Color.togoInk.ignoresSafeArea()
        case .split(.empty), .split(.workspace):
            RosterEmptyDetail(waiting: store.roster.waiting > 0)
                .safeAreaInset(edge: .bottom, spacing: 0) { promptBar }
                // The detail keeps its native bar: hiding it here hid it for the workspace too,
                // which shares this stack's root, and left an iPhone Duo's vertical bar with the
                // status only. Settings is always here, as it is in the workspace's bar, so the
                // unfolded Duo's dock is never empty (root.md). New session is the sidebar's,
                // so it comes first here only while the user has hidden the sidebar (each
                // control once, P3; the prominent action leads, D13).
                .toolbar {
                    if store.sidebar.isHidden {
                        ToolbarItem(placement: .primaryAction) { RosterNewSessionButton(store: store) }
                    }
                    SettingsToolbarItem(store: store)
                }
        }
    }
}

/// Keeps a full-width floating bar to the wider page in book posture, so its field and buttons
/// never sit on the fold of an iPhone Duo. Flat, tabletop, or with the fold beside it, it keeps
/// its width. Measured from outside its own padding, so the padding cannot feed back.
private struct OffTheFold: ViewModifier {
    @Environment(\.fold) private var fold
    @Environment(\.foldOrigin) private var anchor
    @State private var frame: CGRect = .zero

    func body(content: Content) -> some View {
        let page = fold.readingPage(from: anchor, minX: frame.minX, width: frame.width)
        content
            .padding(.leading, page?.lowerBound ?? 0)
            .padding(.trailing, page.map { frame.width - $0.upperBound } ?? 0)
            .onGeometryChange(for: CGRect.self) { $0.frame(in: .global) } action: { frame = $0 }
    }
}

/// Grows the pushed workspace out of its row in a collapsed split; beside the sidebar the
/// column simply shows it. `namespace` is fixed for the life of the window (nil in snapshots),
/// so the branch never flips and the workspace keeps its identity.
private struct WorkspaceEntrance: ViewModifier {
    let sourceID: TabID?
    let namespace: Namespace.ID?

    func body(content: Content) -> some View {
        if let namespace {
            // motion: origin — the session grows out of the line that was tapped
            content.navigationTransition(Motion.rowGrow(sourceID: sourceID, in: namespace))
        } else {
            content
        }
    }
}
