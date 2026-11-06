import UIKit

/// What the pointer needs from an engine. Points are in `pointerView` coordinates.
@MainActor protocol PointerTarget: AnyObject {
    /// The terminal itself, a scroll view.
    var pointerView: UIView { get }
    var mouseTracking: MouseTracking { get }
    var lineHeight: CGFloat { get }
    func cell(at point: CGPoint) -> GridCell
    func sendMouse(button: Int, release: Bool, motion: Bool, at cell: GridCell)
    /// Vertical travel through `ScrollRouter`: wheel events while the remote reports the mouse,
    /// local scrollback otherwise.
    func scroll(by distance: CGFloat, at point: CGPoint)
    func endScroll()
    func select(from start: CGPoint, to end: CGPoint)
    func showEditMenu(at point: CGPoint)
}

/// A trackpad or mouse on the terminal.
///
/// The pointer is an I-beam over the grid. While the remote reports the mouse, clicks, drags
/// and the secondary button go to it. Without reporting, a click-drag selects and a secondary
/// click opens the edit menu. Trackpad and wheel scrolling take the same route as a finger,
/// through `ScrollRouter`, so SwiftTerm's own pointer scroll recognizers are taken over.
///
/// Hover stays with SwiftTerm, which reports motion with no button held only in any-event
/// mode (1003); its public mouse API has no way to send a buttonless motion.
@MainActor public final class PointerController: NSObject, UIGestureRecognizerDelegate, UIPointerInteractionDelegate {
    static let left = 0
    /// X11 button 3, the secondary button, which goes out as button code 2.
    static let secondary = 2

    /// Shows the edit menu at a point. Replaced in tests.
    var showMenu: (CGPoint) -> Void

    private weak var target: (any PointerTarget)?
    private var dragStart: CGPoint?
    private var dragCell: GridCell?
    /// Whether the drag in progress reports to the remote rather than selecting.
    private var dragReports = false
    private var scrollPan: UIPanGestureRecognizer?

    init(target: any PointerTarget, host: UIView) {
        self.target = target
        showMenu = { [weak target] in target?.showEditMenu(at: $0) }
        super.init()
        install(on: target.pointerView, host: host)
    }

    /// The I-beam, one line tall.
    var style: (length: CGFloat, axis: UIAxis) {
        (target?.lineHeight ?? 0, .vertical)
    }

    // MARK: Events

    func secondaryClick(at point: CGPoint) {
        guard let target else { return }
        guard target.mouseTracking != .off else {
            showMenu(point)
            return
        }
        let cell = target.cell(at: point)
        target.sendMouse(button: Self.secondary, release: false, motion: false, at: cell)
        target.sendMouse(button: Self.secondary, release: true, motion: false, at: cell)
    }

    func scroll(by distance: CGFloat, at point: CGPoint) {
        target?.scroll(by: distance, at: point)
    }

    func dragBegan(at point: CGPoint) {
        guard let target else { return }
        dragStart = point
        dragReports = target.mouseTracking != .off
        guard dragReports else {
            target.select(from: point, to: point)
            return
        }
        let cell = target.cell(at: point)
        dragCell = cell
        target.sendMouse(button: Self.left, release: false, motion: false, at: cell)
    }

    /// Motion goes out once per cell crossed, not once per pointer sample.
    func dragMoved(to point: CGPoint) {
        guard let target, let start = dragStart else { return }
        guard dragReports else {
            target.select(from: start, to: point)
            return
        }
        let cell = target.cell(at: point)
        guard cell != dragCell else { return }
        dragCell = cell
        target.sendMouse(button: Self.left, release: false, motion: true, at: cell)
    }

    /// A cancelled drag still releases, or the remote keeps the button held.
    func dragEnded(at point: CGPoint) {
        defer {
            dragStart = nil
            dragCell = nil
        }
        guard let target, let start = dragStart else { return }
        guard dragReports else {
            target.select(from: start, to: point)
            return
        }
        target.sendMouse(button: Self.left, release: true, motion: false, at: dragCell ?? target.cell(at: point))
    }

    // MARK: Installation

