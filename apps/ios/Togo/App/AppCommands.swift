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
                .disabled(store?.services.hosts.isEmpty ?? true)
            // Closing a tab detaches; the session keeps running on its machine.
            Button(String(localized: "cmd.closeTab", defaultValue: "Close Tab")) { store?.perform(.detach) }
                .keyboardShortcut("w", modifiers: .command)
                .disabled(!(store?.workspaceOpen ?? false))
            Button(String(localized: "cmd.find", defaultValue: "Find")) { store?.requestFind() }
                .keyboardShortcut("f", modifiers: .command)
                .disabled(!(store?.workspaceOpen ?? false))
            Divider()
            ForEach(1...9, id: \.self) { number in
                Button(String(localized: "cmd.tab", defaultValue: "Tab \(number)")) { store?.showTab(number: number) }
                    .keyboardShortcut(KeyEquivalent(Character(String(number))), modifiers: .command)
                    .disabled((store?.workspace.tabs.count ?? 0) < number)
            }
        }
    }
}

extension FocusedValues {
    @Entry var appStore: AppStore?
}

extension AppStore {
    /// Back to the sessions list from anywhere. Tabs stay attached.
    func showControlCenter() {
        workspaceOpen = false
        sheet = nil
        path = []
    }

    /// ⌘T: the new session sheet, aimed at the machine of the tab on screen.
    func startNewSession() {
        guard !services.hosts.isEmpty else { return }
        showSheet(.newSession(workspaceOpen ? selectedTab?.hostID : nil))
    }

    /// ⌘1 to ⌘9 work from the sessions list too, opening the workspace on that tab.
    func showTab(number: Int) {
        guard workspace.tabs.indices.contains(number - 1) else { return }
        selectTab(number: number)
        workspaceOpen = true
    }

    func requestFind() {
        guard workspaceOpen else { return }
        findRequests += 1
    }

    /// Sheets hang off the sessions list, under the workspace cover, so the cover closes first
    /// and the sheet follows once it is gone.
    func showSheet(_ sheet: AppSheet) {
        guard workspaceOpen else {
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
