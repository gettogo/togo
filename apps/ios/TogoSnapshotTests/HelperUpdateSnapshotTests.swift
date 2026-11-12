import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// The helper release row on a machine's page, at the longest version text and the largest type.
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

    private func page(_ updates: HelperUpdates, name: String = "Studio") -> some View {
        NavigationStack {
            List { HelperUpdateSection(machine: Self.machine, name: name) }
                .groupedList()
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
