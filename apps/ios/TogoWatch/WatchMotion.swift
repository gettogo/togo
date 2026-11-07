import SwiftUI

// The Watch shares the app's roles (MotionTokens.swift) and choreography
// (MotionChoreography.swift), but not Motion.swift: its UIKit animators do not exist on watchOS.
// This is the part of Motion.swift the Watch needs, the role's curve and its Reduce Motion
// fade. Keep it in step with Togo/Design/Motion.swift.

/// The only animation curves the Watch uses, by role, as on the phone.
enum Motion {
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
}

/// How one role moves, with full motion and under Reduce Motion.
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
        case fade(Double)
        case instant
    }

    var curve: Curve
    var reduced: Reduced
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
