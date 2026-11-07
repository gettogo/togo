import UIKit

/// A terminal controller that can fit its grid to the space it has, on request.
@MainActor protocol TerminalRefitting: AnyObject {
    func refit()
    /// Keeps the grid at its size while on, as while an animation resizes the terminal every
    /// frame; turning it off fits the grid to the space it has then.
    func holdFit(_ on: Bool)
}

final class TerminalContainer: UIViewController {
    private var wanted: UIViewController?
    private var fitted: CGSize?
    /// The size the terminal keeps while an animation resizes the container, nil when none does.
    private var heldSize: CGSize?
    /// The terminal the hold was put on, let go when it lands even if it moved elsewhere.
    private weak var heldChild: (any TerminalRefitting)?
    /// When the container's size last changed, to tell an animation's frames from a single change.
    private var lastResize: ContinuousClock.Instant?
    private var lastBounds: CGSize = .zero
    /// The line "Show in terminal" landed on, over the terminal.
    private let band = JumpBandView()
    private var highlight = JumpHighlight()
    /// Another layer covers the terminal, as the chat does: it takes no touches and VoiceOver
    /// passes it by.
    private(set) var covered = false
    /// How many times the grid was fitted to a new size: once per fold, sidebar toggle or pane
    /// change, never per frame of an animation (motion spec §4).
    private(set) var fits = 0

    /// A container can be put on screen without appearing, as when a pager page that held
    /// something else starts showing a terminal, so it also attaches when its view reaches a
    /// window.
    override func loadView() {
        let view = WindowView()
        view.onWindow = { [weak self] in self?.attach() }
        self.view = view
    }

    private final class WindowView: UIView {
        var onWindow: () -> Void = {}

        override func didMoveToWindow() {
            super.didMoveToWindow()
            if window != nil { onWindow() }
        }
    }

    func show(_ child: UIViewController) {
        if let wanted, wanted !== child {
            release()
        }
        wanted = child
        if viewIfLoaded?.window != nil {
            attach()
        }
    }

    /// The chat covers the terminal or no longer does.
    func setCovered(_ on: Bool) {
        guard on != covered else { return }
        covered = on
        view.isUserInteractionEnabled = !on
        view.accessibilityElementsHidden = on
        if on { clearBand() }
    }

