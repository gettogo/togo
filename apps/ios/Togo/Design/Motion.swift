import SwiftUI

/// The only animation curves the app uses. Each one belongs to one kind of change, so the same
/// motion always means the same thing.
enum Motion {
    /// A tab settling under the finger after a swipe.
    static let tabSettle = Animation.spring(response: 0.30, dampingFraction: 0.86)
    /// A request arriving from the top.
    static let dropIn = Animation.spring(response: 0.42, dampingFraction: 0.78)
    /// An answered request shrinking away.
    static let collapse = Animation.easeOut(duration: 0.25)
    /// A button morphing into its confirmation.
    static let confirm = Animation.easeOut(duration: 0.15)
    /// The Reduce Motion stand-in for every movement, and plain appearance changes.
    static let fade = Animation.easeOut(duration: 0.2)
    /// One paw print lighting while a connection is made.
    static let pawStep = Animation.easeOut(duration: 0.12)

    /// A tapped row growing into its terminal. The system zoom already cross-fades under
    /// Reduce Motion.
    static func rowGrow(sourceID: some Hashable, in namespace: Namespace.ID) -> some NavigationTransition {
        .zoom(sourceID: sourceID, in: namespace)
    }

    static func motion(_ animation: Animation, reduce: Bool) -> Animation {
        reduce ? fade : animation
    }

    /// Status text changes in place; under Reduce Motion it swaps with no animation at all.
    static func stateText(reduce: Bool) -> Animation? {
        reduce ? nil : fade
    }
}

extension EnvironmentValues {
    /// Holds looping animations on their first frame, so a snapshot of one renders the same
    /// every run.
    @Entry var loopsPaused = false
}

extension View {
    /// `animation(_:value:)` that falls back to a cross-fade under Reduce Motion.
    func motion(_ animation: Animation, value: some Equatable) -> some View {
        modifier(MotionModifier(animation: animation, value: value))
    }
}

private struct MotionModifier<Value: Equatable>: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let animation: Animation
    let value: Value

    func body(content: Content) -> some View {
        content.animation(Motion.motion(animation, reduce: reduceMotion), value: value)
    }
}
