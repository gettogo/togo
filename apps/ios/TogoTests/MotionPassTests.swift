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
        for role in [MotionRole.fade, .dim, .listen, .emphasis, .ambient] {
            #expect(Motion.animation(role, reduce: true) == nil, "\(role)")
        }
        for role in [MotionRole.tabSettle, .dropIn, .collapse, .shrinkIntoTab, .rowGrow, .paneFlow, .sidebar, .chrome,
                     .respond, .resize, .reveal, .conceal, .progress] {
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
            // `ambient` names a loop's period, not a transition's length.
            let longest = role == .ambient ? 4.0 : 1.0
            #expect(Motion.nominalDuration(role) > 0 && Motion.nominalDuration(role) <= longest, "\(role)")
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

    // MARK: Motion principles v2 lint (§6)

    /// The v2 rules flag every case in the shared fixture, once per `// expect` line, and
    /// nothing in the allowed fixture, the same as `scripts/check-motion.sh`.
    @Test func v2RulesMatchTheirFixtures() throws {
        let flagged = try MotionLint.v2Fixture("v2-flagged.txt")
        let found = MotionLint.v2Offences(in: flagged.source, path: "Lint/v2-flagged.swift")
        #expect(found.map(\.rule).sorted() == flagged.expected.sorted(), "\(found)")
        let allowed = try MotionLint.v2Fixture("v2-allowed.txt")
        #expect(MotionLint.v2Offences(in: allowed.source, path: "Lint/v2-allowed.swift").isEmpty)
    }

    private func rules(_ source: String, path: String = "Chat/Row.swift") -> [String] {
        MotionLint.v2Offences(in: source, path: path).map(\.rule)
    }

    @Test func noRiseRule() {
        #expect(rules("x.transition(.move(edge: .top).combined(with: .offset(y: 4))) // motion: origin — x") == ["no-rise"])
        #expect(rules("Text(\"a\").revealOnAppear(index: 0)") == ["no-rise"])
        #expect(rules("struct Up: Transition {\n  func body() {\n    content.offset(y: 6)\n  }\n}") == ["no-rise"])
        // An offset outside a transition is layout, not an arrival.
        #expect(rules("Text(\"a\").offset(y: 6)").isEmpty)
        #expect(rules("struct Up: Transition {\n}\nText(\"a\").offset(y: 6)").isEmpty)
        // The prompt bar's edge and the tutorial's order explain their movement.
        #expect(rules("Text(\"a\").revealOnAppear(index: 0)", path: "Tutorial/TutorialPage.swift").isEmpty)
        #expect(rules("let t = AnyTransition.offset(y: 8)", path: "Roster/PromptBar.swift").isEmpty)
    }

    @Test func noRollInTimelineRule() {
        let rolling = "TimelineView(.periodic(from: origin, by: 1)) { _ in\n  Text(\"1s\").rollingNumber(1)\n}"
        #expect(rules(rolling) == ["no-roll-in-timeline"])
        #expect(rules("TimelineView(.periodic(from: origin, by: 1)) { _ in Text(\"\").countChange(1) }") == ["no-roll-in-timeline"])
        let clock = "TimelineView(.periodic(from: origin, by: 1)) { _ in\n  Text(\"1s\").clockText()\n}\nText(\"2\").countChange(2)"
        #expect(rules(clock).isEmpty)
    }

    @Test func periodicNowRule() {
        #expect(rules("TimelineView(.periodic(from: .now, by: 1)) { _ in }") == ["periodic-now"])
        #expect(rules("TimelineView(.periodic(from: Date(), by: 1)) { _ in }") == ["periodic-now"])
        #expect(rules("TimelineView(.periodic(from: clock.displayOrigin(waiting: w), by: 1)) { _ in }").isEmpty)
    }

    @Test func motionPurposeRule() {
        #expect(rules("withAnimation(Motion.animation(.resize, reduce: r)) { open = true }") == ["motion-purpose"])
        #expect(rules("x.matchedGeometryEffect(id: 1, in: ns)") == ["motion-purpose"])
        #expect(rules("// motion: cause → effect — the tap became the line\nx.matchedGeometryEffect(id: 1, in: ns)").isEmpty)
        // Within three lines above, not four.
        #expect(rules("// motion: state change — x\na\nb\nx.motion(.respond, value: v)").isEmpty)
        #expect(rules("// motion: state change — x\na\nb\nc\nx.motion(.respond, value: v)") == ["motion-purpose"])
    }

    @Test func availabilityAnimatesRule() {
        let keyed = "// motion: state change — x\nPane().motion(.fade, value: store.shownLayer)"
        #expect(rules(keyed) == ["availability-animates"])
        let mission = "// motion: state change — x\nBar().animation(Motion.animation(.fade, reduce: r), value: hasMission)"
        #expect(rules(mission) == ["availability-animates"])
        #expect(rules("// motion: state change — the user toggled\nPane().motion(.fade, value: harness.layer)").isEmpty)
        // Any layer state that comes from availability or load, not only the three names.
        #expect(rules("// motion: state change — x\nZStack {}.motion(.fade, value: layer == .pending)") == ["availability-animates"])
        #expect(rules("// motion: state change — x\nPane().motion(.fade, value: store.hasLoaded)") == ["availability-animates"])
    }
}
