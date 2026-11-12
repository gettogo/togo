import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// The helper release row on a machine's edit screen, at the longest version text and the largest type.
/// The Control Center note is the single fixed line "Update available", so it has no name or
/// version to lengthen it.
@MainActor
struct HelperUpdateSnapshotTests {
    private static let machine = Samples.studio
    private static let sizes: [ScreenSize] = [.iPhone17, .duoFolded]

    private func updates(
        latest: String? = "v1.0.11", installed: String = "v1.0.10", homebrew: Bool = false, phase: HelperUpdates.Phase = .idle
    ) -> HelperUpdates {
        let suite = "helper-update-snapshots"
        let defaults = UserDefaults(suiteName: suite) ?? .standard
        defaults.removePersistentDomain(forName: suite)
        let state = HelperUpdateState(checkedAt: .now, latest: latest, installed: installed, homebrew: homebrew)
        let ledger = HelperUpdateLedger(machines: [Self.machine: state])
        defaults.set(try? JSONEncoder().encode(ledger), forKey: "helperUpdates.v1")
        let model = HelperUpdates(defaults: defaults)
        model.show(phase, for: Self.machine)
        return model
    }

    /// The screen a tap on a machine in Settings > Machines opens, with the update row at its top.
    private func page(_ updates: HelperUpdates, name: String = "Studio") -> some View {
        var draft = MachineDraft()
        draft.machineID = Self.machine
        draft.name = name
        draft.address = "studio.local"
        draft.user = "dev"
        return NavigationStack {
            MachineForm(draft: .constant(draft), showsErrors: false, importError: nil, onImportKey: { _, _ in }, onConnect: {})
                .navigationTitle(name)
                .navigationBarTitleDisplayMode(.inline)
        }
        .environment(\.helperUpdates, updates)
        .togoControls()
    }

    @Test func available() {
        assertScreen(named: "helper-update-available", sizes: Self.sizes) { page(updates()) }
    }

    @Test func updating() {
        assertScreen(named: "helper-update-updating", sizes: Self.sizes) { page(updates(phase: .updating)) }
    }

    @Test func failedWithLongText() {
        assertScreen(named: "helper-update-failed", sizes: Self.sizes) {
            page(updates(phase: .failed("download failed: the release server did not answer before the time limit ran out")))
        }
    }

    @Test func homebrew() {
        assertScreen(named: "helper-update-homebrew", sizes: Self.sizes) { page(updates(latest: nil, homebrew: true)) }
    }
}
