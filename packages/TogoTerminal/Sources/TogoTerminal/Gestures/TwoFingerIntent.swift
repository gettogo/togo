import UIKit
import UIKit.UIGestureRecognizerSubclass

/// What two fingers on the terminal mean, judged from how far their midpoint travelled and
/// how much the gap between them changed. SwiftTerm's scroll view pan tracks the same two
/// fingers, so the session swipe and the pinch only begin for their own motion and leave a
/// vertical two-finger drag to scrolling.
enum TwoFingerIntent: Equatable, Sendable {
    case scroll, swipe, pinch

    /// A swipe must be clearly sideways: twice as much horizontal travel as vertical.
    static let horizontalDominance: CGFloat = 2
    /// A pinch changes the finger gap at least as much as the fingers travel: one finger
    /// held still moves the midpoint half as far as the gap changes, a two-sided pinch barely
    /// moves it. Drift in the gap during a swipe stays well under the travel.
    static let spreadPerTravel: CGFloat = 1
    /// And the gap must change by a tenth of where it started, so a few points of drift
    /// early in a gesture never read as a pinch.
    static let spreadPerGap: CGFloat = 0.1

    static func classify(translation: CGPoint, spreadChange: CGFloat, startGap: CGFloat) -> TwoFingerIntent {
        let travel = max(abs(translation.x), abs(translation.y))
        let spread = abs(spreadChange)
        if spread > 0, spread >= travel * spreadPerTravel, spread >= max(startGap, 0) * spreadPerGap {
            return .pinch
        }
        if abs(translation.x) > 0, abs(translation.x) >= abs(translation.y) * horizontalDominance { return .swipe }
        return .scroll
    }

    /// Travel or speed that switches session when a sideways drag ends. Nil keeps the session.
    static let swipeDistance: CGFloat = 60
    static let swipeVelocity: CGFloat = 500

    static func swipeDirection(translation: CGPoint, velocity: CGPoint) -> GestureController.Direction? {
        guard classify(translation: translation, spreadChange: 0, startGap: 0) == .swipe else { return nil }
        guard abs(translation.x) >= swipeDistance || abs(velocity.x) >= swipeVelocity else { return nil }
        return translation.x < 0 ? .left : .right
    }
}

/// Where each finger went down, keyed by touch. UIKit reuses `UITouch` objects (and so their
/// identities) across sequences, so a touch that begins again overwrites its old start, and
/// ended touches are dropped.
struct TouchStarts: Sendable {
    private var starts: [ObjectIdentifier: CGPoint] = [:]

    mutating func began(_ id: ObjectIdentifier, at point: CGPoint) { starts[id] = point }
    mutating func ended(_ id: ObjectIdentifier) { starts[id] = nil }
    mutating func reset() { starts.removeAll() }

    /// The two starts when exactly two fingers are down.
    var pair: (CGPoint, CGPoint)? {
        guard starts.count == 2 else { return nil }
        let points = Array(starts.values)
        return (points[0], points[1])
    }
}

/// Midpoint travel, gap change and starting gap of two fingers, from their starts.
@MainActor func twoFingerMotion(_ recognizer: UIGestureRecognizer, starts: TouchStarts) -> TwoFingerIntent {
    guard let view = recognizer.view, recognizer.numberOfTouches == 2, let start = starts.pair else { return .scroll }
    let now = (recognizer.location(ofTouch: 0, in: view), recognizer.location(ofTouch: 1, in: view))
    func midpoint(_ a: CGPoint, _ b: CGPoint) -> CGPoint { CGPoint(x: (a.x + b.x) / 2, y: (a.y + b.y) / 2) }
    func gap(_ a: CGPoint, _ b: CGPoint) -> CGFloat { hypot(a.x - b.x, a.y - b.y) }
    let from = midpoint(start.0, start.1)
    let to = midpoint(now.0, now.1)
    let startGap = gap(start.0, start.1)
    return TwoFingerIntent.classify(
        translation: CGPoint(x: to.x - from.x, y: to.y - from.y),
        spreadChange: gap(now.0, now.1) - startGap, startGap: startGap)
}

/// A two-finger pan that keeps its own touch starts for `TwoFingerIntent`.
final class TwoFingerPan: UIPanGestureRecognizer {
    private(set) var starts = TouchStarts()

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent) {
        for touch in touches { starts.began(ObjectIdentifier(touch), at: touch.location(in: view)) }
        super.touchesBegan(touches, with: event)
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent) {
        for touch in touches { starts.ended(ObjectIdentifier(touch)) }
        super.touchesEnded(touches, with: event)
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent) {
        for touch in touches { starts.ended(ObjectIdentifier(touch)) }
        super.touchesCancelled(touches, with: event)
    }

    override func reset() {
        starts.reset()
        super.reset()
    }
}

/// A pinch that keeps its own touch starts for `TwoFingerIntent`.
final class TwoFingerPinch: UIPinchGestureRecognizer {
    private(set) var starts = TouchStarts()

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent) {
        for touch in touches { starts.began(ObjectIdentifier(touch), at: touch.location(in: view)) }
        super.touchesBegan(touches, with: event)
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent) {
        for touch in touches { starts.ended(ObjectIdentifier(touch)) }
        super.touchesEnded(touches, with: event)
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent) {
        for touch in touches { starts.ended(ObjectIdentifier(touch)) }
        super.touchesCancelled(touches, with: event)
    }

    override func reset() {
        starts.reset()
        super.reset()
    }
}
