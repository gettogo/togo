import SwiftUI

/// How wide an action is: across the screen at the bottom of a step, or as wide as its label
/// inside content.
enum ActionWidth: Sendable {
    case full, fit
}

/// The one thing to do next: snow on ink, a filled capsule. Neutral in both appearances
/// because the accent is kept for things that need the user.
struct PrimaryButtonStyle: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.isEnabled) private var isEnabled
    var width: ActionWidth = .full

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .multilineTextAlignment(.center)
            .foregroundStyle(Color.togoInk)
            .padding(.horizontal, 20)
            .padding(.vertical, width == .full ? 14 : 12)
            .frame(minWidth: 44, maxWidth: width == .full ? .infinity : nil, minHeight: width == .full ? 50 : 44)
            .background(Color.togoSnow, in: .capsule)
            .contentShape(.capsule)
            .opacity(isEnabled ? (configuration.isPressed ? 0.85 : 1) : 0.4)
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.98 : 1)
            .animation(Motion.motion(Motion.confirm, reduce: reduceMotion), value: configuration.isPressed)
    }
}

/// Any other action: plain snow text with a full-size touch target.
struct SecondaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled
    @Environment(\.multilineTextAlignment) private var alignment
    var width: ActionWidth = .full

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body)
            .multilineTextAlignment(width == .full ? .center : alignment)
            .foregroundStyle(Color.togoSnow)
            .padding(.vertical, 10)
            .frame(minWidth: 44, maxWidth: width == .full ? .infinity : nil, minHeight: 44)
            .contentShape(.rect)
            .opacity(isEnabled ? (configuration.isPressed ? 0.6 : 1) : 0.4)
    }
}

extension ButtonStyle where Self == PrimaryButtonStyle {
    static var primary: Self { Self() }
    static func primary(_ width: ActionWidth) -> Self { Self(width: width) }
}

extension ButtonStyle where Self == SecondaryButtonStyle {
    static var secondary: Self { Self() }
    static func secondary(_ width: ActionWidth) -> Self { Self(width: width) }
}
