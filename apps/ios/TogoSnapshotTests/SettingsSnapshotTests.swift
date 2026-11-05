import SnapshotTesting
import SwiftUI
import Testing
import TogoTerminal
@testable import Togo

@MainActor
struct SettingsSnapshotTests {
    private let values = SettingsValues.sample

    private func screen(_ page: some View) -> some View {
        NavigationStack { page }
            .togoControls()
    }

    private func page(_ page: SettingsPage, _ values: SettingsValues? = nil) -> some View {
        screen(SettingsView(values: values ?? self.values, actions: SettingsActions { _ in }).page(page))
    }

    @Test func root() {
        assertScreen(screen(SettingsView(values: values, actions: SettingsActions { _ in })), named: "settings-root")
    }

    @Test func terminal() {
        assertScreen(named: "settings-terminal") { page(.terminal) }
    }

    @Test func terminalImportError() {
        let error = ThemeImport.message(for: ThemeImportError.missingColor("ansi 3"), filename: "Dracula.itermcolors")
        assertScreen(named: "settings-terminal-import-error") {
            screen(TerminalSettingsView(settings: values.settings, fonts: values.fonts, update: { _ in }, importError: error))
        }
    }

    @Test func keyboard() {
        assertScreen(page(.keyboard), named: "settings-keyboard")
    }

    @Test func builderCtrlUp() {
        var draft = ShortcutDraft()
        draft.ctrl = true
        draft.key = .up
        assertScreen(screen(ShortcutBuilderView(draft: draft) { _ in }), named: "settings-builder")
    }

    @Test func gestures() {
        assertScreen(page(.gestures), named: "settings-gestures")
    }

    @Test func machines() {
        assertScreen(page(.machines), named: "settings-machines")
    }

    @Test func machinesChecking() {
        var checking = values
        checking.machines[0].helper = .checking
        checking.machines[1].helper = .missing
        assertScreen(page(.machines, checking), named: "settings-machines-checking")
    }

    @Test func noMachines() {
        var empty = values
        empty.machines = []
        assertScreen(page(.machines, empty), named: "settings-machines-empty")
    }

    @Test func sync() {
        assertScreen(page(.sync), named: "settings-sync")
    }

    @Test func syncICloudUnavailable() {
        var signedOut = values
        signedOut.iCloudAvailable = false
        assertScreen(page(.sync, signedOut), named: "settings-sync-unavailable")
    }

    @Test func privacy() {
        assertScreen(page(.privacy), named: "settings-privacy")
    }

    @Test func about() {
        assertScreen(page(.about), named: "settings-about")
    }

    @Test func acknowledgments() {
        assertScreen(screen(AcknowledgmentsView()), named: "settings-acknowledgments")
    }
}
