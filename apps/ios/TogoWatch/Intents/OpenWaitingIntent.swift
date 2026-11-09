import AppIntents
import Foundation
import TogoPush

/// Opens the Watch app on the oldest waiting request, from the Control Center control or the
/// Ultra's Action button. Built into the app and the widget extension: it leaves the link in
/// the Watch's app group, so the app takes it whichever process ran this.
struct OpenWaitingIntent: AppIntent {
    static let title = LocalizedStringResource("watch.control.intent", defaultValue: "Open Waiting Requests")
    static var description: IntentDescription {
        IntentDescription(
            LocalizedStringResource("watch.control.intent.description", defaultValue: "Shows the request waiting on you.")
        )
    }
    static let supportedModes: IntentModes = .foreground

    func perform() async throws -> some IntentResult {
        WatchLinkInbox.post(.waiting, to: UserDefaults(suiteName: SharedGroup.watch))
        await MainActor.run { NotificationCenter.default.post(name: WatchLinkInbox.posted, object: nil) }
        return .result()
    }
}
