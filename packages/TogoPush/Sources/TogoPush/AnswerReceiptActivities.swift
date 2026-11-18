#if canImport(ActivityKit) && os(iOS)
import ActivityKit
import Foundation

public enum AnswerReceiptActivities {
    /// A late receipt may redraw only the activity still displaying that exact host/request.
    public static func refresh(_ event: AnswerReceiptEvent, pending: PendingStore) async {
        let feedback = LiveAnswerFeedback.resolve(pending.request(event.requestId), local: nil, now: Date())
        for activity in Activity<TogoActivityAttributes>.activities where activity.attributes.hostId == event.hostId {
            let content = activity.content
            var state = content.state
            guard state.status == "needs_you", state.requestId == event.requestId else { continue }
            state.answerFeedback = feedback
            await activity.update(ActivityContent(state: state, staleDate: content.staleDate, relevanceScore: content.relevanceScore))
        }
    }
}
#endif
