import SwiftUI

/// The one filled button in the app: it answers yes to something that needs the user.
struct ApproveButton: View {
    let title: String
    /// The answer is on its way: a spinner takes the words' place, the button keeps its size.
    var busy = false
    let action: () -> Void

    init(_ title: String, busy: Bool = false, action: @escaping () -> Void) {
        self.title = title
        self.busy = busy
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            Text(title)
                .opacity(busy ? 0 : 1)
                .overlay {
                    if busy {
                        ProgressView()
                            .controlSize(.small)
                            .tint(Color.togoInk)
                    }
                }
                // motion: cause → effect — the tapped button's words give way to a spinner; the button keeps its size.
                .motion(.respond, value: busy)
        }
        .buttonStyle(ApproveStyle())
        .accessibilityLabel(title)
    }
}

/// Light mode fills with the darker text accent and white, because ink on the light accent
/// falls short of 4.5:1. Dark mode keeps the light accent with ink text.
private struct ApproveStyle: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.isEnabled) private var isEnabled
    @Environment(\.colorScheme) private var colorScheme

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(colorScheme == .dark ? Color.togoInk : Color.white)
            // Agents word their approve option at length ("Yes, and don't ask again for this
            // command"); it wraps and never loses its end. The shape is a capsule on one line and
            // a rounded box once the words wrap, so no corner clips them.
            .multilineTextAlignment(.center)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 18)
            .padding(.vertical, 11)
            .frame(minWidth: 44, minHeight: 44)
            .background(colorScheme == .dark ? Color.togoAccent : Color.togoAccentText, in: .rect(cornerRadius: 22, style: .continuous))
            .contentShape(.rect(cornerRadius: 22, style: .continuous))
            .opacity(isEnabled ? (configuration.isPressed ? 0.85 : 1) : 0.5)
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.96 : 1)
            // motion: cause → effect — the button gives under the finger, so the press is felt where it lands.
            .motion(.confirm, value: configuration.isPressed)
            // motion: state change — the button dims in place when it can't be pressed.
            .motion(.respond, value: isEnabled)
    }
}

/// A text-only answer beside the approve button, such as Deny or More.
struct PlainAnswerStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body)
            .foregroundStyle(Color.togoSnow)
            .multilineTextAlignment(.leading)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 12)
            .padding(.vertical, 11)
            .frame(minWidth: 44, minHeight: 44)
            .contentShape(.rect)
            .opacity(isEnabled ? (configuration.isPressed ? 0.6 : 1) : 0.5)
    }
}
