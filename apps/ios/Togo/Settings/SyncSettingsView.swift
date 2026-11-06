import SwiftUI
import TogoKit

struct SyncSettingsView: View {
    let settings: AppSettings
    let iCloudAvailable: Bool
    let update: (AppSettings) -> Void

    var body: some View {
        List {
            Section {
                Toggle(
                    settingsText("settings.sync.toggle", "Sync machines and settings with iCloud"),
                    isOn: iCloudAvailable ? settings.binding(\.syncEnabled, update) : .constant(false)
                )
                .disabled(!iCloudAvailable)
                .frame(minHeight: 44)
            } footer: {
                VStack(alignment: .leading, spacing: 6) {
                    if !iCloudAvailable {
                        Text(settingsText("settings.sync.unavailable", "Sign in to iCloud in Settings to sync."))
                    }
                    Text(settingsText("settings.sync.footer", "Private keys stay on this \(DeviceName.model)."))
                }
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("settings.sync.title", "iCloud sync"))
        .navigationBarTitleDisplayMode(.inline)
    }
}
