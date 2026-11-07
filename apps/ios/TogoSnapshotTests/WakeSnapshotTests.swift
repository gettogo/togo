import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct WakeSnapshotTests {
    private static let now = Samples.now
    private static let target = WakeTarget(mac: "a4:83:e7:00:11:22", broadcast: ["192.168.1.255", "255.255.255.255"], enabled: true)

    private func machine(wake: WakeTarget?) -> MachineSection {
        MachineSection(
            id: Samples.studio, name: "Studio", symbol: .desktop,
            reachability: .offline(since: Self.now.addingTimeInterval(-720), reason: "Asleep"),
            helper: .present(version: "0.3.0", live: true), sessions: [], lastSeen: Self.now.addingTimeInterval(-720),
            wake: wake
        )
    }

    private func page(_ machine: MachineSection, note: String? = nil) -> some View {
        NavigationStack {
            List {
                RosterSection(
                    group: RosterModel.make(machines: [machine], requests: []).groups[0], now: Self.now,
                    wakeEnabled: true, wakeNote: note
                ) { _ in }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .background(Color.togoInk)
            .navigationBarTitleDisplayMode(.inline)
        }
        .togoControls()
    }

    @Test func offlineWithWake() {
        assertScreen(named: "wake-offline") { page(machine(wake: Self.target)) }
    }

    @Test func offlineAfterSending() {
        assertScreen(named: "wake-sent") {
            page(machine(wake: Self.target), note: WakeCopy.sent("Studio"))
        }
    }

    @Test func offlineWithoutATarget() {
        assertScreen(named: "wake-none") { page(machine(wake: nil)) }
    }
}
