import AppIntents
import SwiftUI
import WidgetKit

/// A Control Center and Action button control that opens Togo on the request waiting on you.
/// It runs on the Watch and never hands off to the iPhone.
struct WaitingControl: ControlWidget {
    static let kind = "togo.watch.control.waiting"

    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: Self.kind) {
            ControlWidgetButton(action: OpenWaitingIntent()) {
                Label(
                    String(localized: "watch.control.title", defaultValue: "Togo Waiting"),
                    systemImage: "circle.inset.filled"
                )
            }
        }
        .displayName(LocalizedStringResource("watch.control.name", defaultValue: "Togo Waiting"))
        .description(LocalizedStringResource("watch.control.description", defaultValue: "Open the request waiting on you."))
    }
}
