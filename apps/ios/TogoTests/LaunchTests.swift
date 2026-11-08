import Foundation
import SwiftUI
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

    /// The first frame is the system launch screen: the whole mark, at rest, on ink.
    @Test func firstFrameIsTheWholeMark() {
        for reduceMotion in [false, true] {
            let frame = LaunchFrame(time: .zero, reduceMotion: reduceMotion)
            #expect(frame.paws == [1, 1, 1, 1])
            #expect(frame.beaconScale == 1)
            #expect(frame.glowOpacity == 0)
            #expect(frame.handoff == 0)
            #expect(frame.inkOpacity == 1)
            #expect(frame.markOpacity(movesToTarget: false) == 1)
        }
        #expect(LaunchMark.launchScreenSide == 164, "the launch screen image is 164 pt square")
    }

    @Test func reduceMotionHoldsThenCrossFades() {
        for milliseconds in stride(from: 0, through: 300, by: 25) {
            let frame = LaunchFrame(time: .milliseconds(milliseconds), reduceMotion: true)
            #expect(frame.paws == [1, 1, 1, 1])
            #expect(frame.beaconScale == 1)
            #expect(frame.glowOpacity == 0)
            #expect(frame.markOpacity(movesToTarget: true) == frame.inkOpacity, "the mark never moves; it fades")
        }
        #expect(LaunchFrame(time: LaunchTimeline.reducedTotal, reduceMotion: true).inkOpacity == 0)
        #expect(LaunchTimeline.reducedFade <= .milliseconds(200))
    }

    @Test func shortEnough() {
        #expect(LaunchTimeline.total >= .milliseconds(900))
        #expect(LaunchTimeline.total <= .milliseconds(1_150))
        #expect(LaunchTimeline.total == LaunchTimeline.handoffStart + LaunchTimeline.handoff)
    }

    @Test func pawsStepLeftToRightOneBeatApart() {
        let starts = (0..<LaunchTimeline.pawCount).map(LaunchTimeline.pawStart)
        #expect(starts[0] == LaunchTimeline.rest, "nothing moves until the mark has been seen whole")
        for (earlier, later) in zip(starts, starts.dropFirst()) {
            #expect(later - earlier == LaunchTimeline.pawBeat)
        }
        let midWalk = LaunchFrame(time: starts[1] + .milliseconds(40), reduceMotion: false)
        #expect(midWalk.paws[0] > 1)
        #expect(midWalk.paws[1] > 1)
        #expect(midWalk.paws[2] == 1)
        #expect(midWalk.paws[3] == 1)
        let stepped = LaunchFrame(time: LaunchTimeline.beaconStart + LaunchTimeline.beaconLight, reduceMotion: false)
        #expect(stepped.paws.allSatisfy { abs($0 - 1) < 0.001 }, "every print is back at rest")
    }

    @Test func beaconLightsAfterTheLastPaw() {
        #expect(LaunchTimeline.beaconStart > LaunchTimeline.pawStart(LaunchTimeline.pawCount - 1))
        let before = LaunchFrame(time: LaunchTimeline.beaconStart - .milliseconds(10), reduceMotion: false)
        #expect(before.glowOpacity == 0)
        #expect(before.beaconScale == 1)
        let lighting = LaunchFrame(time: LaunchTimeline.beaconStart + .milliseconds(120), reduceMotion: false)
        #expect(lighting.glowOpacity > 0)
        #expect(lighting.glowScale > 1)
        #expect(lighting.beaconScale > 1)
        let end = LaunchFrame(time: LaunchTimeline.total, reduceMotion: false)
        #expect(end.glowOpacity == 0)
        #expect(abs(end.beaconScale - 1) < 0.001)
    }

    @Test func handsOverAtTheEnd() {
        let start = LaunchFrame(time: LaunchTimeline.handoffStart, reduceMotion: false)
        #expect(start.inkOpacity == 1)
        let end = LaunchFrame(time: LaunchTimeline.total, reduceMotion: false)
        #expect(end.inkOpacity == 0)
        #expect(end.markOpacity(movesToTarget: false) == 0, "with nowhere to go, the mark fades")
        #expect(end.markOpacity(movesToTarget: true) == 1, "on first run it lands on the screen's own mark")
    }

    // MARK: Clock

    private static let t0 = Date(timeIntervalSinceReferenceDate: 0)

    private func run(
        _ clock: inout LaunchClock, frames: Int, every step: Double, from start: Date = t0,
        active: Bool = true, ready: Bool = true, reduceMotion: Bool = false
    ) -> Date {
        var date = start
        for _ in 0..<frames {
            _ = clock.tick(at: date, active: active, ready: ready, reduceMotion: reduceMotion)
            date += step
        }
        return date
    }

    @Test func clockStartsAtTheFirstFrameDrawn() {
        var clock = LaunchClock()
        #expect(clock.tick(at: Self.t0 + 5, active: true, ready: true, reduceMotion: false) == .zero)
        #expect(clock.tick(at: Self.t0 + 5.016, active: true, ready: true, reduceMotion: false) > .zero)
    }

    @Test func playsToTheEndAtSixtyFrames() {
        var clock = LaunchClock()
        _ = run(&clock, frames: 120, every: 1.0 / 60)
        #expect(clock.finished(reduceMotion: false))
        var reduced = LaunchClock()
        _ = run(&reduced, frames: 30, every: 1.0 / 60, reduceMotion: true)
        #expect(reduced.finished(reduceMotion: true))
    }

    /// A main thread busy building the first screen pauses the launch rather than skipping it.
    @Test func aStallPausesTheWalkInsteadOfSkippingIt() {
        var clock = LaunchClock()
        _ = clock.tick(at: Self.t0, active: true, ready: true, reduceMotion: false)
        let after = clock.tick(at: Self.t0 + 0.8, active: true, ready: true, reduceMotion: false)
        #expect(after == LaunchTimeline.longestStep)
        #expect(LaunchFrame(time: after, reduceMotion: false).handoff == 0)
    }

    @Test func waitsWhileTheSceneIsNotActive() {
        var clock = LaunchClock()
        let next = run(&clock, frames: 30, every: 1.0 / 60, active: false)
        #expect(clock.time == .zero, "the mark holds on its first frame")
        _ = run(&clock, frames: 120, every: 1.0 / 60, from: next)
        #expect(clock.finished(reduceMotion: false))
    }

    @Test func holdsTheHandOffUntilTheFirstScreenIsReady() {
        var clock = LaunchClock()
        let next = run(&clock, frames: 70, every: 1.0 / 60, ready: false)
        #expect(clock.time == LaunchTimeline.handoffStart)
        _ = run(&clock, frames: 30, every: 1.0 / 60, from: next, ready: true)
        #expect(clock.finished(reduceMotion: false))
    }

    @Test func handsOffAnywayAfterTheCap() {
        var clock = LaunchClock()
        _ = run(&clock, frames: 60 * 2, every: 1.0 / 60, ready: false)
        #expect(clock.wall > LaunchTimeline.readyCap)
        #expect(clock.finished(reduceMotion: false))
    }

    @Test func goneByTheCeilingWhateverHappens() {
        var clock = LaunchClock()
        _ = run(&clock, frames: 5, every: 1, active: false, ready: false)
        #expect(clock.finished(reduceMotion: false))
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
                for target in [nil, CGRect(x: 20, y: 80, width: 120, height: 120)] {
                    let frame = LaunchFrame(time: .milliseconds(milliseconds), reduceMotion: reduceMotion)
                    let body = LaunchOverlayFrame(frame: frame, target: target).body
                    #expect(outermost(body).map(hitTestingOff) == true, "the overlay takes touches at \(milliseconds) ms")
                }
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
}

