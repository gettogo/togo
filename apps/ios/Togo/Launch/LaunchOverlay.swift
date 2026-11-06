import SwiftUI

/// When each part of the launch happens. The control center is laid out and connecting
/// underneath from the first frame, so this only decides how long the mark is in the way.
enum LaunchTimeline {
    /// The prompt chevron stroking in.
    static let draw = Duration.milliseconds(300)
    static let pawCount = 4
    /// Time between one paw print and the next.
    static let pawBeat = Duration.milliseconds(80)
    /// Each print fading in; the same length as `Motion.pawStep`, so the walk reads like the
    /// one shown while connecting.
    static let pawFade = Duration.milliseconds(120)
    /// The walk sets off on the chevron's last beat, so the prompt hands over to the trail
    /// without a pause.
    static let walkStart = draw - pawBeat
    /// The beacon lights on the beat after the last print, as if the walk arrived there.
    static let beaconStart = walkStart + pawBeat * pawCount
    /// The beacon switching on. Its growth is a spring and keeps settling through the hold.
    static let beaconLight = Duration.milliseconds(60)
    static let hold = Duration.milliseconds(100)
    static let fadeStart = beaconStart + beaconLight + hold
    static let fade = Duration.milliseconds(200)
    static let total = fadeStart + fade

    /// Under Reduce Motion the whole mark shows at once, holds, and cross-fades.
    static let reducedFade = Duration.milliseconds(200)
    static let reducedTotal = hold + reducedFade

    static func pawStart(_ index: Int) -> Duration {
        walkStart + pawBeat * index
    }

    /// Every eased segment eases out, like `Motion.fade` and `Motion.pawStep`.
    static let curve = UnitCurve.easeOut
    static let beaconSpring = Spring(response: 0.35, dampingRatio: 0.8)
    static let beaconFromScale = 0.6
}

/// The launch at one moment. A pure function of time, so snapshots can stop it on any frame.
struct LaunchFrame: Equatable {
    var chevron: Double
    var paws: [Double]
    var beaconScale: Double
    var beaconOpacity: Double
    var overlayOpacity: Double

    init(time: Duration, reduceMotion: Bool) {
        typealias T = LaunchTimeline
        if reduceMotion {
            chevron = 1
            paws = Array(repeating: 1, count: T.pawCount)
            beaconScale = 1
            beaconOpacity = 1
            overlayOpacity = 1 - Self.progress(time, from: T.hold, over: T.reducedFade)
            return
        }
        chevron = Self.progress(time, from: .zero, over: T.draw)
        paws = (0..<T.pawCount).map { Self.progress(time, from: T.pawStart($0), over: T.pawFade) }
        beaconOpacity = Self.progress(time, from: T.beaconStart, over: T.beaconLight)
        beaconScale = time < T.beaconStart
            ? T.beaconFromScale
            : T.beaconSpring.value(
                fromValue: T.beaconFromScale, toValue: 1, initialVelocity: 0,
                time: (time - T.beaconStart).inSeconds
            )
        overlayOpacity = 1 - Self.progress(time, from: T.fadeStart, over: T.fade)
    }

    /// Eased progress of a segment that starts at `start` and lasts `length`, clamped to 0...1.
    private static func progress(_ time: Duration, from start: Duration, over length: Duration) -> Double {
        let linear = ((time - start).inSeconds / length.inSeconds).clamped(to: 0...1)
        return LaunchTimeline.curve.value(at: linear)
    }
}

/// Remembers whether this process has shown the launch, so a scene that is built again after a
/// resume or a reconnect goes straight to its content.
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

/// The launch over the control center: it runs once, then calls `onFinish` so it can be removed.
/// It is decoration, so VoiceOver skips it and reaches the control center at once.
struct LaunchOverlay: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var start: Date?
    let onFinish: () -> Void

    var body: some View {
        TimelineView(.animation) { context in
            let elapsed = start.map { Duration.seconds(context.date.timeIntervalSince($0)) } ?? .zero
            LaunchOverlayFrame(frame: LaunchFrame(time: elapsed, reduceMotion: reduceMotion))
        }
        .onAppear { start = .now }
        .task {
            guard (try? await Task.sleep(for: reduceMotion ? LaunchTimeline.reducedTotal : LaunchTimeline.total)) != nil
            else { return }
            onFinish()
        }
        .accessibilityHidden(true)
    }
}

/// One frame of the launch: the mark centred on ink, matching the system launch screen behind it.
struct LaunchOverlayFrame: View {
    let frame: LaunchFrame

    var body: some View {
        LaunchMark(
            chevron: frame.chevron, paws: frame.paws,
            beaconScale: frame.beaconScale, beaconOpacity: frame.beaconOpacity
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.togoInk)
        .ignoresSafeArea()
        .opacity(frame.overlayOpacity)
        // The screen underneath takes touches from the first frame, while the mark plays.
        .allowsHitTesting(false)
    }
}

private extension Duration {
    var inSeconds: Double {
        Double(components.seconds) + Double(components.attoseconds) / 1e18
    }
}

private extension Double {
    func clamped(to range: ClosedRange<Double>) -> Double {
        min(max(self, range.lowerBound), range.upperBound)
    }
}
