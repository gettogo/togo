import SwiftUI
import TogoKit

struct PrivacySettingsView: View {
    let settings: AppSettings
    /// From `BiometryName`, so iPhones with Touch ID never read "Face ID".
    let biometry: String
    let update: (AppSettings) -> Void

    /// `BiometryName` falls back to "passcode", which needs an article in a sentence.
    private var unlockWord: String {
        biometry == "passcode" ? settingsText("settings.privacy.yourPasscode", "your passcode") : biometry
    }

    var body: some View {
        List {
            Section {
                Toggle(
                    settingsText("settings.privacy.lock", "Lock with \(biometry)"),
                    isOn: settings.binding(\.resumeLock, update)
                )
                .frame(minHeight: 44)
            } footer: {
                Text(settingsText(
                    "settings.privacy.lock.footer",
                    "Togo asks for \(unlockWord) when you come back to it. Sessions are always hidden in the app switcher."
                ))
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("settings.privacy.title", "Privacy"))
        .navigationBarTitleDisplayMode(.inline)
    }
}
