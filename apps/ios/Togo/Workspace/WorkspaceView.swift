import SwiftUI
import TogoKit
import UIKit

/// Where the user spends most of their time: the selected session's terminal edge to edge
/// on an opaque canvas, the header and tab strip above it, and requests from other sessions
/// dropping in above the key bar.
struct WorkspaceView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.displayScale) private var displayScale
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

    init(store: AppStore, terminals: TerminalPool, noteShown: Bool = false) {
        self.store = store
        self.terminals = terminals
        _noteShown = State(initialValue: noteShown)
    }

    var body: some View {
        pager
            .overlay(alignment: .bottom) { incoming }
            // The tip anchors behind the chrome so its popover never changes the chrome's layout.
            .safeAreaInset(edge: .top, spacing: 0) { chrome.background { Color.clear.tutorialTip(.workspace) } }
            .safeAreaInset(edge: .bottom, spacing: 0) { WorkspaceComposeBar(store: store, terminals: terminals) }
            .background(Color.togoInk)
            .background { shortcuts }
            // Drags on the terminal scroll its history, so a pull down must not close the workspace.
            .interactiveDismissDisabled()
            .attachPrompts(store.services.attachPrompts, active: true)
            .onChange(of: store.workspace.selected) {
                status = nil
                store.composing = nil
                noteShown = false
                endFind()
            }
            .haptic(.tabSettle, trigger: store.workspace.selected)
            .onChange(of: store.findRequests) {
                guard let tab = store.selectedTab, terminals.handle(for: tab.id)?.search != nil else { return }
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
    }

    // MARK: Pager

    /// The terminal sizes itself to the keyboard and the home indicator, so the pager ignores
    /// both and the grid is resized once, by the terminal.
    @ViewBuilder private var pager: some View {
        Group {
            if reduceMotion {
                fadingPager
            } else {
                TabView(selection: selection) {
                    ForEach(store.workspace.tabs) { tab in
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
            ForEach(store.workspace.tabs) { tab in
                if tab.id == store.workspace.selected {
                    page(tab)
                        .transition(.opacity)
                }
            }
        }
        .animation(Motion.fade, value: store.workspace.selected)
        .simultaneousGesture(
            DragGesture(minimumDistance: 24).onEnded { drag in
                let width = drag.translation.width
                guard abs(width) > 60, abs(width) > abs(drag.translation.height) * 2 else { return }
                store.workspace.ended = nil
                store.selectTab(offset: width < 0 ? 1 : -1)
            }
        )
    }

    private func page(_ tab: WorkspaceTab) -> some View {
        let overlay = ConnectingOverlay.Kind(
            link: tab.link, machine: store.machine(for: tab.hostID), name: store.machineName(for: tab.hostID)
        )
        let isSelected = tab.id == store.workspace.selected
        let reconnecting = tab.link.isReconnecting
        return ZStack {
            Color.togoInk
            if let handle = terminals.handle(for: tab.id) {
                TabTerminalView(handle: handle)
                    .opacity(reconnecting ? 0.6 : 1)
            }
            if let overlay {
                ConnectingOverlay(kind: overlay) { retry(tab) }
                    .transition(.opacity)
            }
        }
        .animation(Motion.fade, value: overlay)
        .animation(Motion.fade, value: reconnecting)
        .haptic(.connect, trigger: tab.link.isLive) { $0 && isSelected }
    }

    private var selection: Binding<TabID?> {
        Binding {
            store.workspace.selected
        } set: { id in
            guard let id else { return }
            store.workspace.ended = nil
            store.select(id)
        }
    }

    /// Chip taps and shortcuts settle like a swipe does. Under Reduce Motion the pager jumps and
    /// the pages fade.
    private func show(_ id: TabID) {
        store.workspace.ended = nil
        withAnimation(reduceMotion ? nil : Motion.tabSettle) {
            store.select(id)
        }
    }

    private func retry(_ tab: WorkspaceTab) {
        terminals.restart(tab, store: store)
        Task { await store.services.refresh() }
    }

    // MARK: Chrome

    private var chrome: some View {
        VStack(spacing: 0) {
            if let tab = store.selectedTab {
                header(tab)
            }
            TabStrip(
                chips: chips, selected: store.workspace.selected, namespace: chipSpace,
                onSelect: show, onClose: { store.close($0) }
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
            bib: row?.bib ?? tab.bib, title: name, link: tab.link, status: status ?? store.workspace.ended, noteShown: $noteShown,
            onBack: { store.workspaceOpen = false }
        ) {
            actions(for: tab, row: row)
        }
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
        guard !findText.isEmpty, let tab = store.selectedTab,
              let search = terminals.handle(for: tab.id)?.search else { return }
        findMissed = !search.find(findText, backwards)
    }

    private func endFind() {
        guard finding else { return }
        if let tab = store.selectedTab {
            terminals.handle(for: tab.id)?.search?.clear()
        }
        finding = false
        findText = ""
        findMissed = false
    }

    // MARK: Requests and shortcuts

    private var incoming: some View {
        IncomingRequests(
            cards: store.incomingRequests, failures: store.answerFailures,
            chipIDs: Set(store.workspace.tabs.map(\.id)), namespace: chipSpace,
            machineName: store.machineName(for:),
            onOpen: { card in
                guard let sessionID = card.sessionID else { return }
                withAnimation(reduceMotion ? nil : Motion.tabSettle) {
                    store.open(hostID: card.hostID, sessionID: sessionID)
                }
            },
            onAnswer: { card, decision in Task { await store.answer(card.id, decision) } },
            onMore: { store.workspaceOpen = false }
        )
        .frame(maxWidth: 560)
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
    }

    /// ⌘1 to ⌘9 while the terminal does not have focus. With focus the terminal claims the
    /// keys first and passes them up through `TerminalHandle.onShortcut`.
    private var shortcuts: some View {
        ForEach(Array(chips.prefix(9).enumerated()), id: \.element.id) { index, chip in
            Button(WorkspaceCopy.goTo([chip.bib, chip.name].compactMap(\.self).joined(separator: " "))) {
                show(chip.id)
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

private extension LinkPhase {
    var isLive: Bool {
        if case .live = self { true } else { false }
    }

    var isReconnecting: Bool {
        if case .reconnecting = self { true } else { false }
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
