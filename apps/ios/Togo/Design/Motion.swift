import SwiftUI
import UIKit

/// The only animation curves the app uses. Each one belongs to one kind of change, so the same
/// motion always means the same thing.
enum Motion {
    /// A tapped row growing into its terminal. The system zoom already cross-fades under
    /// Reduce Motion.
    static func rowGrow(sourceID: some Hashable, in namespace: Namespace.ID) -> some NavigationTransition {
        .zoom(sourceID: sourceID, in: namespace)
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

    /// The role's nominal length in seconds: an eased curve's duration, or a spring's response.
    /// A spring keeps settling a little past its response.
    static func nominalDuration(_ role: MotionRole) -> Double {
        spec(role).curve.duration
    }

    /// A UIKit animator that moves the way the role does, or under Reduce Motion its short
    /// cross-fade. Nil when the change should land with no animation: run `animations` directly.
    static func animator(
        _ role: MotionRole, reduce: Bool, animations: @escaping @MainActor () -> Void
    ) -> UIViewPropertyAnimator? {
        let spec = spec(role)
        guard reduce else {
            return UIViewPropertyAnimator(
                duration: spec.curve.duration, timingParameters: timingParameters(role)
            ).adding(animations)
        }
        switch spec.reduced {
        case let .fade(duration):
            return UIViewPropertyAnimator(
                duration: duration, timingParameters: UICubicTimingParameters(animationCurve: .easeOut)
            ).adding(animations)
        case .instant:
            return nil
        }
    }

    /// The role's curve for a `UIViewPropertyAnimator`, so UIKit moves the way SwiftUI does.
    /// Prefer `animator(_:reduce:animations:)`, which also honours Reduce Motion.
    static func timingParameters(_ role: MotionRole) -> UITimingCurveProvider {
        switch spec(role).curve {
        case let .spring(response, damping):
            // A unit mass with the stiffness and damping SwiftUI derives from response and
            // damping fraction.
            let stiffness = pow(2 * .pi / response, 2)
            return UISpringTimingParameters(
                mass: 1, stiffness: stiffness, damping: 4 * .pi * damping / response, initialVelocity: .zero
            )
        case .easeOut: return UICubicTimingParameters(animationCurve: .easeOut)
        case .easeInOut: return UICubicTimingParameters(animationCurve: .easeInOut)
        }
    }
}

private extension UIViewPropertyAnimator {
    func adding(_ animations: @escaping @MainActor () -> Void) -> Self {
        addAnimations(animations)
        return self
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

        /// An eased curve's duration, or a spring's response.
        var duration: Double {
            switch self {
            case let .spring(response, _): response
            case let .easeOut(duration), let .easeInOut(duration): duration
            }
        }

        /// The eased curve as a unit curve, for frames computed from time. Nil for a spring.
        var unitCurve: UnitCurve? {
            switch self {
            case .spring: nil
            case .easeOut: .easeOut
            case .easeInOut: .easeInOut
            }
        }

        /// The spring, for frames computed from time. Nil for an eased curve.
        var spring: Spring? {
            guard case let .spring(response, damping) = self else { return nil }
            return Spring(response: response, dampingRatio: damping)
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
}

private struct MotionRoleModifier<Value: Equatable>: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let role: MotionRole
    let value: Value

    func body(content: Content) -> some View {
        content.animation(Motion.animation(role, reduce: reduceMotion), value: value)
    }
}
