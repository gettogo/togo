import MetalKit
import QuartzCore
import UIKit

/// Hosts one engine's view inside the safe area and above the keyboard, so showing the
/// keyboard shrinks the grid and reaches the remote as a resize. The canvas is opaque: glass
/// belongs to chrome only.
///
/// The grid is fitted by hand rather than by constraints: its frame is the safe area above the
/// keyboard, cut to what the window and any clipping container actually show. A container
/// that is wider than the part of it on screen, as a page or a column can be for a moment
/// while a fold, rotation or sidebar settles, would otherwise give the grid columns past the
/// edge, and the remote would draw text there that nobody sees.
@MainActor public final class TerminalViewController: UIViewController {
    public let engine: SwiftTermEngine

    /// Off by default: a running display link holds the display at its top refresh rate.
    public var recordsFrameStats = false {
        didSet { updateDisplayLink() }
    }
    public private(set) var frameStats = FrameStats()
    /// Attached to the root view, so the left-edge pan also starts in the safe-area margin.
    public let gestures: GestureController

    private var displayLink: CADisplayLink?
    private let root = UIView()
    /// Follows the keyboard, docked, split or floating, so its moves lay the root out again.
    private let keyboardProbe = UIView()
    /// A second fit after things settle, for containers that move without resizing the root.
    private var settleFit: Task<Void, Never>?
    /// Keeps a grid that has a size at that size, as while an animation resizes the host on
    /// every frame. Turning it off fits the grid to the space it has then.
    public var holdsFit = false {
        didSet { if oldValue, !holdsFit { refit() } }
    }
    /// Space kept between the safe area and the grid, so the grid's first column and row can
    /// line up with a layer drawn over it. The cell remainder goes after the last column and
    /// row, so the leading and top edges are exactly these.
    public var gridMargins: UIEdgeInsets = .zero {
        didSet { if gridMargins != oldValue { refit() } }
    }
    /// Another layer covers the terminal, as the chat does. The keyboard is then that layer's,
    /// so the grid fits as if none were up: typing above never resizes the remote, and the
    /// grid already has its size when the layer goes. Covering or uncovering keeps the grid it
    /// has while the layer crossfades, and fits it after (`layerSettle`), so a keyboard
    /// leaving with the switch never reflows the text mid-fade.
    public var isCovered = false {
        didSet {
            guard isCovered != oldValue else { return }
            layerFit?.cancel()
            layerHeld = true
            layerFit = Task { [weak self] in
                try? await Task.sleep(for: Self.layerSettle)
                guard !Task.isCancelled, let self else { return }
                self.layerHeld = false
                self.refit()
            }
        }
    }
    /// Longer than the layers' crossfade.
    public static let layerSettle: Duration = .milliseconds(350)
    private var layerHeld = false
    private var layerFit: Task<Void, Never>?

    public init(engine: SwiftTermEngine, gestureConfig: GestureConfig = GestureConfig()) {
        self.engine = engine
        gestures = GestureController(engine: engine, view: root, bindings: gestureConfig)
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) {
        nil
    }

    override public func loadView() {
        view = root
    }

    override public func viewDidLoad() {
        super.viewDidLoad()
        let terminal = engine.view
        terminal.translatesAutoresizingMaskIntoConstraints = true
        terminal.autoresizingMask = []
        view.addSubview(terminal)
        view.keyboardLayoutGuide.followsUndockedKeyboard = true
        keyboardProbe.isHidden = true
        keyboardProbe.isUserInteractionEnabled = false
        keyboardProbe.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(keyboardProbe)
        NSLayoutConstraint.activate([
            keyboardProbe.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            keyboardProbe.widthAnchor.constraint(equalToConstant: 1),
            keyboardProbe.topAnchor.constraint(equalTo: view.keyboardLayoutGuide.topAnchor),
            keyboardProbe.bottomAnchor.constraint(equalTo: view.bottomAnchor),
        ])
    }