    /// Marks `rect` (in this view's coordinates) for a moment, then lets it go: a fade, or
    /// under Reduce Motion a plain removal.
    func showBand(_ rect: CGRect) {
        guard !rect.isNull, rect.height > 0 else { return }
        if band.superview !== view { view.addSubview(band) }
        view.bringSubviewToFront(band)
        band.layer.removeAllAnimations()
        band.frame = rect.intersection(view.bounds)
        band.alpha = 1
        let token = highlight.show()
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: JumpHighlight.hold)
            self?.endBand(token)
        }
    }

    private func endBand(_ token: Int) {
        guard highlight.end(token) else { return }
        let reduce = UIAccessibility.isReduceMotionEnabled
        let band = self.band
        if JumpHighlight.fades(reduceMotion: reduce), let animator = Motion.animator(.dim, reduce: reduce, animations: { band.alpha = 0 }) {
            animator.addCompletion { _ in if band.alpha == 0 { band.removeFromSuperview() } }
            animator.startAnimation()
        } else {
            band.removeFromSuperview()
        }
    }

    private func clearBand() {
        highlight.clear()
        band.layer.removeAllAnimations()
        band.removeFromSuperview()
    }

    func release() {
        clearBand()
        if let wanted, wanted.parent === self {
            Self.detach(wanted)
        }
        wanted = nil
        // A terminal leaving mid-hold is let go now; the next container fits it.
        heldChild?.holdFit(false)
        heldChild = nil
        heldSize = nil
    }

    override func viewIsAppearing(_ animated: Bool) {
        super.viewIsAppearing(animated)
        attach()
    }

    /// The terminal's frame is set outright on every pass, never left to an autoresizing mask
    /// that only carries changes forward from the frame it had when it arrived.
    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        guard let child = wanted, child.parent === self else { return }
        if view.bounds.size != lastBounds {
            lastBounds = view.bounds.size
            noteResize()
        }
        if holdsForTransition() {
            hold(child)
        } else if child.view.frame != view.bounds {
            child.view.frame = view.bounds
        }
    }

    /// The space SwiftUI gives the terminal changed, or may have: lay it out and fit the grid.
    /// A size that did not change since the last fit costs nothing.
    func refit(for size: CGSize) {
        if size != fitted { noteResize() }
        guard size != fitted, let child = wanted, child.parent === self, !holdsForTransition() else { return }
        fitted = size
        fits += 1
        view.setNeedsLayout()
        view.layoutIfNeeded()
        (child as? TerminalRefitting)?.refit()
        // SwiftUI may hand over the size a moment before UIKit lays the container out.
        Task { @MainActor [weak self] in
            guard let self, let child = self.wanted, child.parent === self else { return }
            self.view.layoutIfNeeded()
            (child as? TerminalRefitting)?.refit()
        }
    }

    private func attach() {
        guard let child = wanted, child.parent !== self else { return }
        if child.parent != nil {
            Self.detach(child)
        }
        addChild(child)
        fitted = nil
        // Arriving mid-animation, the terminal keeps the size it had until the movement lands.
        if Self.isAnimating(view) { noteResize() }
        let holding = holdsForTransition()
        child.view.autoresizingMask = holding ? [] : [.flexibleWidth, .flexibleHeight]
        if holding { hold(child) } else { child.view.frame = view.bounds }
        view.insertSubview(child.view, at: 0)
        child.didMove(toParent: self)
        // A hold another container put on it ends here, with a fit to this one.
        if !holding {
            fits += 1
            (child as? TerminalRefitting)?.holdFit(false)
            (child as? TerminalRefitting)?.refit()
        }
    }

    /// True while an animation moves this container: the row growing into the workspace, the
    /// app opening from its icon. Either resizes it on every frame, and every frame would be a
    /// new grid with the screen's text reflowing under the user, so the terminal keeps one size
    /// until the size stops changing and is fitted to the final space then.
    private func holdsForTransition() -> Bool {
        heldSize != nil
    }

    /// The container changed size. One inside a size or position animation, or hard on the heels
    /// of another, starts a hold.
    private func noteResize() {
        let now = ContinuousClock.now
        let rapid = lastResize.map { now - $0 < Self.quiet } ?? false
        lastResize = now
        guard heldSize == nil, let child = wanted, child.parent === self, rapid || Self.isAnimating(view) else { return }
        var size = child.view.bounds.size
        if size.width < 1 || size.height < 1 { size = view.window?.bounds.size ?? view.bounds.size }
        heldSize = size
        child.view.autoresizingMask = []
        heldChild = child as? TerminalRefitting
        heldChild?.holdFit(true)
        // Polled rather than awaited: nothing reports when another view's animation ends. A
        // movement that never ends, should one, lets go after two seconds.
        Task { @MainActor [weak self] in
            for _ in 0..<40 {
                try? await Task.sleep(for: .milliseconds(50))
                guard let self, let last = self.lastResize else { break }
                if ContinuousClock.now - last >= Self.quiet, !Self.isAnimating(self.view) { break }
            }
            self?.landed()
        }
    }

    /// How long the size must stay put before the terminal is fitted to it.
    private static let quiet: Duration = .milliseconds(120)

    private func landed() {
        heldSize = nil
        fitted = nil
        let held = heldChild
        heldChild = nil
        if let child = wanted, child.parent === self {
            child.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
            child.view.frame = view.bounds
            fits += 1
        }
        held?.holdFit(false)
    }

    /// A size or position animation runs on the view or one of its ancestors.
    private static func isAnimating(_ view: UIView) -> Bool {
        var current: UIView? = view
        while let layer = current?.layer {
            if layer.animationKeys()?.contains(where: { $0.hasPrefix("bounds") || $0.hasPrefix("position") }) == true {
                return true
            }
            current = current?.superview
        }
        return false
    }

    /// The terminal's size while a hold lasts: the one it had when the hold began, or the
    /// window's when it never had one, pinned to the container's top leading corner.
    private func hold(_ child: UIViewController) {
        guard let size = heldSize else { return }
        let frame = CGRect(origin: .zero, size: size)
        if child.view.frame != frame { child.view.frame = frame }
    }

    private static func detach(_ child: UIViewController) {
        child.willMove(toParent: nil)
        child.view.removeFromSuperview()
        child.removeFromParent()
    }
}
