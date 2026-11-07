import SwiftUI
import TogoKit

/// What a window shows at its size.
enum ShellLayout: Equatable, Sendable {
    /// Compact: the roster in a stack, the workspace as a cover over it.
    case stack(coverShown: Bool)
    /// Regular: the roster column beside the terminal column, or the monitor across the window.
    case split(detail: Detail)

    enum Detail: Equatable, Sendable {
        /// The workspace, or with no tab open the empty detail, beside the roster.
        case workspace, empty
        /// The roster alone across the window as a status display.
        case monitor
    }

    /// Reads window size and size class only. The window's state is the same either way, so a
    /// Stage Manager resize, Split View or fold only changes where it is shown. In a compact
    /// window the monitor takes the roster's place under the cover.
    static func resolve(
        size: CGSize, sizeClass: UserInterfaceSizeClass?, workspaceOpen: Bool, hasTabs: Bool, monitorShown: Bool = false
    ) -> ShellLayout {
        guard PaneLayout.showsSidebar(size: size, sizeClass: sizeClass) else {
            return .stack(coverShown: workspaceOpen)
        }
        if monitorShown { return .split(detail: .monitor) }
        return .split(detail: workspaceOpen && hasTabs ? .workspace : .empty)
    }

    var isSplit: Bool {
        if case .split = self { true } else { false }
    }
}

/// Whether the roster column shows beside the terminal, and the ⌘K requests for it.
struct SidebarState: Equatable, Sendable {
    /// The user's last choice; nil follows `prefersHidden`.
    var hidden: Bool?
    /// Two panes in a window too narrow for them and a sidebar hide it by default.
    var prefersHidden = false
    /// Bumped by ⌘K beside the terminal.
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

/// The window below the first-run screen. Compact: the roster in a navigation stack with the
/// prompt bar at its foot, and the workspace as a cover that grows out of the tapped line, the
/// bar then its input. Regular: the roster as a narrow column on ink with one trailing hairline,
/// the terminal column beside it with the bar at its foot.
struct AdaptiveRoot<Destination: View>: View {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.displayScale) private var displayScale
    @Environment(\.fold) private var fold
    @Bindable var store: AppStore
    let terminals: TerminalPool
    /// Lines are zoom sources in this namespace, so the cover grows out of the tapped line.
    var zoom: Namespace.ID?
    /// A fixed clock for snapshots; nil follows the time of day.
    var now: Date?
    /// What monitor mode runs on; nil for the device's own.
    var monitor: MonitorServices?
    /// The bar under the roster and the terminal. Snapshots pass one in a fixed mode.
    var bar: PromptBarModel?
    @ViewBuilder let destination: (AppRoute) -> Destination
    /// The last tab on screen, so closing the cover shrinks into its line instead of nowhere.
    @State private var zoomSource: TabID?
    @State private var ownBar = PromptBarModel()
    @State private var windowHeight: CGFloat = 0

    var body: some View {
        GeometryReader { proxy in
            let layout = ShellLayout.resolve(
                size: proxy.size, sizeClass: sizeClass, workspaceOpen: store.workspaceOpen,
                hasTabs: !store.workspace.tabs.isEmpty, monitorShown: store.monitorShown
            )
            Group {
                switch layout {
                case .stack: stack
                case .split(let detail): split(detail)
                }
            }
            .onChange(of: layout.isSplit, initial: true) { _, split in
                store.setWorkspaceInline(split)
                // A fold or size class change moves the terminal; ask again who has the keyboard.
                KeyboardWatch.shared.refresh()
            }
            .onChange(of: proxy.size) { KeyboardWatch.shared.refresh() }
            .onChange(of: SidebarInputs(size: proxy.size, hasSecondary: store.workspace.secondary != nil), initial: true) {
                _, inputs in store.updateSidebarDefault(size: inputs.size)
            }
            .onChange(of: panesFit(proxy.size), initial: true) { _, fit in store.panesFit = fit }
            .onChange(of: proxy.size.height, initial: true) { _, height in windowHeight = height }
        }
        // Layout reads the window's size, bars and all; the content keeps its safe area.
        .ignoresSafeArea()
        .feedsPromptBar(promptModel, store: store, terminals: terminals, focused: typingInto)
        // Answered requests show their outcome in the bar for a moment, then leave, whatever
        // the window shows.
        .modifier(RequestCollapse(store: store))
        .onChange(of: store.workspace.selected, initial: true) { _, id in
            if let id { zoomSource = id }
        }
    }

