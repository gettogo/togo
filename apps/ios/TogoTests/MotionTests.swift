import SwiftUI
import Testing
@testable import Togo

struct MotionTests {
    @Test func everyRoleHasASpec() {
        for role in MotionRole.allCases {
            let spec = Motion.spec(role)
            switch spec.curve {
            case let .spring(response, damping):
                #expect(response > 0 && damping > 0, "\(role)")
            case let .easeOut(duration), let .easeInOut(duration):
                #expect(duration > 0, "\(role)")
            }
        }
        #expect(MotionRole.allCases.count == 13)
    }

    @Test func reducedIsShortFadeOrInstant() {
        for role in MotionRole.allCases {
            if case let .fade(duration) = Motion.spec(role).reduced {
                #expect(duration > 0 && duration <= 0.2, "\(role)")
            }
        }
        for role in [MotionRole.fade, .dim, .listen] {
            #expect(Motion.spec(role).reduced == .instant, "\(role)")
        }
    }

    @Test func springsSettleQuickly() {
        for role in MotionRole.allCases {
            if case let .spring(response, damping) = Motion.spec(role).curve {
                #expect(response <= 0.45, "\(role)")
                #expect(damping >= 0.75, "\(role)")
            }
        }
    }

    @Test func aliasesMatchRoles() {
        let aliases: [(Animation, MotionRole)] = [
            (Motion.tabSettle, .tabSettle), (Motion.dropIn, .dropIn), (Motion.collapse, .collapse),
            (Motion.confirm, .confirm), (Motion.fade, .fade), (Motion.pawStep, .pawStep),
        ]
        for (alias, role) in aliases {
            #expect(alias == Motion.animation(role, reduce: false), "\(role)")
        }
        #expect(Motion.tabSettle == .spring(response: 0.30, dampingFraction: 0.86))
        #expect(Motion.dropIn == .spring(response: 0.42, dampingFraction: 0.78))
        #expect(Motion.collapse == .easeOut(duration: 0.25))
        #expect(Motion.confirm == .easeOut(duration: 0.15))
        #expect(Motion.fade == .easeOut(duration: 0.2))
        #expect(Motion.pawStep == .easeOut(duration: 0.12))
        #expect(Motion.spec(.chrome).curve == .easeInOut(0.22))
        #expect(Motion.spec(.shrinkIntoTab).curve == .spring(response: 0.36, damping: 0.84))
    }

    @Test func reduceReturnsFallback() {
        #expect(Motion.animation(.dropIn, reduce: true) == .easeOut(duration: 0.2))
        #expect(Motion.animation(.paneFlow, reduce: true) == .easeOut(duration: 0.2))
        #expect(Motion.animation(.fade, reduce: true) == nil)
        #expect(Motion.animation(.dim, reduce: true) == nil)
        #expect(Motion.animation(.listen, reduce: true) == nil)
        #expect(Motion.stateText(reduce: true) == nil)
        #expect(Motion.stateText(reduce: false) == Motion.fade)
    }

    @Test func hapticsAreOnePerAction() {
        let cases = Haptic.allCases
        for (index, haptic) in cases.enumerated() {
            for other in cases[(index + 1)...] {
                #expect(haptic.feedback != other.feedback, "\(haptic) and \(other) feel the same")
            }
        }
        #expect(Haptic.listen.feedback == .impact(flexibility: .soft, intensity: 0.4))
        #expect(Haptic.done.feedback == .impact(flexibility: .soft, intensity: 0.6))
    }
}
