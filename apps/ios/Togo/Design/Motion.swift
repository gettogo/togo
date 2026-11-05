import SwiftUI

/// The only animation curves the app uses. Each one belongs to one kind of change, so the same
/// motion always means the same thing.
enum Motion {
    /// A tab settling under the finger after a swipe.
    static let tabSettle = animation(.tabSettle)
    /// A request arriving from the top.
    static let dropIn = animation(.dropIn)
    /// An answered request shrinking away.
    static let collapse = animation(.collapse)
    /// A button morphing into its confirmation.
    static let confirm = animation(.confirm)
    /// The Reduce Motion stand-in for every movement, and plain appearance changes.
    static let fade = animation(.fade)
    /// One paw print lighting while a connection is made.
    static let pawStep = animation(.pawStep)

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
        animation(.fade, reduce: reduce)
    }

    /// The role's curve, or under Reduce Motion its short cross-fade, or nil when the change
    /// should land with no animation at all.
    static func animation(_ role: MotionRole, reduce: Bool) -> Animation? {
        let spec = spec(role)
        guard reduce else { return spec.curve.animation }
        switch spec.reduced {
        case let .fade(duration): return .easeOut(duration: duration)
        case .instant: return nil
        }
    }

    private static func animation(_ role: MotionRole) -> Animation {
        spec(role).curve.animation
    }
}

/// How one role moves, with full motion and under Reduce Motion. Values come from
/// brand/tokens.json through MotionTokens.swift.
struct MotionSpec: Equatable, Sendable {
    enum Curve: Equatable, Sendable {
        case spring(response: Double, damping: Double)
        case easeOut(Double)
        case easeInOut(Double)

        var animation: Animation {
            switch self {
            case let .spring(response, damping): .spring(response: response, dampingFraction: damping)
            case let .easeOut(duration): .easeOut(duration: duration)
            case let .easeInOut(duration): .easeInOut(duration: duration)
            }
        }
    }

    enum Reduced: Equatable, Sendable {
        /// A cross-fade of this many seconds in place of the movement.
        case fade(Double)
        /// No animation: the change lands at once.
        case instant
    }

    var curve: Curve
    var reduced: Reduced
}

extension EnvironmentValues {
    /// Holds looping animations on their first frame, so a snapshot of one renders the same
    /// every run.
    @Entry var loopsPaused = false
}

extension View {
    /// Animates changes to `value` the way `role` moves, honouring Reduce Motion.
    func motion(_ role: MotionRole, value: some Equatable) -> some View {
        modifier(MotionRoleModifier(role: role, value: value))
    }

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

private struct MotionRoleModifier<Value: Equatable>: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let role: MotionRole
    let value: Value

    func body(content: Content) -> some View {
        content.animation(Motion.animation(role, reduce: reduceMotion), value: value)
    }
}
