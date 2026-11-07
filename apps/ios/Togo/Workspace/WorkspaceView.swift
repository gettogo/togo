import SwiftUI
import TogoKit
import UIKit

/// Where the user spends most of their time: the selected session's terminal edge to edge
/// on an opaque canvas, or two sessions side by side once the user puts one beside the other.
/// A session an adapter serves shows its chat over the terminal, which stays attached under it.
/// The way back, the session's name and its actions are in the navigation bar of the stack
/// around it, so on an iPhone Duo they sit in the vertical bar beside the status bar and the
/// terminal starts at the top edge. Only the tab strip, with more than one tab, and the find
/// field while it is open sit above the terminal. Requests wait in the prompt bar its presenter puts at its
/// foot. On a Duo held as a tabletop the terminal stays above the fold and the rest sits below.
struct WorkspaceView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.displayScale) private var displayScale
    @Environment(\.windowID) private var window
    @Environment(\.machineTools) private var machineTools
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Bindable var store: AppStore
    let terminals: TerminalPool

    @State private var status: String?
    @State private var finding = false
    @State private var findFocused = false
    /// `showsStrip` as the rows follow it: a second tab's strip reveals in a `resize`
    /// transaction, and the terminal under it lands once.
    @State private var stripFollowed: Bool?
    /// The session's bar items as the bar shows them (see `WorkspaceBarItems`).
    @State private var barFollowed: WorkspaceBarItems?
    @State private var renameTarget: SessionRow?
    @State private var renameText = ""
    @State private var killTarget: WorkspaceTab?
    @State private var panes = PanesState()
    @State private var zen: ZenChrome
    /// `store.zen` as the layout follows it, changed with animations off.
    @State private var zenLayout = false
    /// The session menu is open, so zen keeps the bar up under it.
    @State private var menuOpen = false
    @State private var review: ReviewHeaderModel

    init(store: AppStore, terminals: TerminalPool, zen: ZenChrome? = nil) {
        self.store = store
        self.terminals = terminals
        _zen = State(initialValue: zen ?? ZenChrome())
        _review = State(initialValue: ReviewHeaderModel(services: store.reviewServices))
    }

    var body: some View {
        WorkspacePanes(store: store, terminals: terminals, window: window) { geometry, insets in
            ChatPanes(store: store, geometry: geometry, insets: insets)
        }
            .onPreferenceChange(PanesReport.Key.self) { report in
                MainActor.assumeIsolated { if panes.report != report { panes.report = report } }
            }
            // The rows above the terminal keep to the page before the fold in book posture. In
            // zen they keep their place, only emptied, so the terminal is never resized by them.
            .safeAreaInset(edge: .top, spacing: 0) {
                if rowsPlaced {
                    rows.modifier(LeadingPageOnly())
                        .opacity(chromeLayout.shown ? 1 : 0)
                        .allowsHitTesting(chromeLayout.shown)
                        .accessibilityHidden(!chromeLayout.shown)
                        .transition(Motion.revealConceal(reduce: reduceMotion))
                }
            }
            .modifier(WorkspaceBar(revealOnly: chromeLayout.revealItem, title: title, onReveal: { zen.reveal() }) { toolbar })
            .onChange(of: store.zen, initial: true) { _, on in
                if !on { zen.hide() }
                terminals.zenChanged(on, in: window)
                var transaction = Transaction(animation: nil)
                transaction.disablesAnimations = true
                withTransaction(transaction) { zenLayout = on }
            }
            // In zen the bar stays while find or the session menu is open.
            .onChange(of: finding || menuOpen) { _, held in zen.pin(held && zenLayout) }
            .modifier(ReviewCounts(model: review, store: store, row: focusedRow))
            .modifier(HarnessSync(store: store, status: $status))
            .onChange(of: status) { _, line in
                if let line { AccessibilityNotification.Announcement(line).post() }
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                bottomBar
                    .background { Color.clear.tutorialTip(.workspace, arrowEdge: .bottom) }
                    .modifier(PaneColumns(state: panes) { [store] in $0.geometry.composeFrame(focused: store.workspace.focused) })
            }
            .background(Color.togoInk)
            // The pill and the bar item open it: a sheet on a phone, an inspector when wide.
            .modifier(FocusedMissionControl(app: store))
            .background { shortcuts }
            // Drags on the terminal scroll its history, so a pull down must not close the workspace.
            .interactiveDismissDisabled()
            .attachPrompts(store.services.attachPrompts, active: true)
            .reviewPresentation(store: store, terminals: terminals, active: true)
            .onChange(of: store.focusedTab?.id) { _, id in
                status = nil
                // The Compose key in the other pane moves focus to it with its bar open.
                if store.composing != id { store.composing = nil }
                endFind()
            }
            .haptic(.tabSettle, trigger: PaneTabs(store.workspace), change: PaneTabs.settles)
            .onChange(of: store.findRequests) {
                guard let tab = store.focusedTab, terminals.handle(for: tab.id)?.search != nil else { return }
                if zenLayout { zen.reveal() }
                openFind()
            }
            .onChange(of: liveBar.key(tab: store.focusedTab?.id)) {
                // An item arriving, leaving or changing face crossfades on `respond`.
                withAnimation(Motion.animation(.respond, reduce: reduceMotion)) { barFollowed = liveBar }
            }
            .onChange(of: showsStrip) { _, shows in
                withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { stripFollowed = shows }
            }
            .alert(
                renameTarget.map { ControlCenterCopy.renameTitle(SessionActions.label($0)) } ?? "",
                isPresented: presented($renameTarget), presenting: renameTarget
            ) { row in
                TextField(ControlCenterCopy.name, text: $renameText)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                Button(ControlCenterCopy.rename) { Task { await store.rename(row, to: renameText) } }
                Button(ControlCenterCopy.cancel, role: .cancel) {}
            }
            .confirmationDialog(
                killTarget.map(killTitle) ?? "", isPresented: presented($killTarget), titleVisibility: .visible,
                presenting: killTarget
            ) { tab in
                Button(ControlCenterCopy.kill, role: .destructive) { Task { status = await store.end(tab) } }
            } message: { _ in
                Text(ControlCenterCopy.killMessage)
            }
            .coordinateSpace(.named(PaneGeometry.space))
            .readsFold()
    }

    // MARK: Panes

    /// Chip taps and shortcuts settle like a swipe does. Under Reduce Motion the pages fade.
    private func show(_ id: TabID) {
        withAnimation(Motion.animation(.tabSettle, reduce: reduceMotion)) {
            store.choose(id)
        }
    }

    /// The panes land in their new arrangement at once and brighten into it (`WorkspacePanes`).
    private func openBeside(_ id: TabID) {
        store.openBeside(id)
    }

    /// The compose bar under the focused pane and, on a tabletop, the Keyboard button.
    private var bottomBar: some View {
        VStack(spacing: 0) {
            WorkspaceComposeBar(store: store, terminals: terminals)
            WithPanes(state: panes) { report in
                if report.geometry.isTabletop, store.composing == nil {
                    TabletopDeck<EmptyView>.KeyboardButton {
                        store.focusedTab.flatMap { terminals.handle(for: $0.id) }?.raiseKeyboard()
                    }
                }
            }
        }
    }

    // MARK: Chrome

    private var chromeLayout: WorkspaceChrome {
        WorkspaceChrome(zen: zenLayout, revealed: zen.revealed)
    }

    /// The tab strip or find field has a place above the terminal, shown or emptied by zen.
    private var rowsPlaced: Bool { stripShown || finding }

    /// One tab is already named in the title; the strip shows once there is a choice.
    private var showsStrip: Bool { store.workspace.tabs.count > 1 }

    private var stripShown: Bool { stripFollowed ?? showsStrip }

    private var focusedRow: SessionRow? { store.focusedTab.flatMap { store.session(for: $0) } }

    private var title: WorkspaceTitle? {
        guard let tab = store.focusedTab else { return nil }
        let row = store.session(for: tab)
        return WorkspaceTitle(
            tab: tab, row: row, machine: store.machine(for: tab.hostID)?.name,
            message: status ?? store.workspace.ended ?? row.flatMap { LifecycleCopy.line(status: $0.status, exit: $0.exit) }
        )
    }

    /// The tab strip and the find field, the only rows between the bar and the terminal.
    private var rows: some View {
        VStack(spacing: 0) {
            if stripShown {
                TabStrip(
                    chips: chips, selected: store.focusedTab?.id, beside: besideTab, canOpenBeside: panes.report.canSplit,
                    onSelect: show, onOpenBeside: openBeside, onClose: { store.close($0) }
                )
            }
            if finding {
                WorkspaceFindBar(search: focusedSearch, focused: $findFocused, onDone: endFind)
                    .transition(Motion.revealConceal(reduce: reduceMotion))
            }
            Rectangle()
                .fill(Color.togoLine)
                .frame(height: 1 / displayScale)
        }
        .background(Color.togoInk)
        // Like the system bars, the rows stop growing at the third accessibility size so the
        // terminal keeps most of the screen.
        .dynamicTypeSize(...DynamicTypeSize.accessibility3)
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        if let tab = store.focusedTab {
            WorkspaceToolbar(
                leading: leading, onLeading: leadingAction, runAgain: bar.runAgain,
                review: focusedRow.map { ReviewToolbarItems(store: store, row: $0, model: review) },
                layer: bar.layer, mission: bar.mission
            ) {
                actions(for: tab, row: store.session(for: tab))
                    .onAppear { menuOpen = true }
                    .onDisappear { menuOpen = false }
            }
        }
    }

    private var bar: WorkspaceBarItems { barFollowed ?? liveBar }

    /// The bar items the session has now.
    private var liveBar: WorkspaceBarItems {
        WorkspaceBarItems(
            runAgain: store.focusedTab.flatMap(runAgain), layer: store.layerItem,
            // A compact width has the pill at the top of the chat instead.
            mission: sizeClass == .regular ? store.missionItem : nil
        )
    }

    private func runAgain(_ tab: WorkspaceTab) -> WorkspaceRunAgain? {
        guard let row = store.session(for: tab),
              SessionActions.actions(for: row, on: store.machine(for: tab.hostID)).allows(.runAgain) else { return nil }
        return .init(running: store.relaunching.contains(tab.id)) {
            Task { await store.runAgain(row, tools: machineTools) }
        }
    }

    /// Back to the sessions list over a compact window. Beside the sidebar there is no list to go
    /// back to; with the sidebar hidden, the button shows it again.
    private var leading: WorkspaceLeading {
        guard store.workspaceInline else { return .back }
        return store.sidebar.isHidden ? .showSidebar : .none
    }

    private func leadingAction() {
        if store.workspaceInline {
            store.sidebar.hidden = false
        } else {
            store.workspaceOpen = false
        }
    }

    /// The tab in the pane that does not have focus, while two panes are on screen.
    private var besideTab: TabID? {
        guard store.panesVisible else { return nil }
        return store.workspace.focused == .primary ? store.workspace.secondary : store.workspace.selected
    }

    private var chips: [TabStrip.Chip] {
        store.workspace.tabs.map { tab in
            let label = store.chipLabel(for: tab)
            return TabStrip.Chip(id: tab.id, bib: label.bib, name: label.name)
        }
    }

    @ViewBuilder private func actions(for tab: WorkspaceTab, row: SessionRow?) -> some View {
        // The name and status, which the title says unless a vertical bar took it off screen,
        // and why the link fell back from the transport asked for.
        if let title {
            Section {
                Text(title.subtitle)
                if let fallback = title.fallback { Text(fallback) }
            } header: {
                Text(title.title)
            }
        }
        let etAvailable = store.services.hosts.first { $0.id == tab.hostID }?.etAvailable ?? false
        ConnectWithMenu(etAvailable: etAvailable) { transport in
            store.open(hostID: tab.hostID, sessionID: tab.sessionID, transport: transport)
        }
        OpenInWindowButton(tab: tab.id)
        Toggle(isOn: Binding(get: { store.zen }, set: { _ in store.toggleZen() })) {
            Label(ZenCopy.zenMode, systemImage: "arrow.up.left.and.arrow.down.right")
        }
        Button(WorkspaceCopy.copyLines, systemImage: "doc.on.doc") {
            Task { status = await store.copyLastLines(of: tab) }
        }
        if terminals.handle(for: tab.id)?.search != nil {
            Button(WorkspaceCopy.find, systemImage: "magnifyingglass", action: openFind)
        }
        if let links = terminals.handle(for: tab.id)?.links {
            Button(LinkCopy.linksOnScreen, systemImage: "link") {
                LinkCenter.shared.showLinks(links(), from: .init(hostID: tab.hostID, sessionID: tab.sessionID))
            }
        }
        if let row {
            let allowed = SessionActions.actions(for: row, on: store.machine(for: tab.hostID))
            if store.reviewSubject(for: row).canRead {
                Section {
                    Button(ReviewCopy.tabName(.files), systemImage: "folder") { store.review(row, tab: .files) }
                    Button(ReviewCopy.tabName(.history), systemImage: "clock") { store.review(row, tab: .history) }
                }
            }
            if allowed.available.contains(.runAgain) {
                Section {
                    Button(LifecycleCopy.runAgain, systemImage: SessionAction.runAgain.systemImage(for: row)) {
                        Task { await store.runAgain(row, tools: machineTools) }
                    }
                    .disabled(!allowed.allows(.runAgain) || store.relaunching.contains(tab.id))
                }
            }
            Section {
                if allowed.available.contains(.rename) {
                    Button(ControlCenterCopy.rename, systemImage: SessionAction.rename.systemImage(for: row)) {
                        renameText = row.name
                        renameTarget = row
                    }
                    .disabled(!allowed.allows(.rename))
                }
                if allowed.available.contains(.mute) {
                    Button(SessionAction.mute.title(for: row), systemImage: SessionAction.mute.systemImage(for: row)) {
                        Task { await store.setMute(row, !row.muted) }
                    }
                    .disabled(!allowed.allows(.mute))
                }
            }
            if allowed.available.contains(.kill) {
                Section {
                    Button(ControlCenterCopy.kill, systemImage: SessionAction.kill.systemImage(for: row), role: .destructive) {
                        killTarget = tab
                    }
                    .disabled(!allowed.allows(.kill))
                }
            }
            if let note = allowed.note {
                Section {
                    Text(note)
                }
            }
        }
    }

    private func killTitle(_ tab: WorkspaceTab) -> String {
        store.session(for: tab).map { SessionActions.killConfirmation(for: $0).title }
            ?? ControlCenterCopy.killTitle(tab.title)
    }

    // MARK: Find

    private var focusedSearch: TerminalSearch? {
        store.focusedTab.flatMap { terminals.handle(for: $0.id)?.search }
    }

    /// The find row reveals above the terminal in a `resize` transaction.
    private func openFind() {
        findFocused = true
        guard !finding else { return }
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { finding = true }
    }

    /// Clears the marks and the field; the row conceals as the space above the terminal closes.
    private func endFind() {
        guard finding else { return }
        focusedSearch?.clear()
        findFocused = false
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { finding = false }
    }

    // MARK: Shortcuts

    /// ⌘1 to ⌘9 while the terminal does not have focus. With focus the terminal claims the
    /// keys first and passes them up through `TerminalHandle.onShortcut`.
    private var shortcuts: some View {
        ForEach(Array(chips.prefix(9).enumerated()), id: \.element.id) { index, chip in
            Button(WorkspaceCopy.goTo([chip.bib, chip.name].compactMap(\.self).joined(separator: " "))) {
                withAnimation(Motion.animation(.tabSettle, reduce: reduceMotion)) {
                    store.selectTab(number: index + 1)
                }
            }
            .keyboardShortcut(KeyEquivalent(Character(String(index + 1))), modifiers: .command)
        }
        .opacity(0)
        .accessibilityHidden(true)
    }

    private func presented<T>(_ target: Binding<T?>) -> Binding<Bool> {
        Binding { target.wrappedValue != nil } set: { if !$0 { target.wrappedValue = nil } }
    }
}