    override public func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        fitTerminal()
        scheduleSettleFit()
    }

    override public func viewSafeAreaInsetsDidChange() {
        super.viewSafeAreaInsetsDidChange()
        view.setNeedsLayout()
    }

    override public func viewWillTransition(to size: CGSize, with coordinator: any UIViewControllerTransitionCoordinator) {
        super.viewWillTransition(to: size, with: coordinator)
        coordinator.animate(alongsideTransition: nil) { [weak self] _ in self?.refit() }
    }

    /// Fits the grid again now. The host calls it whenever the space it gives the terminal may
    /// have changed: a fold, a rotation, a sidebar or header coming or going.
    public func refit() {
        guard isViewLoaded else { return }
        view.setNeedsLayout()
        view.layoutIfNeeded()
        fitTerminal()
    }

    private func scheduleSettleFit() {
        settleFit?.cancel()
        settleFit = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(350))
            guard !Task.isCancelled else { return }
            self?.fitTerminal()
        }
    }

    private func fitTerminal() {
        if holdsFit || layerHeld, !engine.view.bounds.isEmpty { return }
        let rect = TerminalFit.frame(
            bounds: view.bounds, safeArea: view.safeAreaInsets, keyboard: isCovered ? nil : keyboardFrame,
            visible: visibleBounds(), margins: gridMargins
        )
        let terminal = engine.view
        // A space that holds no grid, as for a moment in a fold or with a keyboard over a short
        // pane, keeps the grid it has: fitted, it would reflow every line and the scrollback to
        // two columns and tell the remote it has no rows.
        if !terminal.bounds.isEmpty, !TerminalFit.holdsGrid(rect.size, cell: engine.cellSize) { return }
        // Bounds and center, not frame: a pinch in flight scales the view with a transform, and
        // a scroll view's bounds origin is its scroll position.
        if terminal.bounds.size != rect.size {
            terminal.bounds = CGRect(origin: terminal.bounds.origin, size: rect.size)
        }
        let center = CGPoint(x: rect.midX, y: rect.midY)
        if terminal.center != center { terminal.center = center }
        matchMetalScale()
    }

    /// SwiftTerm's Metal renderer sizes glyphs and cells in pixels with `UIScreen.main.scale`,
    /// while its MTKView takes the scale of the screen it is on. Where the two differ, as on an
    /// unfolded Duo, every glyph is drawn larger or smaller than its cell: the grid has the
    /// right number of columns, but larger glyphs push the last ones past the edge. The view
    /// is held to the renderer's scale, so a cell is drawn at the size the grid was measured with.
    private func matchMetalScale() {
        let scale = (RendererScale.self as any RendererScaleSource.Type).scale
        guard scale > 0 else { return }
        for case let metal as MTKView in engine.view.subviews where metal.contentScaleFactor != scale {
            metal.contentScaleFactor = scale
            metal.setNeedsDisplay()
        }
    }

    /// The keyboard over this view, docked or split across it. A small floating keyboard
    /// leaves the grid alone: it covers a corner, not the bottom rows.
    private var keyboardFrame: CGRect? {
        let frame = view.keyboardLayoutGuide.layoutFrame
        guard frame.height > 1, frame.width >= view.bounds.width * 0.6 else { return nil }
        return frame
    }

    /// The part of the root on screen: inside the window and inside every clipping container
    /// other than a scroll view. A pager's page waiting beside the one on screen is outside the
    /// window by design, so a root that is mostly hidden, or one mid-transform in a transition,
    /// keeps its own bounds.
    private func visibleBounds() -> CGRect {
        guard let window = view.window else { return view.bounds }
        var rect = view.bounds.intersection(view.convert(window.bounds, from: window))
        var ancestor = view.superview
        while let current = ancestor, current !== window {
            if !current.transform.isIdentity || !CATransform3DIsIdentity(current.layer.transform) {
                return view.bounds
            }
            if current.clipsToBounds, !(current is UIScrollView) {
                rect = rect.intersection(view.convert(current.bounds, from: current))
            }
            ancestor = current.superview
        }
        guard !rect.isNull, rect.width >= view.bounds.width * 0.5, rect.height >= view.bounds.height * 0.5 else {
            return view.bounds
        }
        return rect
    }

    /// The margins around the grid take the theme background, which can change at any time.
    override public func viewWillLayoutSubviews() {
        super.viewWillLayoutSubviews()
        view.backgroundColor = engine.backgroundColor
    }

    override public func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        engine.useMetalIfAvailable()
        engine.view.becomeFirstResponder()
        updateDisplayLink()
        refit()
    }

    override public func viewDidDisappear(_ animated: Bool) {
        super.viewDidDisappear(animated)
        displayLink?.invalidate()
        displayLink = nil
    }

    private func updateDisplayLink() {
        guard recordsFrameStats, viewIfLoaded?.window != nil else {
            displayLink?.invalidate()
            displayLink = nil
            return
        }
        guard displayLink == nil else { return }
        frameStats = FrameStats()
        let link = CADisplayLink(target: self, selector: #selector(displayTick(_:)))
        link.preferredFrameRateRange = CAFrameRateRange(minimum: 80, maximum: 120, preferred: 120)
        link.add(to: .main, forMode: .common)
        displayLink = link
    }

    @objc private func displayTick(_ link: CADisplayLink) {
        frameStats.record(timestamp: link.timestamp, targetTimestamp: link.targetTimestamp)
    }
}

