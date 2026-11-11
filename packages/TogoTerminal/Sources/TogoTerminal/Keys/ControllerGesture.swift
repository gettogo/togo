import Foundation

/// The controller's one gesture, as a value that takes touch samples and says what happened,
/// so the thresholds are tested without a view.
///
/// A move of `slop` or more before `holdDelay` is a flick: the direction is the nearest
/// quadrant of the vector from touchdown, sent at once and repeated while the finger stays;
/// changing quadrant sends the new arrow at once. Staying still for `holdDelay` is a hold: the
/// face lifts, and letting go opens the palette in edit mode. Letting go before `tapLimit`
/// without moving is a tap: the palette.
public struct ControllerGesture: Equatable, Sendable {
    public enum Direction: String, Equatable, Sendable {
        case up, down, left, right

        public var stroke: KeyStroke {
            switch self {
            case .up: KeyStroke(.up)
            case .down: KeyStroke(.down)
            case .left: KeyStroke(.left)
            case .right: KeyStroke(.right)
            }
        }
    }

    public enum Event: Equatable, Sendable {
        /// The finger moved into a quadrant: send this arrow and start repeating it.
        case arrow(Direction)
        /// The finger stayed still: the face lifts.
        case holdBegan
        /// The finger lifted after a hold without moving: the palette in edit mode.
        case holdEnded
        /// A short touch: the palette.
        case tap
        /// The finger lifted after a flick: stop repeating.
        case flickEnded
        /// The touch was taken away; stop everything.
        case cancelled
    }

    public enum Phase: Equatable, Sendable {
        case idle
        /// Touched, not yet a flick or a hold.
        case undecided
        case flick(Direction)
        case held
    }

    public static let slop: CGFloat = 8
    public static let holdDelay: TimeInterval = 0.35
    public static let tapLimit: TimeInterval = 0.25
    /// Past this distance from touchdown the repeat runs at its fast rate.
    public static let fastRadius: CGFloat = 30

    public private(set) var phase: Phase = .idle
    /// The repeat is at its fast rate.
    public private(set) var fast = false
    /// Flicks send arrows; off, any movement is ignored and only tap and hold remain.
    public var flicks = true

    private var origin = CGPoint.zero
    private var began: TimeInterval = 0

    public init(flicks: Bool = true) {
        self.flicks = flicks
    }

    public mutating func touchBegan(at point: CGPoint, time: TimeInterval) -> [Event] {
        origin = point
        began = time
        fast = false
        phase = .undecided
        return []
    }

    public mutating func touchMoved(to point: CGPoint, time: TimeInterval) -> [Event] {
        let vector = CGVector(dx: point.x - origin.x, dy: point.y - origin.y)
        let distance = hypot(vector.dx, vector.dy)
        switch phase {
        case .undecided:
            guard flicks, distance >= Self.slop else { return [] }
            // A hold the timer has not yet reported is still a flick if the move came first.
            guard time - began < Self.holdDelay else { return [] }
            let direction = Self.direction(of: vector)
            phase = .flick(direction)
            fast = distance >= Self.fastRadius
            return [.arrow(direction)]
        case .flick(let current):
            fast = distance >= Self.fastRadius
            let direction = Self.direction(of: vector)
            guard direction != current, distance >= Self.slop else { return [] }
            phase = .flick(direction)
            return [.arrow(direction)]
        case .held, .idle:
            return []
        }
    }

    /// The hold timer fired at `time`: a hold, unless a flick already began.
    public mutating func holdTimerFired(time: TimeInterval) -> [Event] {
        guard phase == .undecided, time - began >= Self.holdDelay else { return [] }
        phase = .held
        return [.holdBegan]
    }

    public mutating func touchEnded(time: TimeInterval) -> [Event] {
        defer { phase = .idle }
        switch phase {
        case .flick: return [.flickEnded]
        case .held: return [.holdEnded]
        case .undecided: return time - began < Self.tapLimit ? [.tap] : []
        case .idle: return []
        }
    }

    public mutating func touchCancelled() -> [Event] {
        defer { phase = .idle }
        return phase == .idle ? [] : [.cancelled]
    }

    /// The nearest of four quadrants, 90° each, with no diagonals.
    public static func direction(of vector: CGVector) -> Direction {
        if abs(vector.dx) >= abs(vector.dy) {
            return vector.dx >= 0 ? .right : .left
        }
        return vector.dy >= 0 ? .down : .up
    }
}

/// How a flicked arrow repeats: 400 ms before the first repeat, then 12 a second, 25 a second
/// once the finger is far from touchdown.
public enum ArrowRepeat {
    public static let initialDelay: TimeInterval = 0.4
    public static let interval: TimeInterval = 1 / 12
    public static let fastInterval: TimeInterval = 1 / 25

    public static func delay(beforeRepeat count: Int, fast: Bool) -> TimeInterval {
        if count == 0 { return initialDelay }
        return fast ? fastInterval : interval
    }
}
