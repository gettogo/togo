import UIKit

public struct GestureConfig: Equatable, Sendable {
    /// Bytes a double tap sends; nil leaves it to word selection. Tab by default.
    public var doubleTap: [UInt8]?
    /// Bytes a triple tap sends; nil leaves it to line selection. Escape by default.
    public var tripleTap: [UInt8]?
    public var twoFingerSwipe: Bool
    public var edgeSwipe: Bool
    public var pinchZoom: Bool
    /// One-finger drags report as mouse drags while the remote tracks the mouse, instead of
    /// scrolling. Off by default.
    public var touchMouse: Bool

    public init(
        doubleTap: [UInt8]? = [0x09],
        tripleTap: [UInt8]? = [0x1B],
        twoFingerSwipe: Bool = true,
        edgeSwipe: Bool = true,
        pinchZoom: Bool = true,
        touchMouse: Bool = false
    ) {
        self.doubleTap = doubleTap
        self.tripleTap = tripleTap
        self.twoFingerSwipe = twoFingerSwipe
        self.edgeSwipe = edgeSwipe
        self.pinchZoom = pinchZoom
        self.touchMouse = touchMouse
    }
}

/// What the gestures need from an engine beyond `TerminalEngine`. Points are in
/// `gestureView` coordinates.
@MainActor protocol GestureTarget: AnyObject {
    var gestureView: UIView { get }
    var isSelecting: Bool { get }
    /// True when the remote asked for mouse reports.
    var tracksMouse: Bool { get }
    /// SwiftTerm's own scroll view pan, which also tracks two fingers.
    var scrollPan: UIGestureRecognizer { get }
    /// Whether the software keyboard is up, so typing reaches the remote.
    var hasKeyboardFocus: Bool { get }
    var fontSize: CGFloat { get }
    func setFontSize(_ size: CGFloat)
    func charWidth(forFontSize size: CGFloat) -> CGFloat
    func sendBytes(_ bytes: [UInt8])
    func cell(at point: CGPoint) -> GridCell
    func sendMouse(button: Int, release: Bool, motion: Bool, at cell: GridCell)
    func bareLink(at point: CGPoint) -> URL?
    func open(_ url: URL)
    func copyToClipboard(_ text: String)
}

/// Terminal gestures on top of SwiftTerm's own taps, long press, selection and scrolling.
///
/// Recognizers attach to `view`, normally the controller's root view, so the left-edge pan
/// also starts in the safe-area margin. Precedence over SwiftTerm's recognizers is decided per
/// touch through the delegate rather than fixed `require(toFail:)` links, so changing
/// `bindings` rebuilds only Togo's recognizers and never leaves SwiftTerm's waiting on one
/// that is gone.
///
/// The session swipe and the pinch run alongside SwiftTerm's scroll view pan and begin only
/// for their own motion (`TwoFingerIntent`); when one begins it cancels the scroll. A
/// vertical two-finger drag stays a scroll.
@MainActor public final class GestureController: NSObject, UIGestureRecognizerDelegate {
    public enum Direction: Sendable {
        /// Fingers moved toward the left edge, conventionally the next session.
        case left
        /// Fingers moved toward the right edge, conventionally the previous session.
        case right
    }

    public var onSwipeSession: (Direction) -> Void = { _ in }
    public var onEdgeSwipe: () -> Void = {}
    /// Called once per pinch, after the size settled and the engine took it.
    public var onFontSize: (CGFloat) -> Void = { _ in }
    /// "Links on Screen" in a link's long-press menu.
    public var onShowLinks: () -> Void = {}

    public var bindings: GestureConfig {
        didSet {
            guard bindings != oldValue else { return }
            install()
        }
    }

    private weak var target: (any GestureTarget)?
    private weak var view: UIView?
    private var recognizers: [UIGestureRecognizer] = []
    private var doubleTap: UITapGestureRecognizer?
    private var tripleTap: UITapGestureRecognizer?
    private var linkTap: UITapGestureRecognizer?
    private var linkPress: UILongPressGestureRecognizer?
    private var mousePan: UIPanGestureRecognizer?
    private var swipePan: TwoFingerPan?
    private var pinch: TwoFingerPinch?
    private var edgePan: UIScreenEdgePanGestureRecognizer?
    private var rightClick: UITapGestureRecognizer?
    private var pinchStartSize: CGFloat = 0
    private let haptics = UISelectionFeedbackGenerator()
    private var touchMouse: TouchMouse?
    private var linkMenu: LinkMenu?
    /// A long press on words selects them; their handles drag either end.
    private var touchSelection: TouchSelection?
    /// Trackpad and mouse: the I-beam, clicks, hover and scrolling.
    let pointer: PointerController?

