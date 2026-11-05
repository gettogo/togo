import SwiftUI

/// What the app knows about its own health, for reporting a problem.
struct DiagnosticsView: View {
    var body: some View {
        Text(settingsText("settings.diagnostics.title", "Diagnostics"))
    }
}
