import ActivityKit
import SwiftUI
import TogoPush
import WidgetKit

/// One Live Activity per machine, started and updated by the helper through push. It shows
/// the session, its state and the request it waits on from the context sealed for this device;
/// the expanded island answers in place where one tap may, and a tap anywhere else opens the
/// session in the app. The Watch Smart Stack and CarPlay get a small form of the card.
struct TogoLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: TogoActivityAttributes.self) { context in
            let card = LiveContextSource.card(context.state, hostId: context.attributes.hostId, stale: context.isStale)
            let machine = LiveCopy.machineName(hostId: context.attributes.hostId, pairings: LiveCopy.sharedPairings())
            LiveActivityContent(
                state: context.state, card: card, hostId: context.attributes.hostId, isStale: context.isStale
            )
                .activityBackgroundTint(Color.togoInk)
                .activitySystemActionForegroundColor(Color.togoSnow)
                .widgetURL(LiveCopy.link(bib: context.state.bib, machine: machine))
        } dynamicIsland: { context in
            let card = LiveContextSource.card(context.state, hostId: context.attributes.hostId, stale: context.isStale)
            let machine = LiveCopy.machineName(hostId: context.attributes.hostId, pairings: LiveCopy.sharedPairings())
            let link = LiveCopy.link(bib: context.state.bib, machine: machine)
            return DynamicIsland {
                // One header row: the mark, title and state lead, the clock trails. Both drop
                // below the camera together when the title will not fit beside it.
                DynamicIslandExpandedRegion(.leading, priority: 1) {
                    IslandExpandedLeading(state: context.state, card: card, isStale: context.isStale)
                        .dynamicIsland(verticalPlacement: .belowIfTooWide)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    IslandExpandedTrailing(state: context.state, isStale: context.isStale)
                        .dynamicIsland(verticalPlacement: .belowIfTooWide)
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
        // The Watch Smart Stack and CarPlay draw the small family; without it they rebuild the
        // view from the island's expanded regions.
        .supplementalActivityFamilies([.small])
    }
}

/// The lock screen card, or its small form on the Watch and in CarPlay.
private struct LiveActivityContent: View {
    @Environment(\.activityFamily) private var family
    let state: TogoActivityAttributes.ContentState
    let card: LiveCard
    let hostId: String
    let isStale: Bool

    var body: some View {
        switch family {
        case .small:
            LiveSmallView(state: state, card: card, isStale: isStale)
        default:
            LiveLockScreenView(state: state, card: card, hostId: hostId, isStale: isStale)
        }
    }
}
