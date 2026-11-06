import Foundation
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

/// Every change in the app moves the way its role says, and under Reduce Motion it fades or
/// lands at once.
@MainActor struct MotionPassTests {
    private static let tab = TabID(hostID: Samples.studio, sessionID: "s-04")

    private static func card(_ phase: RequestCard.Phase, bib: String? = "04") -> RequestCard {
        var card = Samples.request(id: "r-04", bib: bib, title: "Run the tests?")
        card.phase = phase
        return card
    }

    @Test func answeredCardTargetsItsTabChip() {
        let answered = Self.card(.sending(.approve))
        let intoChip = IncomingRequests.departure(of: answered, chipIDs: [Self.tab])
        #expect(intoChip == .intoChip(Self.tab))
        #expect(intoChip.role == .shrinkIntoTab)

        let noChip = IncomingRequests.departure(of: answered, chipIDs: [])
        #expect(noChip == .collapse)
        #expect(noChip.role == .collapse)

        let noSession = IncomingRequests.departure(of: Self.card(.sending(.deny), bib: nil), chipIDs: [Self.tab])
        #expect(noSession == .collapse)

        // A card that leaves because its own tab came on screen was never answered.
        let unanswered = IncomingRequests.departure(of: Self.card(.open), chipIDs: [Self.tab])
        #expect(unanswered == .fade)
        #expect(unanswered.role == .fade)
    }

    @Test func reconnectingDimsTerminal() {
        #expect(PaneView.terminalOpacity(for: .reconnecting(2)) == 0.55)
        for link: LinkPhase in [.live(transport: "mosh", note: nil), .connecting, .roaming] {
            #expect(PaneView.terminalOpacity(for: link) == 1, "\(link)")
        }
        #expect(PaneView.dimRole == .dim)
        // The dim lands at once under Reduce Motion.
        #expect(Motion.animation(PaneView.dimRole, reduce: true) == nil)
    }

    @Test func noAnimationOutsideRoles() throws {
        #expect(try MotionLint.allowListEntries().isEmpty, "the allow-list ends empty")
        let found = try MotionLint.violations()
        #expect(found.isEmpty, "\(found.joined(separator: "\n"))")
    }

    /// The rules catch every way around the roles in the shared fixtures, and nothing that uses
    /// them, so an empty result above means something.
    @Test func lintFlagsAnimationsOutsideRoles() throws {
        let flagged = try MotionLint.fixture("flagged.txt")
        #expect(flagged.count >= 28)
        for line in flagged {
            #expect(MotionLint.flags(line), "\(line)")
        }
        for line in try MotionLint.fixture("allowed.txt") {
            #expect(!MotionLint.flags(line), "\(line)")
        }
    }

    /// Views ask `Motion.animation(_:reduce:)` for their curve, and under Reduce Motion every
    /// role answers with a cross-fade of at most 200 ms or with no animation at all.
    @Test func reduceMotionFadesEveryTransition() {
        for role in MotionRole.allCases {
            let reduced = Motion.animation(role, reduce: true)
            switch Motion.spec(role).reduced {
            case let .fade(duration):
                #expect(duration > 0 && duration <= 0.2, "\(role)")
                #expect(reduced == .easeOut(duration: duration), "\(role)")
            case .instant:
                #expect(reduced == nil, "\(role)")
            }
            #expect(Motion.animation(role, reduce: false) != nil, "\(role)")
        }
        // Text and dimming land at once; movement cross-fades.
        for role in [MotionRole.fade, .dim, .listen] {
            #expect(Motion.animation(role, reduce: true) == nil, "\(role)")
        }
        for role in [MotionRole.tabSettle, .dropIn, .collapse, .shrinkIntoTab, .rowGrow, .paneFlow, .sidebar, .chrome] {
            #expect(Motion.animation(role, reduce: true) != nil, "\(role)")
        }
    }

    @Test func uiKitTimingFollowsTheRole() {
        for role in MotionRole.allCases {
            let timing = Motion.timingParameters(role)
            switch Motion.spec(role).curve {
            case .spring:
                #expect(timing.timingCurveType == .spring, "\(role)")
                #expect(timing.springTimingParameters != nil, "\(role)")
            case .easeOut:
                #expect(timing.cubicTimingParameters?.animationCurve == .easeOut, "\(role)")
            case .easeInOut:
                #expect(timing.cubicTimingParameters?.animationCurve == .easeInOut, "\(role)")
            }
            #expect(Motion.nominalDuration(role) > 0 && Motion.nominalDuration(role) <= 1, "\(role)")
        }
        #expect(Motion.nominalDuration(.fade) == 0.2)
        #expect(Motion.nominalDuration(.dim) == 0.6)
    }

    /// UIKit gets the role's curve with full motion, a short cross-fade under Reduce Motion, and
    /// no animator at all where the role lands at once.
    @Test func uiKitAnimatorHonoursReduceMotion() throws {
        let full = try #require(Motion.animator(.paneFlow, reduce: false) {})
        #expect(full.timingParameters?.timingCurveType == .spring)
        let reduced = try #require(Motion.animator(.paneFlow, reduce: true) {})
        #expect(reduced.duration == 0.2)
        #expect(reduced.timingParameters?.cubicTimingParameters?.animationCurve == .easeOut)
        #expect(Motion.animator(.dim, reduce: true) {} == nil)
        #expect(try #require(Motion.animator(.dim, reduce: false) {}).duration == 0.6)
    }

    /// A swipe, a chip or a sidebar pick ticks once for the pane that settled; a swap of the two
    /// panes is one settle, and closing the second pane is none.
    @Test func paneTabsTickOncePerSettle() {
        let one = TabID(hostID: Samples.studio, sessionID: "s-01")
        let four = Self.tab
        let single = PaneTabs(primary: one, secondary: nil)
        let split = PaneTabs(primary: one, secondary: four)
        #expect(PaneTabs.settles(from: single, to: PaneTabs(primary: four, secondary: nil)))
        #expect(PaneTabs.settles(from: single, to: split))
        #expect(PaneTabs.settles(from: split, to: PaneTabs(primary: four, secondary: one)))
        #expect(!PaneTabs.settles(from: split, to: single))
        #expect(!PaneTabs.settles(from: split, to: split))
    }

    /// The launch reads a spring for the beacon and an eased curve for the rest; a token that
    /// changed kind would make the beacon pop or the paws snap.
    @Test func launchCurvesKeepTheirKind() {
        #expect(LaunchTimeline.beaconGrowth.spring != nil)
        #expect(LaunchTimeline.eased.unitCurve != nil)
        #expect(Motion.spec(.fade).curve.unitCurve != nil)
        #expect(LaunchTimeline.handoffCurve.unitCurve != nil)
    }

    @Test func launchTimesComeFromRoles() {
        #expect(LaunchTimeline.pawStep == .milliseconds(Int(Motion.nominalDuration(.pawStep) * 2000)))
        #expect(LaunchTimeline.handoff == .milliseconds(Int(Motion.nominalDuration(.chrome) * 1000)))
        #expect(LaunchTimeline.reducedFade == .milliseconds(Int(Motion.nominalDuration(.fade) * 1000)))
        #expect(LaunchTimeline.reducedFade <= .milliseconds(200))
    }
}
