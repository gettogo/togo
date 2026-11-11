import UIKit

/// The directional controller's face: one glass-ready disc with a dot that leans toward the
/// flicked direction and four ticks, one lit while an arrow repeats. Flick for arrows, tap for
/// the palette, hold for the palette in edit mode (`ControllerGesture`).
///
/// VoiceOver never needs the gesture: the face is one adjustable button whose activate opens
/// the palette, whose swipes send Up and Down, and whose actions send Left and Right.
@MainActor public final class ControllerFace: UIView {
    public static let side: CGFloat = 44

    /// The first arrow of a flick, and each change of direction.
    public var onArrow: (ControllerGesture.Direction) -> Void = { _ in }
    public var onFlickEnded: () -> Void = {}
    public var onTap: () -> Void = {}
    public var onHoldBegan: () -> Void = {}
    public var onHoldEnded: () -> Void = {}
    /// The repeat may run at its fast rate.
    public var isFast: Bool { gesture.fast }
    /// A finger is on the face.
    public private(set) var isTouching = false
    public var flicks = true {
        didSet { gesture.flicks = flicks }
    }

    private var gesture = ControllerGesture()
    private var holdTimer: Timer?
    private let dot = UIView()
    private var ticks: [ControllerGesture.Direction: UIView] = [:]
    private let now: () -> TimeInterval

    public override init(frame: CGRect) {
        now = { CACurrentMediaTime() }
        super.init(frame: frame)
        setUp()
    }

    /// `now` is injected so a test can drive the thresholds.
    init(now: @escaping () -> TimeInterval) {
        self.now = now
        super.init(frame: CGRect(x: 0, y: 0, width: Self.side, height: Self.side))
        setUp()
    }

    required init?(coder: NSCoder) {
        nil
    }

    private func setUp() {
        isMultipleTouchEnabled = false
        isAccessibilityElement = true
        accessibilityLabel = "Terminal keys"
        accessibilityHint = "Double tap for more keys. Swipe up or down to move the cursor."
        accessibilityTraits = [.button, .adjustable]
        accessibilityCustomActions = [
            UIAccessibilityCustomAction(name: "Left") { [weak self] _ in self?.send(.left); return true },
            UIAccessibilityCustomAction(name: "Right") { [weak self] _ in self?.send(.right); return true },
            UIAccessibilityCustomAction(name: "Edit keys") { [weak self] _ in self?.onHoldEnded(); return true },
        ]
        dot.backgroundColor = .label
        dot.layer.cornerRadius = 3.5
        addSubview(dot)
        for direction in [ControllerGesture.Direction.up, .down, .left, .right] {
            let tick = UIView()
            tick.backgroundColor = .secondaryLabel
            tick.alpha = 0.45
            tick.layer.cornerRadius = 1
            addSubview(tick)
            ticks[direction] = tick
        }
    }

    public override var intrinsicContentSize: CGSize {
        CGSize(width: Self.side, height: Self.side)
    }

    public override func layoutSubviews() {
        super.layoutSubviews()
        let c = CGPoint(x: bounds.midX, y: bounds.midY)
        dot.bounds = CGRect(x: 0, y: 0, width: 7, height: 7)
        dot.center = c
        let reach = min(bounds.width, bounds.height) / 2 - 5
        ticks[.up]?.frame = CGRect(x: c.x - 0.75, y: c.y - reach, width: 1.5, height: 4)
        ticks[.down]?.frame = CGRect(x: c.x - 0.75, y: c.y + reach - 4, width: 1.5, height: 4)
        ticks[.left]?.frame = CGRect(x: c.x - reach, y: c.y - 0.75, width: 4, height: 1.5)
        ticks[.right]?.frame = CGRect(x: c.x + reach - 4, y: c.y - 0.75, width: 4, height: 1.5)
    }

    // MARK: Touches

    public override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard let touch = touches.first, !isTouching else { return }
        isTouching = true
        handle(gesture.touchBegan(at: touch.location(in: self), time: now()))
        holdTimer?.invalidate()
        holdTimer = Timer.scheduledTimer(withTimeInterval: ControllerGesture.holdDelay, repeats: false) { [weak self] _ in
            MainActor.assumeIsolated { self?.holdTimerFired() }
        }
    }

    public override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard let touch = touches.first, isTouching else { return }
        let point = touch.location(in: self)
        handle(gesture.touchMoved(to: point, time: now()))
        lean(toward: point)
    }

    public override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard isTouching else { return }
        isTouching = false
        holdTimer?.invalidate()
        handle(gesture.touchEnded(time: now()))
        settle()
    }

    public override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard isTouching else { return }
        isTouching = false
        holdTimer?.invalidate()
        handle(gesture.touchCancelled())
        settle()
    }

    private func holdTimerFired() {
        guard isTouching else { return }
        handle(gesture.holdTimerFired(time: now()))
    }

    /// Feeds the gesture directly, for tests without touches.
    func feed(_ events: [ControllerGesture.Event]) {
        handle(events)
    }

    private func handle(_ events: [ControllerGesture.Event]) {
        for event in events {
            switch event {
            case .arrow(let direction):
                light(direction)
                onArrow(direction)
            case .holdBegan:
                lift(true)
                onHoldBegan()
            case .holdEnded:
                lift(false)
                onHoldEnded()
            case .tap:
                onTap()
            case .flickEnded:
                onFlickEnded()
            case .cancelled:
                lift(false)
                onFlickEnded()
            }
        }
    }

    // MARK: Looks

    private func lean(toward point: CGPoint) {
        guard case .flick = gesture.phase else { return }
        let dx = point.x - bounds.midX, dy = point.y - bounds.midY
        let distance = max(hypot(dx, dy), 0.001)
        let reach = 6 * tanh(distance / 24)
        dot.transform = CGAffineTransform(translationX: dx / distance * reach, y: dy / distance * reach)
    }

    private func light(_ direction: ControllerGesture.Direction?) {
        for (key, tick) in ticks {
            let on = key == direction
            tick.backgroundColor = on ? tintColor : .secondaryLabel
            tick.alpha = on ? 1 : 0.45
        }
        dot.backgroundColor = direction == nil ? .label : tintColor
    }

    private func lift(_ up: Bool) {
        let reduce = UIAccessibility.isReduceMotionEnabled
        UIView.animate(withDuration: reduce ? 0 : 0.2, delay: 0, options: [.curveEaseOut, .beginFromCurrentState]) {
            self.transform = up ? CGAffineTransform(scaleX: 1.14, y: 1.14) : .identity
        }
    }

    private func settle() {
        light(nil)
        let reduce = UIAccessibility.isReduceMotionEnabled
        UIView.animate(withDuration: reduce ? 0 : 0.2, delay: 0, options: [.curveEaseOut, .beginFromCurrentState]) {
            self.dot.transform = .identity
            self.transform = .identity
        }
    }

    // MARK: Accessibility

    private func send(_ direction: ControllerGesture.Direction) {
        onArrow(direction)
        onFlickEnded()
    }

    public override func accessibilityActivate() -> Bool {
        onTap()
        return true
    }

    public override func accessibilityIncrement() {
        send(.up)
    }

    public override func accessibilityDecrement() {
        send(.down)
    }
}
