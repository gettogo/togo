import Foundation
import OSLog
import TogoKit

/// Only fixed categories reach the log. Original labels, fields and values
/// may contain credentials or conversation content.
struct NativeCommandFailure {
    let category: String
    let message: String

    init(_ error: Error) {
        switch error {
        case NativePickerScreen.Unsupported.unknownActions:
            category = "picker-actions"
            message = String(localized: "native.failure.actions", defaultValue: "These actions couldn't open. Close this screen and try the command again.")
        case NativePickerScreen.Unsupported.nestedControl:
            category = "picker-follow-up"
            message = String(localized: "native.failure.followUp", defaultValue: "The next step couldn't open. Your selection is still waiting in Terminal.")
        case NativePickerScreen.Unsupported.malformedItem:
            category = "picker-item"
            message = String(localized: "native.failure.items", defaultValue: "This list couldn't load. Close this screen and try the command again.")
        case NativeStructuredScreen.Unsupported.field:
            category = "settings-field"
            message = String(localized: "native.failure.field", defaultValue: "This field couldn't open. Its value hasn't been changed.")
        case NativeStructuredScreen.Unsupported.feature:
            category = "companion-feature"
            message = String(localized: "native.failure.update", defaultValue: "Update this machine’s helper to finish this command here.")
        case is ChatSurfaceInteraction.Failure:
            category = "interaction-changed"
            message = SurfaceCopy.inputChanged
        default:
            category = "screen-content"
            message = String(localized: "native.failure.content", defaultValue: "This screen couldn't load. Close it and try the command again.")
        }
    }

    func log() {
        Logger(subsystem: "app.togo", category: "native-command")
            .error("Native command mapping failed: \(category, privacy: .public)")
    }
}
