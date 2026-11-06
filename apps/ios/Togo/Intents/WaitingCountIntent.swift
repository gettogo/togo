import AppIntents
import Foundation
import TogoPush

/// Says how many requests are waiting, from the requests the waiting widget counts. It runs
/// without opening the app and without reaching a machine.
struct WaitingCountIntent: AppIntent {
    static let title = LocalizedStringResource("intent.waiting.title", defaultValue: "What's Waiting")
    static let description = IntentDescription(LocalizedStringResource(
        "intent.waiting.description", defaultValue: "Says how many requests are waiting for you."
    ))
    static let openAppWhenRun = false

    @MainActor func perform() async throws -> some IntentResult & ReturnsValue<Int> & ProvidesDialog {
        let hub = IntentHub.current
        let count = Self.count(hub.requests(), now: hub.clock())
        return .result(value: count, dialog: IntentDialog("\(Self.message(count))"))
    }

    /// Requests still open, by the same rule as the widget.
    static func count(_ requests: [PendingRequest], now: Date) -> Int {
        WaitingSummary.open(requests, now: now).count
    }

    static func message(_ count: Int) -> String {
        count == 0
            ? String(localized: "intent.waiting.none", defaultValue: "Nothing is waiting.")
            : String(localized: "intent.waiting.count", defaultValue: "\(count) requests are waiting.")
    }
}