#if DEBUG
#Preview("Three tabs") {
    let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
    store.open(hostID: Samples.studio, sessionID: "s-01")
    store.open(hostID: Samples.studio, sessionID: "s-scratch")
    store.open(hostID: Samples.studio, sessionID: "s-04")
    store.select(TabID(hostID: Samples.studio, sessionID: "s-01"))
    let terminals = TerminalPool()
    terminals.sync(with: store)
    return WorkspaceView(store: store, terminals: terminals)
        .tint(Color.togoSnow)
}
#endif

/// The panes' last report, read only by the small views that place things around the panes, so
/// a new report never rebuilds the whole workspace.
@Observable final class PanesState {
    var report = PanesReport.single
}

/// Content built from the panes' report.
private struct WithPanes<Content: View>: View {
    let state: PanesState
    @ViewBuilder let content: (PanesReport) -> Content

    var body: some View {
        content(state.report)
    }
}

/// Keeps a full-width bar to the columns the panes give it, such as the leading page of a book,
/// so nothing in it lands on the fold. Without columns it keeps the full width.
private struct PaneColumns: ViewModifier {
    let state: PanesState
    let pick: (PanesReport) -> CGRect?

    func body(content: Content) -> some View {
        if let frame = pick(state.report), frame.width > 0 {
            content
                .frame(width: frame.width)
                .frame(maxWidth: .infinity, alignment: .leading)
                .offset(x: frame.minX)
        } else {
            content
        }
    }
}

/// In book posture the header and tab strip keep to the page before the fold, so no button or
/// chip sits on it. Read from the fold itself rather than the panes' report, so the chrome is
/// laid out once and does not move when the report arrives.
private struct LeadingPageOnly: ViewModifier {
    @Environment(\.fold) private var fold

    func body(content: Content) -> some View {
        if fold.posture == .book, let division = fold.division, division.minX > 0 {
            content
                .frame(width: division.minX)
                .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            content
        }
    }
}
