import SwiftUI

/// When each part of the launch happens. It starts from the whole mark, exactly as the system
/// launch screen drew it, so the hand-over from iOS is invisible; then the paws step one after
/// another, the beacon lights, and the mark hands over to the first screen.
enum LaunchTimeline {
    /// The mark as the launch screen left it, before anything moves.
    static let rest = Duration.milliseconds(150)
    static let pawCount = 4
    /// Time between one paw print stepping and the next.
    static let pawBeat = Duration.milliseconds(100)
    /// One print lifting and settling: the `pawStep` role, there and back.
    static let pawStep = length(of: .pawStep) * 2
    /// How much a print grows at the top of its step.
    static let pawLift = 0.3
    /// The beacon lights on the beat after the last print, as if the walk arrived there.
    static let beaconStart = rest + pawBeat * pawCount
    /// The beacon swelling and its glow spreading out and fading.
    static let beaconLight = Duration.milliseconds(360)
    static let beaconSwell = 0.22
    static let glowSpread = 1.6
    static let glowOpacity = 0.45
    /// The hand-off starts while the glow is still fading, so the mark never sits still.
    static let handoffStart = beaconStart + Duration.milliseconds(260)
    /// The ink fading off the first screen, and on a first run the mark moving into its place.
    static let handoff = length(of: .chrome)
    static let total = handoffStart + handoff

    /// Under Reduce Motion the mark holds, then cross-fades.
    static let reducedRest = Duration.milliseconds(100)
    static let reducedFade = length(of: .fade)
    static let reducedTotal = reducedRest + reducedFade

    /// The mark waits at the hand-off for the first screen to be ready, but never longer than this
    /// from its first frame.
    static let readyCap = Duration.milliseconds(1500)
    /// However the scene behaves, the overlay is gone this long after its first frame.
    static let ceiling = Duration.seconds(4)
    /// The longest step the clock takes between two frames. A busy main thread pauses the launch
    /// instead of skipping it, so the walk is never lost to dropped frames.
    static let longestStep = Duration.milliseconds(50)

    static func pawStart(_ index: Int) -> Duration {
        rest + pawBeat * index
    }

    static func handoffAt(reduceMotion: Bool) -> Duration {
        reduceMotion ? reducedRest : handoffStart
    }

    static func end(reduceMotion: Bool) -> Duration {
        reduceMotion ? reducedTotal : total
    }

    /// The paws step and the glow fades like `pawStep`; the glow spreads like `dropIn`; the
    /// hand-off moves like `chrome`.
    static let eased = Motion.spec(.pawStep).curve
    static let beaconGrowth = Motion.spec(.dropIn).curve
    static let handoffCurve = Motion.spec(.chrome).curve

    private static func length(of role: MotionRole) -> Duration {
        .milliseconds(Int((Motion.nominalDuration(role) * 1000).rounded()))
    }
}

/// The launch at one moment. A pure function of launch time, so snapshots can stop it on any frame.
struct LaunchFrame: Equatable {
    /// Each print's scale; 1 at rest.
    var paws: [Double]
    var beaconScale: Double
    /// The beacon's glow: its scale and opacity.
    var glowScale: Double
    var glowOpacity: Double
    /// 0 while the mark plays, 1 once the first screen has it.
    var handoff: Double
    var reduceMotion: Bool

    init(time: Duration, reduceMotion: Bool) {
        typealias T = LaunchTimeline
        self.reduceMotion = reduceMotion
        if reduceMotion {
            paws = Array(repeating: 1, count: T.pawCount)
            beaconScale = 1
            glowScale = 1
            glowOpacity = 0
            handoff = Self.progress(time, from: T.reducedRest, over: T.reducedFade, curve: Motion.spec(.fade).curve)
            return
        }
        paws = (0..<T.pawCount).map { index in
            1 + T.pawLift * Self.bump(Self.fraction(time, from: T.pawStart(index), over: T.pawStep))
        }
        let light = Self.fraction(time, from: T.beaconStart, over: T.beaconLight)
        beaconScale = 1 + T.beaconSwell * Self.bump(light)
        glowScale = light == 0 ? 1 : T.beaconGrowth.spring?.value(
            fromValue: 1, toValue: 1 + T.glowSpread, initialVelocity: 0,
            time: (time - T.beaconStart).inSeconds
        ) ?? 1
        glowOpacity = light == 0 || light == 1 ? 0 : T.glowOpacity * (1 - Self.ease(light))
        handoff = Self.progress(time, from: T.handoffStart, over: T.handoff, curve: T.handoffCurve)
    }