/// The scale SwiftTerm's iOS renderer reads, `UIScreen.main.scale`. It is read through a
/// protocol because `UIScreen.main` is deprecated, and the point is to match SwiftTerm's own use.
@MainActor private protocol RendererScaleSource {
    static var scale: CGFloat { get }
}

private enum RendererScale: RendererScaleSource {
    @available(iOS, deprecated: 26.0)
    static var scale: CGFloat { UIScreen.main.scale }
}

/// Where the grid goes inside its host view.
enum TerminalFit {
    /// The safe area above the keyboard, cut to the visible part of the view and inset by
    /// `margins`, in whole points so the column count does not flicker on a fraction.
    static func frame(
        bounds: CGRect, safeArea: UIEdgeInsets, keyboard: CGRect?, visible: CGRect, margins: UIEdgeInsets = .zero
    ) -> CGRect {
        var rect = bounds.inset(by: safeArea)
        if let keyboard, keyboard.minY < rect.maxY {
            rect.size.height = max(0, keyboard.minY - rect.minY)
        }
        rect = rect.intersection(visible)
        guard !rect.isNull else { return .zero }
        rect = rect.inset(by: margins)
        guard rect.width > 0, rect.height > 0 else { return .zero }
        return CGRect(
            x: rect.minX.rounded(.up), y: rect.minY.rounded(.up),
            width: max(0, rect.width.rounded(.down)), height: max(0, rect.height.rounded(.down))
        )
    }

    /// True when `size` fits the smallest grid the emulator keeps, two columns by one row of
    /// `cell`. Before the cell is known any size that is not empty counts.
    static func holdsGrid(_ size: CGSize, cell: CGSize) -> Bool {
        guard cell.width > 0, cell.height > 0 else { return size.width > 0 && size.height > 0 }
        return size.width >= cell.width * CGFloat(SwiftTermEngine.minimumCols)
            && size.height >= cell.height * CGFloat(SwiftTermEngine.minimumRows)
    }
}

/// Posted on the main thread whenever a terminal takes or gives up the keyboard, so the host
/// can tell typing into a terminal apart from typing into a field.
///
/// The terminal that has the keyboard is kept here rather than found by walking the responder
/// chain: SwiftTerm's view refuses every action it does not know in `canPerformAction`, so an
/// action sent to the first responder passes the terminal by and lands on its superview.
public enum TerminalFocus {
    public static let didChange = Notification.Name("TerminalFocusDidChange")

    @MainActor private static weak var focused: TogoTerminalView?

    /// True while a terminal on screen is the first responder, so typing goes to it.
    @MainActor public static var hasKeyboard: Bool {
        guard let focused else { return false }
        return focused.isFirstResponder && focused.window != nil
    }

    /// `view` took or gave up the keyboard.
    @MainActor static func changed(_ view: TogoTerminalView) {
        if view.isFirstResponder {
            focused = view
        } else if focused === view {
            focused = nil
        }
        NotificationCenter.default.post(name: didChange, object: nil)
    }
}
