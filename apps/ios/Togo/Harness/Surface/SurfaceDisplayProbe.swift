import QuartzCore
import SwiftUI
import UIKit

struct SurfaceDisplayStamp: Equatable {
    var generation: String
    var revision: UInt64
}

/// Receipts follow an actual visible draw and a subsequent displayed frame.
/// Polling, insertion in the view tree and layout alone grant no credits.
struct SurfaceDisplayProbe: UIViewRepresentable {
    let stamp: SurfaceDisplayStamp
    let displayed: @MainActor (SurfaceDisplayStamp) -> Void

    func makeUIView(context: Context) -> Probe { Probe() }

    func updateUIView(_ view: Probe, context: Context) {
        view.displayed = displayed
        if view.stamp != stamp {
            view.invalidateReceipt()
            view.stamp = stamp
            view.setNeedsDisplay()
        }
    }

    static func dismantleUIView(_ view: Probe, coordinator: ()) { view.invalidateReceipt() }

    @MainActor final class Probe: UIView {
        var stamp: SurfaceDisplayStamp?
        var displayed: (@MainActor (SurfaceDisplayStamp) -> Void)?
        private var drawn: SurfaceDisplayStamp?
        private var drawnAt: CFTimeInterval = 0
        private var link: CADisplayLink?

        init() {
            super.init(frame: .zero)
            isOpaque = false
            isUserInteractionEnabled = false
            isAccessibilityElement = false
            backgroundColor = .clear
            contentMode = .redraw
        }

        required init?(coder: NSCoder) { return nil }

        override func didMoveToWindow() {
            if window == nil { invalidateReceipt() } else { setNeedsDisplay() }
        }

        override func draw(_ rect: CGRect) {
            guard visible, let stamp else { return }
            drawn = stamp
            drawnAt = CACurrentMediaTime()
            link?.invalidate()
            let link = CADisplayLink(target: self, selector: #selector(frameDisplayed(_:)))
            self.link = link
            link.add(to: .main, forMode: .common)
        }

        @objc private func frameDisplayed(_ link: CADisplayLink) {
            guard visible, drawn == stamp else { invalidateReceipt(); return }
            // timestamp refers to the last frame, not the frame about to draw.
            guard link.timestamp >= drawnAt, let drawn else { return }
            invalidateReceipt()
            displayed?(drawn)
        }

        private var visible: Bool {
            guard let window, !window.isHidden, window.windowScene?.activationState == .foregroundActive,
                  !bounds.isEmpty, convert(bounds, to: window).intersects(window.bounds) else { return false }
            var ancestor: UIView? = self
            while let view = ancestor {
                if view.isHidden || view.alpha == 0 { return false }
                ancestor = view.superview
            }
            return true
        }

        func invalidateReceipt() {
            link?.invalidate()
            link = nil
            drawn = nil
        }
    }
}