    private func install(on terminal: UIView, host: UIView) {
        for case let pan as UIPanGestureRecognizer in terminal.gestureRecognizers ?? [] where pan.allowedTouchTypes.isEmpty {
            takeOverScroll(pan)
        }
        // A click-drag selects or reports instead of moving the scrollback.
        (terminal as? UIScrollView)?.panGestureRecognizer.allowedTouchTypes = [
            NSNumber(value: UITouch.TouchType.direct.rawValue), NSNumber(value: UITouch.TouchType.pencil.rawValue),
        ]

        let click = UITapGestureRecognizer(target: self, action: #selector(secondaryClicked(_:)))
        click.buttonMaskRequired = .secondary
        click.allowedTouchTypes = [NSNumber(value: UITouch.TouchType.indirectPointer.rawValue)]
        host.addGestureRecognizer(click)

        // On the host: the terminal view turns off one-touch pans added to it.
        let drag = UIPanGestureRecognizer(target: self, action: #selector(dragged(_:)))
        drag.maximumNumberOfTouches = 1
        drag.allowedTouchTypes = [NSNumber(value: UITouch.TouchType.indirectPointer.rawValue)]
        drag.delegate = self
        host.addGestureRecognizer(drag)

        // SwiftTerm's own interaction draws the default pointer; this one forwards to it.
        for interaction in terminal.interactions where interaction is UIPointerInteraction {
            terminal.removeInteraction(interaction)
        }
        terminal.addInteraction(UIPointerInteraction(delegate: self))
    }

    /// SwiftTerm keeps one recognizer for continuous and one for discrete scrolling, and its
    /// scroll view pan waits for them to fail. The first takes every kind; the other fails at once.
    private func takeOverScroll(_ pan: UIPanGestureRecognizer) {
        pan.removeTarget(nil, action: nil)
        pan.delegate = self
        guard scrollPan == nil else { return }
        pan.allowedScrollTypesMask = .all
        pan.addTarget(self, action: #selector(scrolled(_:)))
        scrollPan = pan
    }

    // MARK: Recognizers

    @objc private func secondaryClicked(_ click: UITapGestureRecognizer) {
        guard click.state == .ended, let view = target?.pointerView else { return }
        secondaryClick(at: click.location(in: view))
    }

    @objc private func scrolled(_ pan: UIPanGestureRecognizer) {
        guard let view = target?.pointerView else { return }
        switch pan.state {
        case .began, .changed:
            let distance = pan.translation(in: view).y
            pan.setTranslation(.zero, in: view)
            scroll(by: distance, at: pan.location(in: view))
        default:
            target?.endScroll()
        }
    }

    @objc private func dragged(_ pan: UIPanGestureRecognizer) {
        guard let view = target?.pointerView else { return }
        let point = pan.location(in: view)
        switch pan.state {
        case .began:
            // The pan begins after some travel; the press belongs where the drag started.
            let travel = pan.translation(in: view)
            dragBegan(at: CGPoint(x: point.x - travel.x, y: point.y - travel.y))
            dragMoved(to: point)
        case .changed: dragMoved(to: point)
        case .ended, .cancelled, .failed: dragEnded(at: point)
        default: break
        }
    }

    public func gestureRecognizerShouldBegin(_ recognizer: UIGestureRecognizer) -> Bool {
        if let pan = recognizer as? UIPanGestureRecognizer, pan.allowedTouchTypes.isEmpty {
            return pan === scrollPan
        }
        return true
    }

    // MARK: UIPointerInteractionDelegate

    /// SwiftTerm still sees every region request, for its link tracking.
    public func pointerInteraction(
        _ interaction: UIPointerInteraction, regionFor request: UIPointerRegionRequest, defaultRegion: UIPointerRegion
    ) -> UIPointerRegion? {
        _ = (target?.pointerView as? UIPointerInteractionDelegate)?
            .pointerInteraction?(interaction, regionFor: request, defaultRegion: defaultRegion)
        return defaultRegion
    }

    public func pointerInteraction(_ interaction: UIPointerInteraction, styleFor region: UIPointerRegion) -> UIPointerStyle? {
        let style = self.style
        return UIPointerStyle(shape: .verticalBeam(length: max(style.length, 1)), constrainedAxes: style.axis)
    }
}

extension SwiftTermEngine: PointerTarget {
    var pointerView: UIView { terminalView }
    var lineHeight: CGFloat { cellSize.height }

    func scroll(by distance: CGFloat, at point: CGPoint) {
        pan(by: distance, at: point)
    }

    func endScroll() {
        terminalView.onWheelPanEnded?()
    }

    func select(from start: CGPoint, to end: CGPoint) {
        let a = cell(at: start)
        let b = cell(at: end)
        var (first, last) = (a.row, a.col) <= (b.row, b.col) ? (a, b) : (b, a)
        // Starting on the trailing half of a wide glyph starts on the glyph.
        if first.col > 0, terminalView.cellWidths(row: first.row).dropFirst(first.col).first == 0 {
            first.col -= 1
        }
        // The end is exclusive, so it goes one past the cell under the pointer, and past both
        // halves of a wide glyph.
        let width = terminalView.cellWidths(row: last.row).dropFirst(last.col).first ?? 1
        terminalView.select(from: first, to: GridCell(col: last.col + max(width, 1), row: last.row))
    }

    func showEditMenu(at point: CGPoint) {
        terminalView.showEditMenu(at: point)
    }
}
