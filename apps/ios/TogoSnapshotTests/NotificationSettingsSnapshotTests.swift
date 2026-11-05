import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct NotificationSettingsSnapshotTests {
    private func screen(
        _ authorization: PushRegistrar.Authorization, settings: AppSettings = AppSettings(), relayDraft: String? = nil
    ) -> some View {
        var values = NotificationValues.sample
        values.authorization = authorization
        return NavigationStack {
            NotificationSettingsView(
                settings: settings, notifications: values, update: { _ in }, turnOn: {}, relayDraft: relayDraft
            )
        }
        .togoControls()
    }

    @Test func authorized() {
        var settings = AppSettings()
        settings.liveActivityCommandMachines = [Samples.studio]
        assertScreen(named: "settings-notifications-on") { screen(.authorized, settings: settings) }
    }

    /// The end of the page: each machine's state and its command text switch. Studio has not
    /// yet confirmed the choice that was just turned on.
    @Test func machines() {
        var settings = AppSettings()
        settings.liveActivityCommandMachines = [Samples.studio]
        let machines = [
            NotificationMachine(id: Samples.studio, name: "Studio", state: .paired, helperCommand: false),
            NotificationMachine(id: Samples.build, name: "Build box", state: .helperTooOld),
            NotificationMachine(id: UUID(), name: "Mac mini", state: .notPaired),
            NotificationMachine(id: UUID(), name: "Pi", state: .needsHelper),
            NotificationMachine(id: UUID(), name: "Laptop", state: .offline),
        ]
        assertScreen(named: "settings-notifications-machines") {
            NavigationStack {
                List { NotificationMachinesSection(machines: machines, settings: settings) { _, _ in } }
                    .groupedList()
                    .navigationTitle("Notifications")
                    .navigationBarTitleDisplayMode(.inline)
            }
            .togoControls()
        }
    }

    @Test func denied() {
        assertScreen(named: "settings-notifications-denied") { screen(.denied) }
    }

    @Test func notDetermined() {
        assertScreen(named: "settings-notifications-not-asked") { screen(.notDetermined) }
    }

    @Test func customRelayInvalid() {
        assertScreen(named: "settings-notifications-relay-invalid") { screen(.authorized, relayDraft: "http://relay.example.com") }
    }
}