    private var promptModel: PromptBarModel { bar ?? ownBar }

    /// The terminal the bar types into: the focused pane's, while a terminal is on screen.
    private var typingInto: TabID? {
        store.workspaceOnScreen ? store.focusedTab?.id : nil
    }

    /// Whether two panes would sit side by side in this window, with the roster out of the way.
    private func panesFit(_ size: CGSize) -> Bool {
        PaneLayout.arrange(size: size, sizeClass: sizeClass, fold: fold, hasSecondary: true, primaryFraction: 0.5) != .single
    }

    private struct SidebarInputs: Equatable {
        var size: CGSize
        var hasSecondary: Bool
    }

    /// The bar floats at the foot of whatever it sits under, which keeps its last line clear.
    /// Hidden, it takes no room at all.
    private var promptBar: some View {
        let shown = promptModel.stage != .hidden
        return PromptBar(model: promptModel, maxHeight: windowHeight > 0 ? windowHeight * 0.5 : nil)
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
    }

    // MARK: Compact

    private var stack: some View {
        NavigationStack(path: $store.path) {
            compactRoot
                .navigationDestination(for: AppRoute.self, destination: destination)
        }
        .fullScreenCover(isPresented: $store.workspaceOpen) {
            // A stack of its own, so the workspace's items are in a system bar that an iPhone
            // Duo stands on its side beside the status bar.
            NavigationStack {
                workspaceOverBar
            }
            .modifier(WorkspaceEntrance(sourceID: zoomSource, namespace: zoom))
        }
        // Under Reduce Motion the cover neither zooms nor slides; the workspace fades in itself.
        .transaction(value: store.workspaceOpen) { if reduceMotion { $0.disablesAnimations = true } }
    }

    @ViewBuilder private var compactRoot: some View {
        if store.monitorShown {
            RosterMonitor(store: store, services: monitor, now: now)
        } else {
            // One live bar at a time: while the terminal covers the roster, the bar is the cover's.
            RosterView(store: store, terminals: terminals, zoom: zoom, now: now, launchTarget: true)
                .safeAreaInset(edge: .bottom, spacing: 0) {
                    if !store.workspaceOpen { promptBar }
                }
        }
    }

    // MARK: Regular

    @ViewBuilder private func split(_ detail: ShellLayout.Detail) -> some View {
        if detail == .monitor {
            NavigationStack(path: $store.path) {
                RosterMonitor(store: store, services: monitor, now: now)
                    .navigationDestination(for: AppRoute.self, destination: destination)
            }
        } else {
            HStack(spacing: 0) {
                if !store.sidebar.isHidden {
                    NavigationStack {
                        RosterView(store: store, terminals: terminals, wide: true, now: now, launchTarget: true)
                    }
                    .frame(width: PaneLayout.rosterWidth(fold: fold))
                    .overlay(alignment: .trailing) {
                        Rectangle().fill(Color.togoLine).frame(width: 1 / displayScale).ignoresSafeArea()
                    }
                    .transition(.move(edge: .leading))
                }
                NavigationStack(path: $store.path) {
                    detailRoot(detail)
                        .navigationDestination(for: AppRoute.self, destination: destination)
                }
                .frame(maxWidth: .infinity)
            }
            .background(Color.togoInk)
            .motion(.sidebar, value: store.sidebar.isHidden)
        }
    }

    @ViewBuilder private func detailRoot(_ detail: ShellLayout.Detail) -> some View {
        switch detail {
        case .workspace:
            // The workspace fills the detail stack's bar, which goes vertical on an iPhone Duo;
            // the roster column's bar stays across its top.
            workspaceOverBar
        case .empty, .monitor:
            RosterEmptyDetail(
                waiting: store.roster.waiting > 0, rosterHidden: store.sidebar.isHidden,
                showRoster: { store.sidebar.hidden = false }
            )
            .safeAreaInset(edge: .bottom, spacing: 0) { promptBar }
            // The detail's bar is never empty, so its vertical bar on an iPhone Duo is never a
            // blank strip.
            .toolbar {
                if store.sidebar.isHidden {
                    ToolbarItem(placement: .navigation) {
                        Button(AppCommandCopy.showSidebar, systemImage: "sidebar.leading") { store.sidebar.hidden = false }
                    }
                }
                ToolbarItem(placement: .primaryAction) {
                    Button(RosterCopy.newSession, systemImage: "plus") { store.startNewSession() }
                        .disabled(store.services.hosts.isEmpty)
                }
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
