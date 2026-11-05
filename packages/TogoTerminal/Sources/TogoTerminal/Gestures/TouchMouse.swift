import UIKit

/// A grid cell in the visible screen, zero-based from the top left.
struct GridCell: Equatable, Sendable {
    var col: Int
    var row: Int
}

/// The mouse events a remote asked for with DECSET 9, 1000, 1002 or 1003.
enum MouseTracking: Equatable, Sendable {
    case off
    /// X10 (mode 9): presses only.
    case presses
    /// Normal tracking (1000): presses and releases.
    case clicks
    /// Button-event tracking (1002): clicks and motion while a button is down.
    case drags
    /// Any-event tracking (1003): clicks and all motion.
    case anyMotion

    /// Touch only ever moves with the button down, so 1002 and 1003 both take its motion.
    func reports(release: Bool, motion: Bool) -> Bool {
        switch self {
        case .off: false
        case .presses: !release && !motion
        case .clicks: !motion
        case .drags, .anyMotion: true
        }
    }
}

/// Mouse reports for a finger: a one-finger drag is a left-button drag and a two-finger tap
/// a right click. The remote picks the encoding (SGR with mode 1006); the engine sends only
/// the event classes its `MouseTracking` asked for.
@MainActor final class TouchMouse {
    static let left = 0
    static let right = 2

    private weak var target: (any GestureTarget)?
    private let loupe: Loupe
    private var lastCell: GridCell?

    init(target: any GestureTarget) {
        self.target = target
        loupe = Loupe(source: target.gestureView)
    }

    func began(at point: CGPoint, host: UIView) {
        guard let target else { return }
        let cell = target.cell(at: point)
        lastCell = cell
        target.sendMouse(button: Self.left, release: false, motion: false, at: cell)
        loupe.show(at: point, in: host)
    }

    /// Motion goes out once per cell crossed, not once per touch sample.
    func moved(to point: CGPoint) {
        guard let target else { return }
        loupe.move(to: point)
        let cell = target.cell(at: point)
        guard cell != lastCell else { return }
        lastCell = cell
        target.sendMouse(button: Self.left, release: false, motion: true, at: cell)
    }

    /// A cancelled drag still releases, or the remote keeps the button held.
    func ended(at point: CGPoint) {
        loupe.hide()
        guard let target else { return }
        let cell = lastCell ?? target.cell(at: point)
        lastCell = nil
        target.sendMouse(button: Self.left, release: true, motion: false, at: cell)
    }

    func rightClick(at point: CGPoint) {
        guard let target else { return }
        let cell = target.cell(at: point)
        target.sendMouse(button: Self.right, release: false, motion: false, at: cell)
        target.sendMouse(button: Self.right, release: true, motion: false, at: cell)
    }
}
