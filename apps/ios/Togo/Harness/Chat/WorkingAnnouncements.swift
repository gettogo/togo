import SwiftUI
import TogoKit

/// Says once that work began and once that it ended, never on each tick or step. The live
/// indicator itself is the in-progress turn's work line (see `WorkingLogic.placement`).
struct WorkingAnnouncements: ViewModifier {
    let active: Bool

    func body(content: Content) -> some View {
        content
            .onChange(of: active) { _, now in
                guard UIAccessibility.isVoiceOverRunning else { return }
                AccessibilityNotification.Announcement(now ? WorkingCopy.started : WorkingCopy.finished).post()
            }
            .haptic(.done, trigger: active, when: { !$0 })
    }
}

/// Speaks a finished reply once and a new needs row once, at high priority; never each
/// streamed piece. What was on screen when the chat opened is not spoken.
@MainActor final class ChatAnnouncer {
    private var seen: Set<String>?
    private var finished: Set<String> = []

    func update(_ rows: [ChatRow]) {
        defer { remember(rows) }
        guard let seen, UIAccessibility.isVoiceOverRunning else { return }
        for row in rows {
            switch row {
            case .needs(let item) where !seen.contains(item.id):
                NeedsAnnouncement.post(ChatCopy.spokenNeeds(item.chatText ?? ""))
            case .reply(let item):
                if case .assistant(let text) = item.body, !text.partial, !finished.contains(item.id) {
                    announce(ChatCopy.spokenReply(text.text), high: false)
                }
            default:
                break
            }
        }
    }

    private func remember(_ rows: [ChatRow]) {
        seen = Set(rows.map(\.id))
        for row in rows {
            if case .reply(let item) = row, case .assistant(let text) = item.body, !text.partial {
                finished.insert(item.id)
            }
        }
    }

    private func announce(_ text: String, high: Bool) {
        var line = AttributedString(text)
        line.accessibilitySpeechAnnouncementPriority = high ? .high : .default
        AccessibilityNotification.Announcement(line).post()
    }
}