    /// Gestures that read or write the grid need a `SwiftTermEngine`; with any other engine
    /// only the session swipes and the edge pan are installed.
    public init(engine: any TerminalEngine, view: UIView, bindings: GestureConfig) {
        self.target = engine as? any GestureTarget
        self.view = view
        self.bindings = bindings
        pointer = (engine as? any PointerTarget).map { PointerController(target: $0, host: view) }
        super.init()
        if let target {
            touchMouse = TouchMouse(target: target)
            let menu = LinkMenu(view: target.gestureView)
            menu.open = { [weak target] in target?.open($0) }
            menu.copy = { [weak target] in target?.copyToClipboard($0) }
            menu.showAll = { [weak self] in self?.onShowLinks() }
            linkMenu = menu
        }
        touchSelection = (engine as? any TouchSelectionTarget).map(TouchSelection.init(target:))
        install()
    }

    // MARK: Installation

    private func install() {
        guard view != nil else { return }
        recognizers.forEach { $0.view?.removeGestureRecognizer($0) }
        recognizers.removeAll()
        doubleTap = nil
        tripleTap = nil
        linkTap = nil
        linkPress = nil
        mousePan = nil
        swipePan = nil
        pinch = nil
        edgePan = nil
        rightClick = nil

        if bindings.twoFingerSwipe {
            let swipe = TwoFingerPan(target: self, action: #selector(swiped(_:)))
            swipe.minimumNumberOfTouches = 2
            swipe.maximumNumberOfTouches = 2
            swipePan = swipe
            add(swipe)
        }
        if bindings.edgeSwipe {
            let edge = UIScreenEdgePanGestureRecognizer(target: self, action: #selector(edgePanned(_:)))
            edge.edges = .left
            edgePan = edge
            add(edge)
        }
        guard target != nil else { return }
        if bindings.pinchZoom {
            let zoom = TwoFingerPinch(target: self, action: #selector(pinched(_:)))
            pinch = zoom
            add(zoom)
        }
        if bindings.tripleTap != nil {
            let tap = tapRecognizer(taps: 3)
            tripleTap = tap
            add(tap)
        }
        if bindings.doubleTap != nil {
            let tap = tapRecognizer(taps: 2)
            doubleTap = tap
            add(tap)
        }
        let link = tapRecognizer(taps: 1)
        linkTap = link
        add(link)
        let press = UILongPressGestureRecognizer(target: self, action: #selector(longPressed(_:)))
        press.minimumPressDuration = 0.5
        linkPress = press
        add(press)
        if let touchSelection {
            add(touchSelection.pan)
        }
        if bindings.touchMouse {
            installTouchMouse()
        }
    }

    private func installTouchMouse() {
        let pan = UIPanGestureRecognizer(target: self, action: #selector(mousePanned(_:)))
        pan.maximumNumberOfTouches = 1
        pan.allowedTouchTypes = [NSNumber(value: UITouch.TouchType.direct.rawValue)]
        mousePan = pan
        add(pan)
        let tap = UITapGestureRecognizer(target: self, action: #selector(twoFingerTapped(_:)))
        tap.numberOfTouchesRequired = 2
        rightClick = tap
        add(tap)
    }

    private func tapRecognizer(taps: Int) -> UITapGestureRecognizer {
        let tap = UITapGestureRecognizer(target: self, action: #selector(tapped(_:)))
        tap.numberOfTapsRequired = taps
        return tap
    }

    private func add(_ recognizer: UIGestureRecognizer) {
        recognizer.delegate = self
        view?.addGestureRecognizer(recognizer)
        recognizers.append(recognizer)
    }

    // MARK: Actions

    /// Sends the bound sequence for a double or triple tap; false when nothing was sent.
    @discardableResult
    func handleTaps(_ count: Int) -> Bool {
        guard let target, let bytes = tapBindings.sequence(forTaps: count, isSelecting: target.isSelecting) else {
            return false
        }
        target.sendBytes(bytes)
        return true
    }

    private var tapBindings: TapBindings {
        TapBindings(doubleTap: bindings.doubleTap, tripleTap: bindings.tripleTap)
    }

    @objc private func tapped(_ tap: UITapGestureRecognizer) {
        guard tap.state == .ended else { return }
        if tap === linkTap {
            guard let target, let url = target.bareLink(at: tap.location(in: target.gestureView)) else { return }
            target.open(url)
        } else {
            handleTaps(tap.numberOfTapsRequired)
        }
    }

    /// A link's menu on a link; anywhere else the word under the finger is selected with its
    /// handles and Copy, as in any iOS text, rather than SwiftTerm's Select / Select All menu.
    @objc private func longPressed(_ press: UILongPressGestureRecognizer) {
        guard press.state == .began, let target else { return }
        let point = press.location(in: target.gestureView)
        if let url = target.bareLink(at: point), let linkMenu {
            haptics.selectionChanged()
            linkMenu.present(url, at: point)
        } else if let touchSelection {
            // A blank keeps SwiftTerm's menu, for Paste.
            if touchSelection.selectWord(at: point) { haptics.selectionChanged() } else { touchSelection.showMenu(at: point) }
        }
    }

    @objc private func swiped(_ pan: UIPanGestureRecognizer) {
        switch pan.state {
        case .began, .changed:
            cancelScrolling()
        case .ended:
            guard let view, let direction = TwoFingerIntent.swipeDirection(
                translation: pan.translation(in: view), velocity: pan.velocity(in: view)) else { return }
            onSwipeSession(direction)
        default:
            break
        }
    }

    /// Toggling `isEnabled` is UIKit's way to cancel a recognizer mid-gesture. Called on every
    /// sample of a swipe or pinch, since the scroll pan may only begin after they have.
    private func cancelScrolling() {
        guard let scroll = target?.scrollPan, scroll.state == .began || scroll.state == .changed else { return }
        scroll.isEnabled = false
        scroll.isEnabled = true
    }

    @objc private func edgePanned(_ pan: UIScreenEdgePanGestureRecognizer) {
        guard pan.state == .ended, let view else { return }
        let travel = pan.translation(in: view).x
        let velocity = pan.velocity(in: view).x
        // A short drag let go without speed is a change of mind.
        if travel > view.bounds.width / 4 || velocity > 500 { onEdgeSwipe() }
    }

    /// The grid keeps its size during the pinch and is scaled as a picture; the font changes
    /// once, on release, so the remote sees a single resize.
    @objc private func pinched(_ pinch: UIPinchGestureRecognizer) {
        guard let target else { return }
        let terminal = target.gestureView
        switch pinch.state {
        case .began:
            pinchStartSize = target.fontSize
            haptics.prepare()
            cancelScrolling()
        case .changed:
            cancelScrolling()
            let size = clampedSize(pinchStartSize * pinch.scale)
            let scale = size / pinchStartSize
            terminal.transform = CGAffineTransform(scaleX: scale, y: scale)
        case .ended:
            terminal.transform = .identity
            let snapped = ZoomSnapper.snap(
                width: terminal.bounds.width, fontSize: clampedSize(pinchStartSize * pinch.scale),
                charWidth: target.charWidth(forFontSize:))
            haptics.selectionChanged()
            guard snapped != target.fontSize else { return }
            target.setFontSize(snapped)
            onFontSize(snapped)
        default:
            terminal.transform = .identity
        }
    }

    private func clampedSize(_ size: CGFloat) -> CGFloat {
        min(max(size, ZoomSnapper.sizeRange.lowerBound), ZoomSnapper.sizeRange.upperBound)
    }

    @objc private func mousePanned(_ pan: UIPanGestureRecognizer) {
        guard let target, let touchMouse, let view else { return }
        let point = pan.location(in: target.gestureView)
        switch pan.state {
        case .began: touchMouse.began(at: point, host: view)
        case .changed: touchMouse.moved(to: point)
        case .ended, .cancelled, .failed: touchMouse.ended(at: point)
        default: break
        }
    }

    @objc private func twoFingerTapped(_ tap: UITapGestureRecognizer) {
        guard tap.state == .ended, let target else { return }
        handleRightClick(at: tap.location(in: target.gestureView))
    }

    /// A shell that never asked for mouse reports gets nothing, not a stray packet as input.
    func handleRightClick(at point: CGPoint) {
        guard let target, target.tracksMouse else { return }
        touchMouse?.rightClick(at: point)
    }

    // MARK: UIGestureRecognizerDelegate

    public func gestureRecognizerShouldBegin(_ recognizer: UIGestureRecognizer) -> Bool {
        if let swipePan, recognizer === swipePan { return twoFingerMotion(swipePan, starts: swipePan.starts) == .swipe }
        guard let target else { return true }
        if let pinch, recognizer === pinch { return twoFingerMotion(pinch, starts: pinch.starts) == .pinch }
        if recognizer === rightClick { return target.tracksMouse }
        let point = recognizer.location(in: target.gestureView)
        if recognizer === doubleTap || recognizer === tripleTap {
            // With the keyboard down and a mouse-tracking app in front, SwiftTerm keeps the
            // double tap as the way to bring the keyboard back.
            if !target.hasKeyboardFocus && target.tracksMouse { return false }
            let count = (recognizer as? UITapGestureRecognizer)?.numberOfTapsRequired ?? 0
            return tapBindings.sequence(forTaps: count, isSelecting: target.isSelecting) != nil
        }
        if recognizer === linkTap {
            return !target.isSelecting && target.bareLink(at: point) != nil
        }
        if recognizer === linkPress {
            // Without touch selection (another engine) only a link takes the press.
            return !target.isSelecting && (touchSelection != nil || target.bareLink(at: point) != nil)
        }
        if let touchSelection, recognizer === touchSelection.pan {
            return touchSelection.shouldBegin()
        }
        if recognizer === mousePan {
            return !target.isSelecting && target.tracksMouse
        }
        return true
    }

    /// The swipe and pinch share two fingers with the scroll view pan until one of them
    /// begins and cancels the scroll.
    public func gestureRecognizer(
        _ recognizer: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer
    ) -> Bool {
        guard recognizer === swipePan || recognizer === pinch, let target else { return false }
        return other === target.scrollPan
    }

    /// SwiftTerm's taps wait for Togo's taps of the same or a higher count, its long press for
    /// the link press, and one-finger pans (scrolling, selection drag) for the mouse drag.
    /// Every pan on the terminal waits for the left-edge pan, which fails at once away from
    /// the edge.
    public func gestureRecognizer(
        _ recognizer: UIGestureRecognizer, shouldBeRequiredToFailBy other: UIGestureRecognizer
    ) -> Bool {
        guard let target, other.view === target.gestureView else { return false }
        if recognizer === edgePan { return other is UIPanGestureRecognizer }
        if let mine = recognizer as? UITapGestureRecognizer, mine.numberOfTouchesRequired == 1,
           let theirs = other as? UITapGestureRecognizer, theirs.numberOfTouchesRequired == 1 {
            return theirs.numberOfTapsRequired <= mine.numberOfTapsRequired
        }
        if recognizer === linkPress { return other is UILongPressGestureRecognizer }
        // Scrolling and SwiftTerm's own selection drag wait while a finger may be on a handle.
        if let touchSelection, recognizer === touchSelection.pan { return other is UIPanGestureRecognizer }
        if recognizer === mousePan, let pan = other as? UIPanGestureRecognizer {
            return pan.minimumNumberOfTouches == 1
        }
        return false
    }

    /// A tap waits for any one-finger tap with more taps, Togo's or SwiftTerm's, so a double
    /// tap never fires on the way to a triple. A mouse drag from the left edge is the edge pan.
    public func gestureRecognizer(
        _ recognizer: UIGestureRecognizer, shouldRequireFailureOf other: UIGestureRecognizer
    ) -> Bool {
        if recognizer === mousePan { return other is UIScreenEdgePanGestureRecognizer }
        guard let target, other.view === target.gestureView || recognizers.contains(other) else { return false }
        guard let mine = recognizer as? UITapGestureRecognizer, mine.numberOfTouchesRequired == 1,
              let theirs = other as? UITapGestureRecognizer, theirs.numberOfTouchesRequired == 1 else { return false }
        return theirs.numberOfTapsRequired > mine.numberOfTapsRequired
    }
}
