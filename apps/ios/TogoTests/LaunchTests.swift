import Testing
@testable import Togo

@MainActor
struct LaunchTests {
    @Test func launchOnlyOnColdStart() {
        let gate = LaunchGate()
        #expect(gate.claim(), "the first scene of a process plays the launch")
        #expect(!gate.claim(), "a scene built again after resume or reconnect does not")
        #expect(!gate.claim())
    }

    @Test func reduceMotionUsesStaticMark() {
        for milliseconds in stride(from: 0, through: 300, by: 50) {
            let frame = LaunchFrame(time: .milliseconds(milliseconds), reduceMotion: true)
            #expect(frame.chevron == 1)
            #expect(frame.paws == [1, 1, 1, 1])
            #expect(frame.beaconScale == 1)
            #expect(frame.beaconOpacity == 1)
        }
        #expect(LaunchFrame(time: .zero, reduceMotion: true).overlayOpacity == 1)
        #expect(LaunchFrame(time: LaunchTimeline.reducedTotal, reduceMotion: true).overlayOpacity == 0)
        #expect(LaunchTimeline.reducedFade <= .milliseconds(200))
    }

    @Test func totalDurationUnder900ms() {
        let sum = LaunchTimeline.walkStart
            + LaunchTimeline.pawBeat * LaunchTimeline.pawCount
            + LaunchTimeline.beaconLight + LaunchTimeline.hold + LaunchTimeline.fade
        #expect(LaunchTimeline.total == sum)
        #expect(LaunchTimeline.total <= .milliseconds(900))
        #expect(LaunchTimeline.draw == .milliseconds(300))
        #expect(LaunchTimeline.pawBeat == .milliseconds(80))
        #expect(LaunchTimeline.hold == .milliseconds(100))
        #expect(LaunchTimeline.fade == .milliseconds(200))
    }

    @Test func pawsWalkLeftToRightOneBeatApart() {
        let starts = (0..<LaunchTimeline.pawCount).map(LaunchTimeline.pawStart)
        for (earlier, later) in zip(starts, starts.dropFirst()) {
            #expect(later - earlier == LaunchTimeline.pawBeat)
        }
        let midWalk = LaunchFrame(time: starts[1] + .milliseconds(40), reduceMotion: false)
        #expect(midWalk.paws[0] > midWalk.paws[1])
        #expect(midWalk.paws[1] > 0)
        #expect(midWalk.paws[2] == 0)
    }

    @Test func beaconLightsAfterTheLastPawAndSettles() {
        let lastPaw = LaunchTimeline.pawStart(LaunchTimeline.pawCount - 1)
        #expect(LaunchTimeline.beaconStart > lastPaw)
        let before = LaunchFrame(time: LaunchTimeline.beaconStart - .milliseconds(10), reduceMotion: false)
        #expect(before.beaconOpacity == 0)
        let lit = LaunchFrame(time: LaunchTimeline.beaconStart, reduceMotion: false)
        #expect(abs(lit.beaconScale - 0.6) < 0.001)
        #expect(lit.beaconOpacity == 0)
        let settled = LaunchFrame(time: LaunchTimeline.total, reduceMotion: false)
        #expect(abs(settled.beaconScale - 1) < 0.01)
        #expect(settled.beaconOpacity == 1)
    }

    @Test func touchesPassThroughAfterHalfASecond() {
        #expect(LaunchFrame(time: .milliseconds(499), reduceMotion: false).blocksTouches)
        #expect(!LaunchFrame(time: .milliseconds(500), reduceMotion: false).blocksTouches)
    }

    @Test func overlayIsOpaqueUntilTheFadeAndGoneAtTheEnd() {
        #expect(LaunchFrame(time: LaunchTimeline.fadeStart, reduceMotion: false).overlayOpacity == 1)
        #expect(LaunchFrame(time: LaunchTimeline.total, reduceMotion: false).overlayOpacity == 0)
        let chevronDone = LaunchFrame(time: LaunchTimeline.draw, reduceMotion: false)
        #expect(chevronDone.chevron == 1)
        #expect(LaunchFrame(time: .zero, reduceMotion: false).chevron == 0)
    }
}
