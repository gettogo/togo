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
