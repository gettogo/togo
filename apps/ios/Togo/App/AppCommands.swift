import SwiftUI
import TogoKit

/// Hardware keyboard shortcuts, listed in the iPad menu bar and the ⌘ overlay. While the
/// terminal has focus it claims the same keys first and passes them up through
/// `TerminalHandle.onShortcut`, so both paths end in the same store actions.
struct AppCommands: Commands {
    @FocusedValue(\.appStore) private var store

    var body: some Commands {
        CommandMenu(String(localized: "cmd.menu", defaultValue: "Sessions")) {
            Button(String(localized: "cmd.controlCenter", defaultValue: "Control Center")) { store?.showControlCenter() }
                .keyboardShortcut("k", modifiers: .command)
            Button(String(localized: "cmd.newSession", defaultValue: "New Session")) { store?.startNewSession() }
                .keyboardShortcut("t", modifiers: .command)
                .disabled((store?.services.hosts.isEmpty ?? true) || reviewOpen)
            // Closing a tab detaches; the session keeps running on its machine. Over the review,
            // it closes the review.
            Button(String(localized: "cmd.closeTab", defaultValue: "Close Tab")) { store?.perform(.detach) }
                .keyboardShortcut("w", modifiers: .command)
                .disabled(!(store?.workspaceOpen ?? false))
            Button(String(localized: "cmd.find", defaultValue: "Find")) { store?.requestFind() }
                .keyboardShortcut("f", modifiers: .command)
                .disabled(!(store?.workspaceOpen ?? false) || reviewOpen)
            Divider()
            // The review screens for the session on screen.
            Button(String(localized: "cmd.changes", defaultValue: "Changes")) { store?.openReview(.changes) }
                .keyboardShortcut("d", modifiers: [.command, .shift])
                .disabled(!(store?.canOpenReview(.changes) ?? false))
            Button(String(localized: "cmd.files", defaultValue: "Files")) { store?.openReview(.files) }
                .keyboardShortcut("o", modifiers: [.command, .shift])
                .disabled(!(store?.canOpenReview(.files) ?? false))
            Button(String(localized: "cmd.ports", defaultValue: "Ports")) { store?.openReview(.ports) }
                .keyboardShortcut("p", modifiers: [.command, .shift])
                .disabled(!(store?.canOpenReview(.ports) ?? false))
            Divider()
            panes
            Divider()
            ForEach(1...9, id: \.self) { number in
                Button(String(localized: "cmd.tab", defaultValue: "Tab \(number)")) { store?.showTab(number: number) }
                    .keyboardShortcut(KeyEquivalent(Character(String(number))), modifiers: .command)
                    .disabled((store?.workspace.tabs.count ?? 0) < number || reviewOpen)
            }
        }
    }

    /// The sidebar, the second pane and moving between tabs and panes.
    @ViewBuilder private var panes: some View {
        let inline = store?.workspaceInline ?? false
        let hidden = store?.sidebar.isHidden ?? false
        Button(hidden ? AppCommandCopy.showSidebar : AppCommandCopy.hideSidebar) { store?.toggleSidebar() }
            .keyboardShortcut("s", modifiers: [.command, .control])
            .disabled(!inline)
        Toggle(BoardCopy.toggle, isOn: Binding(get: { store?.boardShown ?? false }, set: { _ in store?.toggleBoard() }))
            .keyboardShortcut("b", modifiers: [.command, .control])
            .disabled(store == nil || reviewOpen)
        Toggle(String(localized: "cmd.zenMode", defaultValue: "Zen Mode"), isOn: Binding(get: { store?.zen ?? false }, set: { _ in store?.toggleZen() }))
            .keyboardShortcut("f", modifiers: [.command, .control])
            .disabled(!zenAvailable || reviewOpen)
        let split = store?.workspace.secondary != nil
        Button(split ? AppCommandCopy.closePane : AppCommandCopy.openBeside) { store?.toggleSplit() }
            .keyboardShortcut("\\", modifiers: .command)
            .disabled(!(store?.canToggleSplit ?? false))
        let panesShown = (store?.panesVisible ?? false) && split
        Button(AppCommandCopy.focusLeft) { store?.perform(.focusLeft) }
            .keyboardShortcut(.leftArrow, modifiers: [.command, .option])
            .disabled(!panesShown || reviewOpen)
        Button(AppCommandCopy.focusRight) { store?.perform(.focusRight) }
            .keyboardShortcut(.rightArrow, modifiers: [.command, .option])
            .disabled(!panesShown || reviewOpen)
        let tabs = store?.workspace.tabs.count ?? 0
        let open = store?.workspaceOpen ?? false
        Button(AppCommandCopy.previousTab) { store?.perform(.previousTab) }
            .keyboardShortcut("[", modifiers: [.command, .shift])
            .disabled(!open || tabs < 2 || reviewOpen)
        Button(AppCommandCopy.nextTab) { store?.perform(.nextTab) }
            .keyboardShortcut("]", modifiers: [.command, .shift])
            .disabled(!open || tabs < 2 || reviewOpen)
    }

