import SwiftUI
import TogoKit
import UIKit

/// Where the user spends most of their time: the selected session's terminal edge to edge
/// on an opaque canvas, or two sessions side by side once the user puts one beside the other,
/// the header and tab strip above, and requests from other sessions dropping in above the key
/// bar. On a Duo held as a tabletop the terminal stays above the fold and the rest sits below.
struct WorkspaceView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.displayScale) private var displayScale
    @Environment(\.windowID) private var window
    @Environment(\.machineTools) private var machineTools
    @Bindable var store: AppStore
    let terminals: TerminalPool

    @Namespace private var chipSpace
    @State private var noteShown: Bool
    @State private var status: String?
    @State private var finding = false
    @State private var findText = ""
    @State private var findMissed = false
    @FocusState private var findFocused: Bool
    @State private var renameTarget: SessionRow?
    @State private var renameText = ""
    @State private var killTarget: WorkspaceTab?
    @State private var panes = PanesState()
    @State private var zen: ZenChrome
    /// `store.zen` as the layout follows it, changed with animations off.
    @State private var zenLayout = false

    init(store: AppStore, terminals: TerminalPool, noteShown: Bool = false, zen: ZenChrome? = nil) {
        self.store = store
        self.terminals = terminals
        _noteShown = State(initialValue: noteShown)
        _zen = State(initialValue: zen ?? ZenChrome())
    }

    var body: some View {
        WorkspacePanes(store: store, terminals: terminals, window: window) { geometry, insets in
            requests(in: geometry, insets: insets)
        }
            .onPreferenceChange(PanesReport.Key.self) { report in
                MainActor.assumeIsolated { if panes.report != report { panes.report = report } }
            }
            // The tip anchors behind the chrome so its popover never changes the chrome's layout.
            // In zen mode the chrome leaves the inset, with animations off so the terminal is
            // resized once, and comes back over the terminal for a moment from the strip along the
            // top edge. The chrome is only ever in one of the two places.
            .safeAreaInset(edge: .top, spacing: 0) {
                if chromeLayout.inset {
                    chrome.background { Color.clear.tutorialTip(.workspace) }
                        .modifier(LeadingPageOnly())
                }
            }
            .overlay(alignment: .top) {
                ZStack(alignment: .top) {
                    if chromeLayout.overlay {
                        // Using the chrome keeps it up for another three seconds.
                        chrome.modifier(LeadingPageOnly())
                            .simultaneousGesture(TapGesture().onEnded { zen.reveal() })
                            // Under Reduce Motion the chrome fades in place instead of sliding.
                            .transition(reduceMotion ? .opacity : .move(edge: .top).combined(with: .opacity))
                    } else if chromeLayout.revealStrip {
                        ZenRevealStrip { zen.reveal() }
                    }
                }
                .motion(.chrome, value: chromeLayout.overlay)
            }
            .onChange(of: store.zen, initial: true) { _, on in
                if !on { zen.hide() }
                terminals.zenChanged(on, in: window)
                var transaction = Transaction(animation: nil)
                transaction.disablesAnimations = true
                withTransaction(transaction) { zenLayout = on }
            }
            // In zen the chrome stays while find is open, so the field taking the keyboard is on screen.
            .onChange(of: finding) { _, open in zen.pin(open && zenLayout) }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                bottomBar
                    .modifier(PaneColumns(state: panes) { [store] in $0.geometry.composeFrame(focused: store.workspace.focused) })
            }
            .background(Color.togoInk)
            .background { shortcuts }
            // Drags on the terminal scroll its history, so a pull down must not close the workspace.
            .interactiveDismissDisabled()
            .attachPrompts(store.services.attachPrompts, active: true)
            .reviewPresentation(store: store, terminals: terminals, active: true)
            .onChange(of: store.focusedTab?.id) { _, id in
                status = nil
                // The Compose key in the other pane moves focus to it with its bar open.
                if store.composing != id { store.composing = nil }
                noteShown = false
                endFind()
            }
            .haptic(.tabSettle, trigger: PaneTabs(store.workspace), change: PaneTabs.settles)
            .onChange(of: store.findRequests) {
                guard let tab = store.focusedTab, terminals.handle(for: tab.id)?.search != nil else { return }
                if zenLayout { zen.reveal() }
                finding = true
                findFocused = true
            }
            .alert(
                renameTarget.map { ControlCenterCopy.renameTitle(SessionActions.label($0)) } ?? "",
                isPresented: presented($renameTarget), presenting: renameTarget
            ) { row in
                TextField(ControlCenterCopy.name, text: $renameText)
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
            .verticalBarsOff()
    }

    // MARK: Panes

    /// Chip taps and shortcuts settle like a swipe does. Under Reduce Motion the pages fade.
    private func show(_ id: TabID) {
        withAnimation(Motion.animation(.tabSettle, reduce: reduceMotion)) {
            store.choose(id)
        }
    }

    private func openBeside(_ id: TabID) {
        withAnimation(Motion.animation(.paneFlow, reduce: reduceMotion)) {
            store.openBeside(id)
        }
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

    private var chrome: some View {
        VStack(spacing: 0) {
            if let tab = store.focusedTab {
                header(tab)
            }
            TabStrip(
                chips: chips, selected: store.focusedTab?.id, beside: besideTab, canOpenBeside: panes.report.canSplit,
                namespace: chipSpace, onSelect: show, onOpenBeside: openBeside, onClose: { store.close($0) }
            )
            if finding {
                findBar
            }
            Rectangle()
                .fill(Color.togoLine)
                .frame(height: 1 / displayScale)
        }
        .background(Color.togoInk)
        // Like the system bars, the chrome stops growing at the third accessibility size so the
        // terminal keeps most of the screen.
        .dynamicTypeSize(...DynamicTypeSize.accessibility3)
    }

    private func header(_ tab: WorkspaceTab) -> some View {
        let row = store.session(for: tab)
        let name = row.map { $0.name.isEmpty ? $0.agentName : $0.name } ?? tab.title
        return WorkspaceHeader(
            bib: row?.bib ?? tab.bib, title: name, link: tab.link,
            status: status ?? store.workspace.ended ?? row.flatMap { LifecycleCopy.line(status: $0.status, exit: $0.exit) },
            noteShown: $noteShown,
            onBack: leadingAction, leading: leading,
            accessories: {
                if let row {
                    if SessionActions.actions(for: row, on: store.machine(for: tab.hostID)).allows(.runAgain) {
                        RunAgainButton(running: store.relaunching.contains(tab.id)) {
                            Task { await store.runAgain(row, tools: machineTools) }
                        }
                    }
                    ReviewHeaderButtons(store: store, row: row)
                        .id(tab.id)
                }
            }
        ) {
            actions(for: tab, row: row)
        }
    }

    /// Back to the sessions list over a compact window. Beside the sidebar there is no list to go
    /// back to; with the sidebar hidden, the button shows it again.
    private var leading: WorkspaceHeaderLeading {
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
            Button(WorkspaceCopy.find, systemImage: "magnifyingglass") {
                finding = true
                findFocused = true
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

    private var findBar: some View {
        HStack(spacing: 4) {
            TextField(WorkspaceCopy.findPrompt, text: $findText)
                .font(.body)
                .foregroundStyle(Color.togoSnow)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .submitLabel(.search)
                .focused($findFocused)
                .onSubmit { find(backwards: false) }
                .onChange(of: findText) { findMissed = false }
            if findMissed {
                Text(WorkspaceCopy.noMatches)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .lineLimit(1)
            }
            Button(WorkspaceCopy.findPrevious, systemImage: "chevron.up") { find(backwards: true) }
                .labelStyle(.iconOnly)
                .frame(minWidth: 44, minHeight: 44)
            Button(WorkspaceCopy.findNext, systemImage: "chevron.down") { find(backwards: false) }
                .labelStyle(.iconOnly)
                .frame(minWidth: 44, minHeight: 44)
            Button(WorkspaceCopy.findDone, action: endFind)
                .font(.body.weight(.semibold))
                .frame(minHeight: 44)
        }
        .foregroundStyle(Color.togoSnow)
        .padding(.horizontal, 16)
    }

    private func find(backwards: Bool) {
        guard !findText.isEmpty, let tab = store.focusedTab,
              let search = terminals.handle(for: tab.id)?.search else { return }
        findMissed = !search.find(findText, backwards)
    }

    private func endFind() {
        guard finding else { return }
        if let tab = store.focusedTab {
            terminals.handle(for: tab.id)?.search?.clear()
        }
        finding = false
        findText = ""
        findMissed = false
    }

    // MARK: Requests and shortcuts

    /// Cards over the focused pane, clear of the fold, above the bars and the keyboard; on a
    /// tabletop they sit in the deck under the fold.
    @ViewBuilder private func requests(in geometry: PaneGeometry, insets: EdgeInsets) -> some View {
        if let deck = geometry.deck {
            TabletopDeck { incoming }
                .frame(width: deck.width, height: max(0, deck.height - insets.bottom))
                .offset(x: deck.minX, y: deck.minY)
        } else {
            let rect = geometry.requestsFrame(focused: store.workspace.focused)
            incoming
                .frame(maxWidth: 560)
                .padding(.horizontal, 12)
                .padding(.bottom, 8)
                .frame(width: rect.width, height: max(0, rect.height - insets.bottom), alignment: .bottom)
                .offset(x: rect.minX, y: rect.minY)
        }
    }

    private var incoming: some View {
        IncomingRequests(
            cards: store.incomingRequests, failures: store.answerFailures,
            chipIDs: Set(store.workspace.tabs.map(\.id)), namespace: chipSpace,
            machineName: store.machineName(for:),
            onOpen: { card in
                guard let sessionID = card.sessionID else { return }
                withAnimation(Motion.animation(.tabSettle, reduce: reduceMotion)) {
                    store.openInFocusedPane(hostID: card.hostID, sessionID: sessionID)
                }
            },
            onAnswer: { card, decision in Task { await store.answer(card.id, decision) } },
            onMore: { store.workspaceOpen = false }
        )
    }

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

private extension View {
    /// The workspace is immersive: on a Duo the system's vertical bars give way to the terminal.
    @ViewBuilder func verticalBarsOff() -> some View {
        if #available(iOS 27.1, *) {
            toolbarVerticalBehavior(.disabled)
        } else {
            self
        }
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
