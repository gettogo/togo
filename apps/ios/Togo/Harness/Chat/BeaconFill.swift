import SwiftUI

/// The ground of a card that needs the user: the raised surface with a faint beacon light
/// drifting slowly inside it, held by the card's own shape. No glow leaves the card.
///
/// The light is two soft radial pools of the beacon blue at low strength that wander on slow,
/// unrelated loops, so the card feels lit rather than animated. It holds still under Reduce
/// Motion and Low Power Mode, while the card is scrolled away, while the app is not in front,
/// and in paused snapshots; still, it is a faint even tint.
struct BeaconFill<S: InsettableShape>: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.loopsPaused) private var loopsPaused
    @Environment(\.scenePhase) private var scenePhase
    @State private var visible = true
    @State private var lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled

    let shape: S

    /// How strong the light is at its brightest; the snow text on it stays well above AA.
    static var peak: Double { 0.16 }
    /// The even tint shown when the light holds still.
    static var still: Double { 0.08 }

    var body: some View {
        shape
            .fill(Color.togoRaised)
            .overlay {
                if drifts {
                    // One Canvas whose only inputs are the two points, at most 30 frames a
                    // second, paused with the rest (spec §4, §5).
                    TimelineView(AnimationTimelineSchedule(minimumInterval: 1.0 / 30, paused: !drifts)) { context in
                        Canvas { canvas, size in
                            pools(at: context.date.timeIntervalSinceReferenceDate, in: &canvas, size: size)
                        }
                    }
                } else {
                    shape.fill(Color.togoAccent.opacity(Self.still))
                }
            }
            .clipShape(shape)
            .overlay { shape.strokeBorder(Color.togoAccent.opacity(0.22), lineWidth: 1) }
            .onScrollVisibilityChange(threshold: 0.01) { visible = $0 }
            .onDisappear { visible = false }
            .onAppear { visible = true }
            .onReceive(NotificationCenter.default.publisher(for: .NSProcessInfoPowerStateDidChange)) { _ in
                lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled
            }
            // The drift is this screen's one self-moving thing: other loops hold still.
            .preference(key: AmbientClaim.self, value: drifts)
            .accessibilityHidden(true)
    }

    private var drifts: Bool {
        visible && !reduceMotion && !loopsPaused && !lowPower && scenePhase == .active
    }

    /// Two pools on slow loops (about 11 s and 17 s), crossing now and then.
    private func pools(at time: TimeInterval, in canvas: inout GraphicsContext, size: CGSize) {
        let rect = CGRect(origin: .zero, size: size)
        let reach = max(size.width, size.height)
        let a = BeaconDrift.point(time: time, period: 11, phase: 0)
        let b = BeaconDrift.point(time: time, period: 17, phase: 2.1)
        canvas.fill(Path(rect), with: .color(Color.togoAccent.opacity(Self.still * 0.5)))
        canvas.fill(Path(rect), with: .radialGradient(
            Gradient(colors: [Color.togoAccent.opacity(Self.peak), Color.togoAccent.opacity(0)]),
            center: CGPoint(x: a.x * size.width, y: a.y * size.height), startRadius: 0, endRadius: reach * 0.65
        ))
        canvas.fill(Path(rect), with: .radialGradient(
            Gradient(colors: [Color.togoAccent.opacity(Self.peak * 0.7), Color.togoAccent.opacity(0)]),
            center: CGPoint(x: b.x * size.width, y: b.y * size.height), startRadius: 0, endRadius: reach * 0.5
        ))
    }
}

/// Where a pool of light is at a moment: a slow Lissajous loop inside the card, kept off the
/// very edges so the light always reads as inside it.
enum BeaconDrift {
    static func point(time: TimeInterval, period: Double, phase: Double) -> (x: Double, y: Double) {
        let t = time / period * 2 * .pi + phase
        return (0.5 + 0.42 * sin(t), 0.5 + 0.38 * sin(t * 1.618 + 0.7))
    }
}

/// A request that needs the user, drawn as a card on the beacon's faint moving light.
struct NeedsCard: ViewModifier {
    static let radius: CGFloat = 14

    func body(content: Content) -> some View {
        content
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background { BeaconFill(shape: RoundedRectangle(cornerRadius: Self.radius, style: .continuous)) }
    }
}

#if DEBUG
#Preview("Beacon fill") {
    VStack(alignment: .leading, spacing: 10) {
        Text("Needs you").font(.footnote.weight(.semibold)).foregroundStyle(Color.togoAccent)
        Text("Claude needs your permission").foregroundStyle(Color.togoSnow)
    }
    .modifier(NeedsCard())
    .padding()
    .background(Color.togoInk)
}
#endif