    private var reviewOpen: Bool { store?.reviewing != nil }

    /// Zen needs the workspace on screen, not open behind the board.
    private var zenAvailable: Bool {
        guard let store else { return false }
        return store.zen || (store.workspaceOpen && !store.workspaceBehindBoard)
    }
}

enum AppCommandCopy {
    static let showSidebar = String(localized: "sidebar.cmd.show", defaultValue: "Show Sidebar")
    static let hideSidebar = String(localized: "sidebar.cmd.hide", defaultValue: "Hide Sidebar")
    static let openBeside = String(localized: "sidebar.cmd.openBeside", defaultValue: "Open Beside")
    static let closePane = String(localized: "sidebar.cmd.closePane", defaultValue: "Close Pane")
    static let focusLeft = String(localized: "sidebar.cmd.focusLeft", defaultValue: "Focus Left Pane")
    static let focusRight = String(localized: "sidebar.cmd.focusRight", defaultValue: "Focus Right Pane")
    static let previousTab = String(localized: "sidebar.cmd.previousTab", defaultValue: "Previous Tab")
    static let nextTab = String(localized: "sidebar.cmd.nextTab", defaultValue: "Next Tab")
}

extension FocusedValues {
    @Entry var appStore: AppStore?
}

extension AppStore {
    /// ⌘K. Beside the sidebar it shows the sidebar and puts the cursor in its search; in a
    /// compact window it goes back to the sessions list. Tabs stay attached either way.
    func showControlCenter() {
        if workspaceInline {
            sidebar.hidden = false
            sidebar.searchRequests += 1
            return
        }
        workspaceOpen = false
        sheet = nil
        path = []
    }

    /// ⌘T: the new session sheet, aimed at the machine of the tab on screen.
    func startNewSession() {
        guard !services.hosts.isEmpty, reviewing == nil else { return }
        showSheet(.newSession(workspaceOpen ? focusedTab?.hostID : nil))
    }

    /// ⌘1 to ⌘9 work from the sessions list too, opening the workspace on that tab.
    func showTab(number: Int) {
        guard reviewing == nil, workspace.tabs.indices.contains(number - 1) else { return }
        selectTab(number: number)
        workspaceOpen = true
        if workspaceInline {
            boardShown = false
            path = []
        }
    }

    func requestFind() {
        guard workspaceOpen, reviewing == nil else { return }
        findRequests += 1
    }

    /// ⌃⌘S. Only a regular-width window has a sidebar to show.
    func toggleSidebar() {
        guard workspaceInline else { return }
        sidebar.toggle()
    }

    /// ⌘\ needs a window wide enough for two panes and a second tab to put beside the first.
    var canToggleSplit: Bool {
        workspaceInline && !workspaceBehindBoard && reviewing == nil && (panesFit || workspace.secondary != nil) && (workspace.secondary != nil || workspace.tabs.count > 1)
    }

    /// ⌘\: closes the second pane, or opens the tab after the focused one beside it. The only
    /// way a second pane opens without a drag; nothing opens one on its own.
    func toggleSplit() {
        guard canToggleSplit else { return }
        if workspace.secondary != nil {
            closeSecondary()
            return
        }
        guard let current = workspace.tabs.firstIndex(where: { $0.id == workspace.selected }) else { return }
        let next = workspace.tabs[(current + 1) % workspace.tabs.count].id
        openBeside(next)
        workspaceOpen = true
    }

    /// ⇧⌘[ and ⇧⌘]: the focused pane shows the tab before or after its own, skipping the one in
    /// the other pane, and stops at either end of the strip.
    func stepTab(_ offset: Int) {
        guard workspaceOpen, let current = focusedTab?.id,
              let index = workspace.tabs.firstIndex(where: { $0.id == current }) else { return }
        let twoPanes = panesVisible && workspace.secondary != nil
        let other = workspace.focused == .primary ? workspace.secondary : workspace.selected
        var target = index + offset
        while workspace.tabs.indices.contains(target) {
            let id = workspace.tabs[target].id
            if !twoPanes || id != other {
                place(id, in: twoPanes ? workspace.focused : .primary)
                return
            }
            target += offset
        }
    }

    /// The tabs on screen: the primary's, and the secondary's while both panes show.
    var visibleTabs: [TabID] {
        let secondary = panesVisible ? workspace.secondary : nil
        return [workspace.selected, secondary].compactMap(\.self)
    }

    /// Sheets hang off the sessions list. In a compact window that is under the workspace
    /// cover, so the cover closes first and the sheet follows once it is gone.
    func showSheet(_ sheet: AppSheet) {
        guard workspaceOpen, !workspaceInline else {
            self.sheet = sheet
            return
        }
        workspaceOpen = false
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: Self.coverDismissal)
            self?.sheet = sheet
        }
    }

    static let coverDismissal: Duration = .milliseconds(450)
}
