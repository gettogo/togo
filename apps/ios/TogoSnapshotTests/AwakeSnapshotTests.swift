import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct AwakeSnapshotTests {
    private func status(
        mode: AwakeMode, held: Bool, reason: AwakeReason, detail: String? = nil, power: String = "ac"
    ) -> AwakeStatus {
        AwakeStatus(
            mode: mode, held: held, reason: reason, detail: detail, power: power, batteryPercent: 64,
            batteryFloor: 20, platformSupported: true, needsAdmin: false
        )
    }

    private func page(_ status: AwakeStatus) -> some View {
        let model = MachineAwakeModel(host: Samples.studio, name: "Studio", tools: NoMachineTools(), status: status)
        return NavigationStack {
            List { MachineAwakeRows(model: model, name: "Studio") }
                .groupedList()
                .navigationTitle("Studio")
                .navigationBarTitleDisplayMode(.inline)
        }
        .togoControls()
    }

    @Test func pluggedAndHeld() {
        assertScreen(named: "awake-plugged-held") { page(status(mode: .plugged, held: true, reason: .active)) }
    }

    @Test func graceNote() {
        assertScreen(named: "awake-grace") { page(status(mode: .plugged, held: true, reason: .grace)) }
    }

    @Test func onBattery() {
        assertScreen(named: "awake-on-battery") {
            page(status(mode: .plugged, held: false, reason: .onBattery, power: "battery"))
        }
    }

    @Test func failedDetailWraps() {
        assertScreen(named: "awake-failed") {
            page(status(
                mode: .battery, held: false, reason: .failed,
                detail: "disablesleep: sudo: a password is required for this command to run unattended"
            ))
        }
    }

    @Test func loadingBeforeTheFirstRead() {
        assertScreen(named: "awake-loading") {
            let model = MachineAwakeModel(host: Samples.studio, name: "Studio", tools: StalledTools(), status: nil)
            return NavigationStack {
                List { MachineAwakeRows(model: model, name: "Studio") }
                    .groupedList()
                    .navigationTitle("Studio")
                    .navigationBarTitleDisplayMode(.inline)
            }
            .togoControls()
        }
    }

    @Test func batterySetup() {
        assertScreen(named: "awake-battery-setup") {
            NavigationStack {
                List { Section { AwakeBatterySetup(name: "Studio") }.groupedRows() }
                    .groupedList()
            }
            .togoControls()
        }
    }

    @Test func headingWords() {
        var section = MachineSection(
            id: Samples.studio, name: "Studio", symbol: .desktop, reachability: .online,
            helper: .present(version: "0.3.0", live: true),
            sessions: [
                Samples.row(
                    hostID: Samples.studio, bib: "01", name: "api", agentName: "Claude Code", status: .working,
                    activity: "Editing", cwd: "~/src/api"
                ),
            ],
            lastSeen: Samples.now
        )
        section.awake = status(mode: .plugged, held: true, reason: .active)
        assertScreen(named: "awake-heading") {
            List { RosterSection(group: RosterModel.make(machines: [section], requests: []).groups[0], now: Samples.now, perform: { _ in }) }
                .listStyle(.plain)
                .togoControls()
        }
    }
}

/// Never answers, so a page that is still reading stays in that state while it is drawn.
@MainActor private final class StalledTools: MachineTools {
    func call<T: Sendable>(_ host: HostID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T {
        try await Task.sleep(for: .seconds(3600))
        throw CancellationError()
    }
}
