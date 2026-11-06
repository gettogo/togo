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

    @Test func machinesRemoving() {
        var removing = values
        removing.machines = [values.machines[0]]
        removing.pendingRemovals = [PendingRemoval(id: "h_build", name: "Build box")]
        assertScreen(named: "settings-machines-removing") { page(.machines, removing) }
    }

    @Test func machinesRemovingKeys() {
        var removing = values
        removing.machines = [values.machines[0]]
        removing.pendingRemovals = [
            PendingRemoval(id: "h_build", name: "Build box"),
            PendingRemoval(
                id: PendingRemoval.keyID(Samples.build), name: "Build box",
                kind: .key(comment: "togo@Sams-iPhone", manual: false)
            ),
            PendingRemoval(
                id: PendingRemoval.keyID(Samples.studio), name: "Mini",
                kind: .key(comment: "togo@Sams-iPhone", manual: true)
            ),
        ]
        assertScreen(named: "settings-machines-removing-keys") { page(.machines, removing) }
    }

    @Test func removeMachineQuestion() {
        var machine = values.machines[0]
        machine.removesKey = true
        assertScreen(named: "settings-machines-remove-question") {
            screen(MachinesSettingsView(machines: values.machines, isPad: false, asking: machine))
        }
    }

    @Test func removeMachineQuestionOnIPad() {
        var machine = values.machines[0]
        machine.removesKey = true
        assertScreen(named: "settings-machines-remove-question-ipad") {
            screen(MachinesSettingsView(machines: values.machines, isPad: true, asking: machine))
        }
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

@MainActor
struct ReportProblemSnapshotTests {
    private let issue = IssueLink.url(app: "1.0 (42)", os: "27.2", device: "iPhone", helpers: ["0.3.0"])

    @Test func page() {
        assertScreen(named: "settings-report-problem") {
            NavigationStack {
                ReportProblemPage(file: URL(fileURLWithPath: "/tmp/togo-diagnostics-2026-11-05.json"), issue: issue)
            }.togoControls()
        }
    }

    @Test func exportNotReady() {
        assertScreen(named: "settings-report-problem-waiting") {
            NavigationStack { ReportProblemPage(file: nil, issue: issue) }.togoControls()
        }
    }
}
