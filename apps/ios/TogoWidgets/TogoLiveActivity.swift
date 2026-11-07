import ActivityKit
import SwiftUI
import TogoPush
import WidgetKit

/// One Live Activity per machine, started and updated by the helper through push. It shows
/// the session, its state and the request it waits on from the context sealed for this device;
/// the expanded island answers in place where one tap may, and a tap anywhere else opens the
/// session in the app.
struct TogoLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: TogoActivityAttributes.self) { context in
            let card = LiveContextSource.card(context.state, hostId: context.attributes.hostId, stale: context.isStale)
            let machine = LiveCopy.machineName(hostId: context.attributes.hostId, pairings: LiveCopy.sharedPairings())
            LiveLockScreenView(state: context.state, card: card, isStale: context.isStale)
                .activityBackgroundTint(Color.togoInk)
                .activitySystemActionForegroundColor(Color.togoSnow)
                .widgetURL(LiveCopy.link(bib: context.state.bib, machine: machine))
        } dynamicIsland: { context in
            let card = LiveContextSource.card(context.state, hostId: context.attributes.hostId, stale: context.isStale)
            let machine = LiveCopy.machineName(hostId: context.attributes.hostId, pairings: LiveCopy.sharedPairings())
            let link = LiveCopy.link(bib: context.state.bib, machine: machine)
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    IslandExpandedLeading(card: card)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    IslandExpandedTrailing(state: context.state, isStale: context.isStale)
                }
                DynamicIslandExpandedRegion(.center) {
                    IslandExpandedCenter(state: context.state, card: card, isStale: context.isStale)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    IslandExpandedBottom(
                        state: context.state, card: card, hostId: context.attributes.hostId, link: link,
                        isStale: context.isStale
                    )
                }
            } compactLeading: {
                IslandCompactLeading(card: card)
            } compactTrailing: {
                IslandCompactTrailing(state: context.state, card: card, isStale: context.isStale)
            } minimal: {
                IslandMinimal(state: context.state, card: card, isStale: context.isStale)
            }
            .widgetURL(link)
        }
    }
}