    /// The ink behind the mark, faded out over the hand-off.
    var inkOpacity: Double { 1 - handoff }

    /// The mark itself: it fades with the ink unless it is moving into a place of its own on the
    /// first screen. A target that only marks where the mark goes, like the roster's headline,
    /// takes it shrinking and fading. Under Reduce Motion it never moves, so it always fades.
    func markOpacity(movesToTarget: Bool, fades: Bool = false) -> Double {
        movesToTarget && !reduceMotion && !fades ? 1 : 1 - handoff
    }

    /// Up and back down over one segment: 0 at both ends, 1 in the middle.
    private static func bump(_ fraction: Double) -> Double {
        sin(.pi * ease(fraction))
    }

    private static func ease(_ fraction: Double) -> Double {
        LaunchTimeline.eased.unitCurve?.value(at: fraction) ?? fraction
    }

    private static func fraction(_ time: Duration, from start: Duration, over length: Duration) -> Double {
        ((time - start).inSeconds / length.inSeconds).clamped(to: 0...1)
    }

    private static func progress(
        _ time: Duration, from start: Duration, over length: Duration, curve: MotionSpec.Curve
    ) -> Double {
        let part = fraction(time, from: start, over: length)
        return curve.unitCurve?.value(at: part) ?? part
    }
}

/// Launch time as the overlay plays it. It starts at the first frame actually drawn, advances
/// only while the scene is active, takes at most `longestStep` per frame, and holds at the
/// hand-off until the first screen is ready.
struct LaunchClock: Equatable {
    private(set) var time: Duration = .zero
    /// Real time since the first frame.
    private(set) var wall: Duration = .zero
    private var last: Date?

    /// Advances to `date` and returns the launch time to draw.
    mutating func tick(at date: Date, active: Bool, ready: Bool, reduceMotion: Bool) -> Duration {
        typealias T = LaunchTimeline
        guard let last else {
            self.last = date
            return time
        }
        let real = Duration.seconds(max(0, date.timeIntervalSince(last)))
        self.last = date
        wall += real
        let end = T.end(reduceMotion: reduceMotion)
        if wall >= T.ceiling {
            time = end
            return time
        }
        guard active else { return time }
        let next = time + min(real, T.longestStep)
        let handoff = T.handoffAt(reduceMotion: reduceMotion)
        let mayHandOff = ready || wall >= T.readyCap
        time = min(mayHandOff ? next : min(next, handoff), end)
        return time
    }

    func finished(reduceMotion: Bool) -> Bool {
        time >= LaunchTimeline.end(reduceMotion: reduceMotion)
    }
}

/// Remembers whether this process has shown the launch, so a scene that is built again after a
/// resume or a reconnect goes straight to its content. A new process, a cold launch, always
/// plays it.
@MainActor final class LaunchGate {
    static let app = LaunchGate()
    private var shown = false

    func claim() -> Bool {
        guard !shown else { return false }
        shown = true
        return true
    }

    /// Marks the launch as already shown, for UI tests that start on a ready screen.
    func skip() {
        shown = true
    }
}

/// The launch over the first screen: it runs once, then calls `onFinish` so it can be removed.
/// It is decoration, so VoiceOver skips it and reaches the screen underneath at once.
struct LaunchOverlay: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scenePhase) private var scenePhase
    /// The first real screen is up, so the hand-off lands on content rather than on ink.
    let ready: Bool
    /// Where the first screen draws the mark itself, if it does; the mark moves there.
    var target: CGRect?
    /// The target only marks a place, so the mark fades as it gets there.
    var targetFades = false
    let onFinish: () -> Void
    @State private var clock = ClockBox()

    var body: some View {
        TimelineView(.animation) { context in
            let time = clock.value.tick(
                at: context.date, active: scenePhase == .active, ready: ready, reduceMotion: reduceMotion
            )
            let finished = clock.value.finished(reduceMotion: reduceMotion)
            LaunchOverlayFrame(frame: LaunchFrame(time: time, reduceMotion: reduceMotion), target: target, targetFades: targetFades)
                .onChange(of: finished) { _, done in
                    if done { onFinish() }
                }
        }
        .modifier(LaunchAccessibility())
    }

    /// The clock lives across frames without making each tick a state change of its own.
    private final class ClockBox {
        var value = LaunchClock()
    }
}

