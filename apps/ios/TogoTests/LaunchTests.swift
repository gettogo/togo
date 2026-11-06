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

    /// A touch on the overlay, at any moment of it, lands on the screen underneath: the
    /// outermost modifier of every frame turns hit testing off. (A hosting view's own hit test
    /// answers itself whatever its content, so the view's modifiers are read instead.)
    @Test func overlayNeverBlocksTouches() {
        func hitTestingOff(_ value: Any) -> Bool {
            let mirror = Mirror(reflecting: value)
            if String(describing: mirror.subjectType).hasPrefix("_AllowsHitTestingModifier") {
                return mirror.children.contains { $0.label == "allowsHitTesting" && ($0.value as? Bool) == false }
            }
            return mirror.children.contains { hitTestingOff($0.value) }
        }
        func outermost(_ body: Any) -> Any? {
            Mirror(reflecting: body).children.first { $0.label == "modifier" }?.value
        }
        for milliseconds in [0, 300, 600, 850, 1_000] {
            for reduceMotion in [false, true] {
                let body = LaunchOverlayFrame(frame: LaunchFrame(time: .milliseconds(milliseconds), reduceMotion: reduceMotion)).body
                #expect(outermost(body).map(hitTestingOff) == true, "the overlay takes touches at \(milliseconds) ms")
            }
        }
    }

    @Test func interactiveSignpostFiresOnce() {
        var signposts = 0
        var launches = 0
        var deferred: [String] = []
        var turns: [@MainActor () -> Void] = []
        let perf = PerfSignposts(
            emit: { signposts += 1 }, recordLaunch: { launches += 1 }, nextTurn: { turns.append($0) }
        )
        perf.afterInteractive { deferred.append("tips") }
        perf.afterInteractive { deferred.append("push") }
        #expect(!perf.isInteractive)
        #expect(deferred.isEmpty, "work waiting for the first screen runs only once it is up")

        perf.interactive()
        perf.interactive()
        perf.interactive()
        #expect(perf.isInteractive)
        #expect(signposts == 1)
        #expect(launches == 1, "the launch time is recorded once, when the first screen is up")
        #expect(deferred.isEmpty, "nothing deferred runs in the turn that draws the first screen")

        // One item per run-loop turn, so the frames between them still draw.
        perf.afterInteractive { deferred.append("late") }
        var ran = 0
        while !turns.isEmpty {
            turns.removeFirst()()
            ran += 1
            #expect(deferred.count == ran, "each turn runs exactly one item")
        }
        #expect(deferred == ["tips", "push", "late"])

        // Once the queue is drained, work asked for runs at once, and nothing runs twice.
        perf.afterInteractive { deferred.append("after") }
        perf.interactive()
        #expect(deferred == ["tips", "push", "late", "after"])
        #expect(turns.isEmpty)
        #expect(signposts == 1)
    }

    @Test func overlayIsOpaqueUntilTheFadeAndGoneAtTheEnd() {
        #expect(LaunchFrame(time: LaunchTimeline.fadeStart, reduceMotion: false).overlayOpacity == 1)
        #expect(LaunchFrame(time: LaunchTimeline.total, reduceMotion: false).overlayOpacity == 0)
        let chevronDone = LaunchFrame(time: LaunchTimeline.draw, reduceMotion: false)
        #expect(chevronDone.chevron == 1)
        #expect(LaunchFrame(time: .zero, reduceMotion: false).chevron == 0)
    }
}
