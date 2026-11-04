import ActivityKit
import SwiftUI
import WidgetKit

// The extension exists now so bundle ids, the app group and provisioning are
// settled early. It renders nothing until the app starts Live Activities,
// and it offers no gallery widget a user could add.
@main
struct TogoWidgets: WidgetBundle {
    var body: some Widget {
        PlaceholderActivity()
    }
}

private struct PlaceholderAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {}
}

private struct PlaceholderActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: PlaceholderAttributes.self) { _ in
            EmptyView()
        } dynamicIsland: { _ in
            DynamicIsland {
                DynamicIslandExpandedRegion(.center) { EmptyView() }
            } compactLeading: {
                EmptyView()
            } compactTrailing: {
                EmptyView()
            } minimal: {
                EmptyView()
            }
        }
    }
}
