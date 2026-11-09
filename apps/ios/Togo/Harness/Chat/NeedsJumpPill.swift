import SwiftUI

/// "Needs you ↓" above the composer while a card that needs the user is off screen, like
/// Messages' new-messages capsule. Tapping it scrolls to the card. Its words are the chat's one
/// beacon element while it shows; it leaves once the card is in view.
struct NeedsJumpPill: View {
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                StatusWord(status: .needsYou, font: .footnote)
                Image(systemName: "arrow.down")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 14)
            .frame(minHeight: 36)
            // Opaque, like the mission pill: the needs-you words sit over scrolling chat text and
            // must read at once, not through glass.
            .background(Capsule().fill(Color.togoRaised))
            .overlay(Capsule().strokeBorder(Color.togoLine, lineWidth: 1))
            // The capsule stays 36 pt tall; the target around it is 44.
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .hoverEffect(.highlight)
        .accessibilityLabel(ChatCopy.jumpToNeeds)
        .accessibilityHint(ChatCopy.jumpToNeedsHint)
        .accessibilityIdentifier("chat.needsJump")
    }
}
