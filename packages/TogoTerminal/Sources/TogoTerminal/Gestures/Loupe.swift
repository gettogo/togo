import UIKit

/// A 1.5x magnifier held above the finger while touch drives the mouse, so the cell under
/// the finger stays visible.
@MainActor final class Loupe: UIView {
    static let magnification: CGFloat = 1.5
    static let diameter: CGFloat = 112
    /// Far enough above the touch that a thumb does not cover it.
    static let lift: CGFloat = 84

    private weak var source: UIView?
    private var snapshot: UIView?

    init(source: UIView) {
        self.source = source
        super.init(frame: CGRect(x: 0, y: 0, width: Self.diameter, height: Self.diameter))
        isUserInteractionEnabled = false
        isAccessibilityElement = false
        clipsToBounds = true
        layer.cornerRadius = Self.diameter / 2
        layer.borderWidth = 1
        layer.borderColor = UIColor.separator.cgColor
        backgroundColor = source.backgroundColor
        alpha = 0
    }

    required init?(coder: NSCoder) {
        nil
    }

    /// `point` is the touch in the source view's coordinates.
    func show(at point: CGPoint, in host: UIView) {
        if superview == nil { host.addSubview(self) }
        move(to: point)
        UIView.animate(withDuration: 0.15) { self.alpha = 1 }
    }

    func move(to point: CGPoint) {
        guard let source, let host = superview else { return }
        let side = Self.diameter / Self.magnification
        let region = CGRect(x: point.x - side / 2, y: point.y - side / 2, width: side, height: side)
        snapshot?.removeFromSuperview()
        if let copy = source.resizableSnapshotView(from: region, afterScreenUpdates: false, withCapInsets: .zero) {
            copy.frame = bounds
            addSubview(copy)
            snapshot = copy
        }
        let touch = source.convert(point, to: host)
        let half = Self.diameter / 2
        let x = min(max(touch.x, half), host.bounds.width - half)
        // Below the finger only when there is no room above it.
        let above = touch.y - Self.lift
        let y = above - half >= 0 ? above : touch.y + Self.lift
        center = CGPoint(x: x, y: y)
    }

    func hide() {
        UIView.animate(withDuration: 0.15, animations: { self.alpha = 0 }) { _ in
            // A drag that started during the fade shows the loupe again; leave it up.
            guard self.alpha == 0 else { return }
            self.snapshot?.removeFromSuperview()
            self.snapshot = nil
            self.removeFromSuperview()
        }
    }
}