/// One frame of the launch: the mark centred on ink at the launch screen's size, so the first
/// frame is the system launch screen, pixel for pixel.
struct LaunchOverlayFrame: View {
    let frame: LaunchFrame
    var target: CGRect?
    var targetFades = false

    var body: some View {
        GeometryReader { proxy in
            let centre = CGPoint(x: proxy.size.width / 2, y: proxy.size.height / 2)
            let side = LaunchMark.launchScreenSide
            let start = CGRect(x: centre.x - side / 2, y: centre.y - side / 2, width: side, height: side)
            let moving = target != nil && !frame.reduceMotion
            let destination = moving ? target ?? start : start
            let t = frame.handoff
            ZStack {
                Color.togoInk.opacity(frame.inkOpacity)
                LaunchMark(
                    paws: frame.paws, beaconScale: frame.beaconScale,
                    glowScale: frame.glowScale, glowOpacity: frame.glowOpacity,
                    side: side
                )
                .scaleEffect(1 + (destination.width / side - 1) * t)
                .position(
                    x: centre.x + (destination.midX - centre.x) * t,
                    y: centre.y + (destination.midY - centre.y) * t
                )
                .opacity(frame.markOpacity(movesToTarget: moving, fades: targetFades))
            }
        }
        .ignoresSafeArea()
        // The screen underneath takes touches from the first frame, while the mark plays.
        .allowsHitTesting(false)
    }
}

/// VoiceOver skips the launch. UI tests on fake services see it as an element named "launch",
/// so they can check that a cold launch plays it and that it goes away.
private struct LaunchAccessibility: ViewModifier {
    func body(content: Content) -> some View {
        #if DEBUG
        if UITestLaunch.current.fakes {
            content.accessibilityElement().accessibilityIdentifier("launch")
        } else {
            content.accessibilityHidden(true)
        }
        #else
        content.accessibilityHidden(true)
        #endif
    }
}

/// Where a screen draws the mark the launch hands over to, in the window's coordinates.
struct LaunchTargetKey: PreferenceKey {
    static let defaultValue: Anchor<CGRect>? = nil

    static func reduce(value: inout Anchor<CGRect>?, nextValue: () -> Anchor<CGRect>?) {
        value = value ?? nextValue()
    }
}

/// Whether the launch target only marks a place, so the mark fades into it.
struct LaunchTargetFadesKey: PreferenceKey {
    static let defaultValue = false

    static func reduce(value: inout Bool, nextValue: () -> Bool) {
        value = value || nextValue()
    }
}

extension View {
    /// Marks this view as the place the launch mark moves into on its way out. With `fades` the
    /// mark shrinks towards it and fades, for a screen that does not draw the mark itself.
    func launchTarget(fades: Bool = false) -> some View {
        anchorPreference(key: LaunchTargetKey.self, value: .bounds) { $0 }
            .preference(key: LaunchTargetFadesKey.self, value: fades)
    }

    /// The launch over this view while `shown`; it removes itself by setting `shown` to false.
    func launchOverlay(shown: Binding<Bool>, ready: Bool) -> some View {
        modifier(LaunchOverlayHost(shown: shown, ready: ready))
    }
}

private struct LaunchOverlayHost: ViewModifier {
    @Binding var shown: Bool
    let ready: Bool
    @State private var targetFades = false

    func body(content: Content) -> some View {
        content
            .onPreferenceChange(LaunchTargetFadesKey.self) { fades in
                MainActor.assumeIsolated { targetFades = fades }
            }
            .overlayPreferenceValue(LaunchTargetKey.self) { anchor in
                if shown {
                    GeometryReader { proxy in
                        LaunchOverlay(ready: ready, target: anchor.map { proxy[$0] }, targetFades: targetFades) {
                            shown = false
                        }
                    }
                    .ignoresSafeArea()
                }
            }
    }
}

extension Duration {
    var inSeconds: Double {
        Double(components.seconds) + Double(components.attoseconds) / 1e18
    }
}

private extension Double {
    func clamped(to range: ClosedRange<Double>) -> Double {
        min(max(self, range.lowerBound), range.upperBound)
    }
}
