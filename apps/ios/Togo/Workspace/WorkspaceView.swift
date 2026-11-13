import SwiftUI
import TogoKit
import UIKit
import TogoTerminal

/// Where the user spends most of their time: the selected session's terminal edge to edge
/// on an opaque canvas, or two sessions side by side once the user puts one beside the other.
/// A session an adapter serves shows its chat over the terminal, which stays attached under it.
/// The way back, the session's name and its actions are in the navigation bar of the stack
/// around it, so on an iPhone Duo they sit in the vertical bar beside the status bar and the
/// terminal starts at the top edge. Only the tab strip, with more than one tab, and the find
/// field while it is open sit above the terminal; under a compact horizontal bar the title's
/// menu lists the tabs instead (`WorkspaceBarLayout`). Requests wait in the prompt bar its presenter puts at its
/// foot. On a Duo held as a tabletop the terminal stays above the fold and the rest sits below.
struct WorkspaceView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.windowID) private var window
    @Environment(\.machineTools) private var machineTools
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.verticalSizeClass) private var verticalSizeClass
    @Environment(\.windowWidth) private var windowWidth
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
    /// Mission control's bar item, its long-press Compact and the session menu's screens.
    @State private var mission = MissionWorkspaceModel()
    /// The root moved the sidebar toggle into this bar (`SidebarTogglePlace.dock`).
    @Environment(\.sidebarToggleInDock) private var sidebarInDock

    init(store: AppStore, terminals: TerminalPool, zen: ZenChrome? = nil) {
        self.store = store
        self.terminals = terminals
        _zen = State(initialValue: zen ?? ZenChrome())
        _review = State(initialValue: ReviewHeaderModel(services: store.reviewServices))
    }

    var body: some View {
        BarEdgeReader { verticalBar in
            workspace(.choose(horizontal: sizeClass, vertical: verticalSizeClass, verticalBar: verticalBar), verticalBar: verticalBar)
                .environment(\.terminalBarVertical, verticalBar)
        }
    }

    private func workspace(_ layout: WorkspaceBarLayout, verticalBar: Bool) -> some View {
        WorkspacePanes(store: store, terminals: terminals, window: window) { geometry, insets in
            ChatPanes(store: store, geometry: geometry, insets: insets)
        }
            .onPreferenceChange(PanesReport.Key.self) { report in
                MainActor.assumeIsolated { if panes.report != report { panes.report = report } }
            }
            // The rows above the terminal keep to the page before the fold in book posture. In
            // zen they keep their place, only emptied, so the terminal is never resized by them.
            // A bar, not a plain inset: the chat scrolls under it behind the system's scroll edge
            // effect, as under the navigation bar, so no line shows above or through it.
            .safeAreaBar(edge: .top, spacing: 0) {
                if rowsPlaced(layout) {
                    rows(layout).modifier(LeadingPageOnly())
                        .modifier(WorkspaceRowsAppearance(store: store))
                        .opacity(chromeLayout.shown ? 1 : 0)
                        .allowsHitTesting(chromeLayout.shown)
                        .accessibilityHidden(!chromeLayout.shown)
                        // motion: origin — the user opened a second tab or find: the row's space
                        // opens downwards from the bar and uncovers it; nothing rises.
                        .transition(Motion.unfold(reduce: reduceMotion))
                }
            }
            .modifier(WorkspaceBar(revealOnly: chromeLayout.revealItem, title: title(layout), onReveal: { zen.reveal() }) { toolbar(layout, verticalBar: verticalBar) })
            .modifier(TitleOffBesideVerticalBar())
            .modifier(TaskBarCompression())
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
                    // The tip's popover anchor is a UIKit view over this strip, where the terminal
                    // keys' row stands above the keyboard: it must never take their touches.
                    .background { Color.clear.tutorialTip(.workspace, arrowEdge: .bottom).allowsHitTesting(false) }
                    .modifier(PaneColumns(state: panes) { [store] in $0.geometry.composeFrame(focused: store.workspace.focused) })
            }
            // The terminal theme's canvas behind the bar while the terminal shows, the app's otherwise.
            .modifier(WorkspaceBackdrop(store: store))
            // The bar item opens it: a sheet on a phone, an inspector when wide.
            .modifier(FocusedMissionControl(app: store))
            .modifier(MissionWorkspace(app: store, model: mission))
            .background { shortcuts(layout) }
            // Drags on the terminal scroll its history, so a pull down must not close the workspace.
            .interactiveDismissDisabled()
            // Sign-in questions and the review are the root's (`RootView`), above the split view
            // this column lives in, so a fold never swaps their host out mid-presentation.
            .onChange(of: store.focusedTab?.id) { _, id in
                status = nil
                endFind()
            }
            .haptic(.tabSettle, trigger: PaneTabs(store.workspace), change: PaneTabs.settles)
            .onChange(of: store.findRequests) {
                guard let tab = store.focusedTab, terminals.handle(for: tab.id)?.search != nil else { return }
                if zenLayout { zen.reveal() }
                openFind()
            }
            .onChange(of: liveBar.key(tab: store.focusedTab?.id)) {
                // motion: state change — an item arriving, leaving or changing face crossfades
                // in its place in the bar.
                withAnimation(Motion.animation(.respond, reduce: reduceMotion)) { barFollowed = liveBar }
            }
            .onChange(of: showsStrip(layout)) { _, shows in
                // motion: preventing a jump — the strip's row opens and the terminal lands once.
                withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { stripFollowed = shows }
            }
            .coordinated("workspace.rename", .alert, isPresented: presented($renameTarget)) { view, shown in
                view.alert(
                    renameTarget.map { ControlCenterCopy.renameTitle(SessionActions.label($0)) } ?? "",
                    isPresented: shown, presenting: renameTarget
                ) { row in
                    TextField(ControlCenterCopy.name, text: $renameText)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Button(ControlCenterCopy.rename) { Task { await store.rename(row, to: renameText) } }
                    Button(ControlCenterCopy.cancel, role: .cancel) {}
                }
            }
            .coordinated("workspace.kill", .alert, isPresented: presented($killTarget)) { view, shown in
                view.alert(
                    killTarget.map(killTitle) ?? "", isPresented: shown, presenting: killTarget
                ) { tab in
                    Button(ControlCenterCopy.kill, role: .destructive) { Task { status = await store.end(tab) } }
                    Button(ControlCenterCopy.cancel, role: .cancel) {}
                } message: { _ in
                    Text(ControlCenterCopy.killMessage)
                }
            }
            .coordinateSpace(.named(PaneGeometry.space))
            .readsFold()
    }

    // MARK: Panes

    /// A tab switch is a cut: the chosen session is simply there. Only the strip's selection
    /// capsule moves (its own `.motion(.tabSettle, value:)`), so no chat crossfades over the
    /// next tab's terminal.
    private func show(_ id: TabID) {
        store.choose(id)
    }

    /// The panes land in their new arrangement at once and brighten into it (`WorkspacePanes`).
    private func openBeside(_ id: TabID) {
        store.openBeside(id)
    }

    /// The chat's composer or the terminal's input status under the focused pane and, on a
    /// tabletop, the Keyboard button.
    private var bottomBar: some View {
        VStack(spacing: 0) {
            WorkspaceComposeBar(store: store, terminals: terminals)
            WithPanes(state: panes) { report in
                if report.geometry.isTabletop {
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
    private func rowsPlaced(_ layout: WorkspaceBarLayout) -> Bool { stripShown(layout) || finding }

    /// One tab is already named in the title; the strip shows once there is a choice, and never
    /// under a compact horizontal bar, where the title's menu lists the tabs.
    private func showsStrip(_ layout: WorkspaceBarLayout) -> Bool { layout.showsRail && store.workspace.tabs.count > 1 }

    private func stripShown(_ layout: WorkspaceBarLayout) -> Bool { stripFollowed ?? showsStrip(layout) }

    private var focusedRow: SessionRow? { store.focusedTab.flatMap { store.session(for: $0) } }

    /// The title for `layout`: a compact bar's subtitle is the bib and status only, so the
    /// title's width leaves the bar room for its three items (the session menu has the rest).
    private func title(_ layout: WorkspaceBarLayout = .full) -> WorkspaceTitle? {
        guard let tab = store.focusedTab else { return nil }
        let row = store.session(for: tab)
        return WorkspaceTitle(
            tab: tab, row: row, machine: store.machine(for: tab.hostID)?.name,
            message: status ?? store.workspace.ended ?? row.flatMap { LifecycleCopy.line(status: $0.status, exit: $0.exit) },
            glance: store.missionStore.map { MissionPlacement.glance($0.mission.summary) },
            brief: layout.titleMenu
        )
    }

    /// The tab strip and the find field, the only rows between the bar and the terminal.
    private func rows(_ layout: WorkspaceBarLayout) -> some View {
        VStack(spacing: 0) {
            if stripShown(layout) {
                TabStrip(
                    chips: chips, selected: store.focusedTab?.id, beside: besideTab, canOpenBeside: panes.report.canSplit,
                    onSelect: show, onOpenBeside: openBeside, onClose: { store.close($0) }
                )
            }
            if finding {
                WorkspaceFindBar(search: focusedSearch, focused: $findFocused, onDone: endFind)
                    // motion: origin — the user asked for find: its row opens under the strip
                    // and uncovers the field in place.
                    .transition(Motion.unfold(reduce: reduceMotion))
            }
        }
        // No fill and no rule of their own: the rows are part of the top bar, one band with the
        // navigation bar, and the chat scrolls under both behind the system's scroll edge
        // effect. A painted band here drew a second strip under the bar's, cut the chat with a
        // seam, and stopped short of a Duo's vertical bar.
        // Like the system bars, the rows stop growing at the third accessibility size so the
        // terminal keeps most of the screen.
        .dynamicTypeSize(...DynamicTypeSize.accessibility3)
    }

    @ToolbarContentBuilder private func toolbar(_ layout: WorkspaceBarLayout, verticalBar: Bool) -> some ToolbarContent {
        if let tab = store.focusedTab {
            if layout.titleMenu {
                ToolbarTitleMenu {
                    WorkspaceTabMenu(
                        entries: store.tabMenuEntries, selected: tab.id,
                        onSelect: show, onClose: { store.close($0) }, onAllSessions: leadingAction
                    )
                }
            }
            // With the controller in a vertical bar, the bar's rarer items move into the session
            // menu, so the system never overflows the bar and the controller holds the last slot.
            let keys = dockKeys(for: tab, verticalBar: verticalBar)
            let inMenu = keys != nil
            WorkspaceToolbar(
                leading: leading, onLeading: leadingAction, runAgain: bar.runAgain,
                review: focusedRow.map {
                    ReviewToolbarItems(store: store, row: $0, model: review, folded: reviewFold(layout, inMenu: inMenu))
                },
                layer: inMenu ? nil : bar.layer,
                mission: layout.sessionItemsInBar && !inMenu ? bar.mission : nil,
                settings: store.workspaceInline && !inMenu ? SettingsToolbarItem(store: store) : nil,
                sidebar: sidebarInDock ? SidebarToggleToolbarItem(store: store) : nil,
                keys: keys,
                showKeyboard: ShowKeyboardToolbarItem(store: store, terminals: terminals, tab: tab.id)
            ) {
                actions(for: tab, row: store.session(for: tab), layout: layout, inMenu: inMenu)
                    .onAppear { menuOpen = true }
                    .onDisappear { menuOpen = false }
            }
        }
    }

    /// The controller as the vertical bar's own capsule, for the focused terminal while the
    /// terminal layer shows (contract rev 4: a bar that can hold it does; the column stays away).
    private func dockKeys(for tab: WorkspaceTab, verticalBar: Bool) -> TerminalKeysToolbarItem? {
        guard verticalBar, store.layer(for: tab.id) == .terminal,
              let keys = terminals.handle(for: tab.id)?.keys else { return nil }
        return TerminalKeysToolbarItem(keys: keys)
    }

    /// Preview leaves the bar where mission control has Ports, and under a compact horizontal
    /// bar, where the session menu has it.
    private func reviewFold(_ layout: WorkspaceBarLayout, inMenu: Bool = false) -> ReviewFold {
        var fold = ReviewFold(store.missionStore?.descriptor)
        if !layout.sessionItemsInBar || inMenu { fold.preview = true }
        return fold
    }

    private var bar: WorkspaceBarItems { barFollowed ?? liveBar }

    /// The bar items the session has now.
    private var liveBar: WorkspaceBarItems {
        WorkspaceBarItems(
            runAgain: store.focusedTab.flatMap(runAgain), layer: store.layerItem,
            mission: mission.item(for: store, presentation: MissionPresentation.choose(sizeClass, windowWidth: windowWidth)) { line in
                if let line { status = line }
            }
        )
    }

    private func runAgain(_ tab: WorkspaceTab) -> WorkspaceRunAgain? {
        guard let row = store.session(for: tab),
              SessionActions.actions(for: row, on: store.machine(for: tab.hostID)).allows(.runAgain) else { return nil }
        return .init(running: store.relaunching.contains(tab.id)) {
            Task { await store.runAgain(row, tools: machineTools) }
        }
    }

    /// No item of its own: in a compact window the workspace is the pushed detail column and the
    /// system's Back ("Sessions") leads its bar; beside the sidebar the split view's toggle
    /// brings a hidden sidebar back (root.md).
    private var leading: WorkspaceLeading { .none }

    private func leadingAction() {
        store.workspaceOpen = false
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

    @ViewBuilder private func actions(for tab: WorkspaceTab, row: SessionRow?, layout: WorkspaceBarLayout, inMenu: Bool = false) -> some View {
        // The name and status, which the title says unless a vertical bar took it off screen,
        // and why the link fell back from the transport asked for.
        if let title = title(layout) {
            Section {
                Text(title.detail)
                if let fallback = title.fallback { Text(fallback) }
            } header: {
                Text(title.title)
            }
        }
        // A compact horizontal bar keeps three items; mission control is the menu's first. With
        // the controller in a vertical bar, the layer switch and Settings join it here too.
        if !layout.sessionItemsInBar || inMenu, let item = bar.mission {
            MissionMenuSection(item: item)
        }
        if inMenu {
            Section {
                if let layer = bar.layer {
                    Button(layer.layer == .chat ? ChatCopy.terminal : ChatCopy.chat,
                           systemImage: layer.layer == .chat ? "terminal" : "bubble.left.and.text.bubble.right") {
                        LayerToolbarItem.resignKeyboard()
                        // motion: state change — fade between this session's chat and terminal.
                        withAnimation(Motion.animation(.fade, reduce: reduceMotion)) { layer.toggle() }
                    }
                }
                if store.workspaceInline {
                    Button(RosterCopy.settings, systemImage: "gearshape") { store.showSettings() }
                }
            }
        }
        let etAvailable = store.services.hosts.first { $0.id == tab.hostID }?.etAvailable ?? false
        ConnectWithMenu(etAvailable: etAvailable) { transport in
            store.open(hostID: tab.hostID, sessionID: tab.sessionID, transport: transport)
        }
        // With two panes on screen each pane's own menu has it (`PaneHeader`), once per pane.
        if !store.panesVisible {
            OpenInWindowButton(tab: tab.id)
        }
        if let harness = store.harness(for: tab.id), harness.chatAvailable {
            MissionSessionSection(store: harness, tab: tab.id, model: mission)
        }
        Toggle(isOn: Binding(get: { store.zen }, set: { _ in store.toggleZen() })) {
            Label(ZenCopy.zenMode, systemImage: "arrow.up.left.and.arrow.down.right")
        }
        .menuToggle()
        // Holds the column up with the keyboard down, where the setting offers it.
        if store.layer(for: tab.id) == .terminal, store.services.settings.terminalKeys.showColumn == .alsoOnDemand {
            Toggle(isOn: Binding(
                get: { store.keptKeys.contains(tab.id) },
                set: { on in if on { store.keptKeys.insert(tab.id) } else { store.keptKeys.remove(tab.id) } }
            )) {
                Label(KeysCopy.keepOnScreen, systemImage: "keyboard.onehanded.left")
            }
            .menuToggle()
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
            let canRead = store.reviewSubject(for: row).canRead
            // Preview, where the bar left it out for room and mission control does not carry it.
            let preview = (!layout.sessionItemsInBar || inMenu) && review.hasPorts && !ReviewFold(store.missionStore?.descriptor).preview
            if canRead || preview {
                Section {
                    if canRead {
                        Button(ReviewCopy.tabName(.files), systemImage: "folder") { store.review(row, tab: .files) }
                        Button(ReviewCopy.tabName(.history), systemImage: "clock") { store.review(row, tab: .history) }
                    }
                    if preview {
                        Button(ReviewEntryCopy.preview, systemImage: "safari") { store.review(row, tab: .ports) }
                    }
                }
            }
            // Run again is the bar's prominent item once the agent stopped; not repeated here.
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
        // motion: preventing a jump — space opens above the terminal for the find row.
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { finding = true }
    }

    /// Clears the marks and the field; the row conceals as the space above the terminal closes.
    private func endFind() {
        guard finding else { return }
        focusedSearch?.clear()
        findFocused = false
        // motion: preventing a jump — the find row conceals, then its space closes.
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { finding = false }
    }

    // MARK: Shortcuts

    /// ⌘1 to ⌘9 while the terminal does not have focus. With focus the terminal claims the
    /// keys first and passes them up through `TerminalHandle.onShortcut`. ⌘I is mission
    /// control's bar item's own; where the item is in the session menu, it is here.
    private func shortcuts(_ layout: WorkspaceBarLayout) -> some View {
        ZStack {
            ForEach(Array(chips.prefix(9).enumerated()), id: \.element.id) { index, chip in
                Button(WorkspaceCopy.goTo([chip.bib, chip.name].compactMap(\.self).joined(separator: " "))) {
                    // A cut, like a chip tap (`show`).
                    store.selectTab(number: index + 1)
                }
                .keyboardShortcut(KeyEquivalent(Character(String(index + 1))), modifiers: .command)
            }
            if !layout.sessionItemsInBar, let mission = store.missionStore {
                Button(MissionCopy.title) { mission.showsMission.toggle() }
                    .keyboardShortcut("i", modifiers: .command)
            }
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
