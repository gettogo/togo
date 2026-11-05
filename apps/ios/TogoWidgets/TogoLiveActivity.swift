import ActivityKit
import SwiftUI
import TogoPush
import WidgetKit

/// One Live Activity per machine, started and updated by the helper through push. It carries
/// no buttons: a tap opens the session in the app, where the request is answered.
struct TogoLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: TogoActivityAttributes.self) { context in
            let machine = LiveCopy.machineName(hostId: context.attributes.hostId, pairings: LiveCopy.sharedPairings())
            LiveLockScreenView(state: context.state, machine: machine, isStale: context.isStale)
                .activityBackgroundTint(Color.togoInk)
                .activitySystemActionForegroundColor(Color.togoSnow)
                .widgetURL(LiveCopy.link(bib: context.state.bib, machine: machine))
        } dynamicIsland: { context in
            let machine = LiveCopy.machineName(hostId: context.attributes.hostId, pairings: LiveCopy.sharedPairings())
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    IslandExpandedLeading(state: context.state, isStale: context.isStale)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    IslandExpandedTrailing(state: context.state, isStale: context.isStale)
                }
                DynamicIslandExpandedRegion(.center) {
                    IslandExpandedCenter(state: context.state, isStale: context.isStale, machine: machine)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    IslandExpandedBottom(state: context.state, isStale: context.isStale)
                }
            } compactLeading: {
                IslandCompactLeading(state: context.state, isStale: context.isStale)
            } compactTrailing: {
                IslandCompactTrailing(state: context.state, isStale: context.isStale)
            } minimal: {
                IslandMinimal(state: context.state, isStale: context.isStale)
            }
            .widgetURL(LiveCopy.link(bib: context.state.bib, machine: machine))
        }
    }
}
