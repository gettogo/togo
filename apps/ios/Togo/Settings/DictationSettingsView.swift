import SwiftUI

/// How the microphone button turns speech into text on this device.
struct DictationSettingsView: View {
    var body: some View {
        Text(settingsText("settings.dictation.title", "Dictation"))
    }
}