struct PrivacyCoverTests {
    /// A cold launch is inactive for a moment before it turns active; the cover would hide
    /// the launch then. Once the scene has been active, leaving it is covered.
    @Test func coverWaitsForTheFirstActivation() {
        #expect(!PrivacyCoverRule.covers(.inactive, wasActive: false))
        #expect(!PrivacyCoverRule.covers(.active, wasActive: false))
        #expect(PrivacyCoverRule.covers(.inactive, wasActive: true))
        #expect(PrivacyCoverRule.covers(.background, wasActive: false))
        #expect(PrivacyCoverRule.covers(.background, wasActive: true))
    }

    /// The shield (a window above every presentation) shows the lock while locked, the cover
    /// on the way out, and holds the cover over a return that is about to lock.
    @MainActor @Test func shieldFaces() {
        #expect(SceneShield.face(phase: .active, wasActive: true, locked: true, locksOnReturn: false) == .lock)
        #expect(SceneShield.face(phase: .background, wasActive: true, locked: false, locksOnReturn: false) == .cover)
        #expect(SceneShield.face(phase: .active, wasActive: true, locked: false, locksOnReturn: true) == .cover)
        #expect(SceneShield.face(phase: .active, wasActive: true, locked: false, locksOnReturn: false) == .hidden)
        #expect(SceneShield.face(phase: .inactive, wasActive: false, locked: false, locksOnReturn: false) == .hidden)
    }

    /// A sign-in code stays on this device and leaves the clipboard after two minutes.
    @MainActor @Test func signInCodesStayLocalAndExpire() throws {
        let now = Date(timeIntervalSince1970: 1000)
        let options = SensitivePasteboard.options(now: now)
        #expect(options[.localOnly] as? Bool == true)
        #expect(options[.expirationDate] as? Date == now.addingTimeInterval(120))
        let board = try #require(UIPasteboard(name: UIPasteboard.Name("togo-test-\(UUID())"), create: true))
        SensitivePasteboard.copy("123-456", to: board)
        #expect(board.string == "123-456")
        UIPasteboard.remove(withName: board.name)
    }
}
