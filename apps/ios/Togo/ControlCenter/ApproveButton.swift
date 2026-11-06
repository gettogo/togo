import SwiftUI

/// The one filled button in the app: it answers yes to something that needs the user.
struct ApproveButton: View {
    let title: String
    let action: () -> Void

    init(_ title: String, action: @escaping () -> Void) {
        self.title = title
        self.action = action
    }

    var body: some View {
        Button(title, action: action)
            .buttonStyle(ApproveStyle())
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
            .lineLimit(2)
            .multilineTextAlignment(.center)
            .padding(.horizontal, 18)
            .padding(.vertical, 11)
            .frame(minWidth: 44, minHeight: 44)
            .background(colorScheme == .dark ? Color.togoAccent : Color.togoAccentText, in: .capsule)
            .contentShape(.capsule)
            .opacity(isEnabled ? (configuration.isPressed ? 0.85 : 1) : 0.5)
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.96 : 1)
            .motion(.confirm, value: configuration.isPressed)
    }
}

/// A text-only answer beside the approve button, such as Deny or More.
struct PlainAnswerStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body)
            .foregroundStyle(Color.togoSnow)
            .lineLimit(2)
            .padding(.horizontal, 12)
            .padding(.vertical, 11)
            .frame(minWidth: 44, minHeight: 44)
            .contentShape(.rect)
            .opacity(isEnabled ? (configuration.isPressed ? 0.6 : 1) : 0.5)
    }
}
