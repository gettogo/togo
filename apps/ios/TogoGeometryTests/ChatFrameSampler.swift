import QuartzCore
import Testing
import UIKit
@testable import Togo

/// Samples the mounted chat on display-link ticks. Presentation positions include in-flight
/// scrolling and layout animations. A sample is geometry evidence, not a compositor capture.
@MainActor final class ChatFrameSampler: NSObject {
    struct Sample {
        var time: CFTimeInterval
        var epoch: String?
        var pinned: Bool
        var dragging: Bool
        var viewport: CGRect
        var composer: CGRect?
        var distanceFromFoot: CGFloat
        var visible: [ChatFramePart: CGRect]

        var hasRows: Bool {
            visible.keys.contains { if case .row = $0 { return true }; return false }
        }

        func offset(of part: ChatFramePart) -> CGFloat? {
            visible[part].map { $0.minY - viewport.minY }
        }
    }

    private weak var view: UIView?
    private var link: CADisplayLink?
    private let store: HarnessSessionStore
    private(set) var samples: [Sample] = []
    private var event: CFTimeInterval?

    init(view: UIView, store: HarnessSessionStore) {
        self.view = view
        self.store = store
    }

    func start() {
        guard link == nil else { return }
        let link = CADisplayLink(target: self, selector: #selector(sample(_:)))
        self.link = link
        link.add(to: .main, forMode: .common)
    }

    func stop() {
        link?.invalidate()
        link = nil
    }

    func mark() -> CFTimeInterval {
        let time = CACurrentMediaTime()
        event = time
        return time
    }

    func attachTrace() {
        let origin = event ?? samples.first?.time ?? 0
        let frames = samples.map { TraceFrame($0, since: origin) }
        if let data = try? JSONEncoder().encode(frames) {
            Attachment.record(Array(data), named: "chat-frames.json")
        }
    }

    func moveIntoHistory(by distance: CGFloat) -> Bool {
        guard let view, let scroll = chatScroll(in: descendants(view)) else { return false }
        let y = max(-scroll.adjustedContentInset.top, scroll.contentOffset.y - distance)
        scroll.setContentOffset(CGPoint(x: scroll.contentOffset.x, y: y), animated: false)
        return true
    }

    @objc private func sample(_ link: CADisplayLink) {
        guard let view, let window = view.window else { return }
        let tree = descendants(view)
        var viewport = CGRect.null
        var foot = CGFloat.infinity
        if let scroll = chatScroll(in: tree) {
            let bounds = scroll.layer.presentation()?.bounds ?? scroll.bounds
            viewport = frame(scroll, bounds: bounds.inset(by: scroll.adjustedContentInset), in: window)
            foot = max(0, scroll.contentSize.height - bounds.maxY + scroll.adjustedContentInset.bottom)
        }
        let viewportInWindow = viewport.intersection(window.bounds)
        var parts: [ChatFramePart: CGRect] = [:]
        var composer: CGRect?
        for marker in tree.compactMap({ $0 as? ChatFrameGuideView }) {
            guard let part = marker.part, isShown(marker) else { continue }
            let rect = frame(marker, bounds: marker.bounds, in: window)
            if part == .composer {
                composer = rect
            } else if rect.intersects(viewportInWindow) {
                // Lazy rows can coexist during an animation. Keep the visible instance.
                if let previous = parts[part], visibleHeight(previous, in: viewportInWindow) >=
                    visibleHeight(rect, in: viewportInWindow) { continue }
                parts[part] = rect
            }
        }
        samples.append(Sample(time: link.timestamp, epoch: store.timeline.epoch, pinned: store.chatScroll.pinned,
                              dragging: store.chatScroll.dragging, viewport: viewportInWindow, composer: composer,
                              distanceFromFoot: foot, visible: parts))
        if samples.count > 480 { samples.removeFirst(samples.count - 480) }
    }

    private func frame(_ view: UIView, bounds: CGRect, in window: UIWindow) -> CGRect {
        if let source = view.layer.presentation(), let destination = window.layer.presentation() {
            return source.convert(bounds, to: destination)
        }
        return view.convert(bounds, to: window)
    }

    private func isShown(_ view: UIView) -> Bool {
        var ancestor: UIView? = view
        while let current = ancestor {
            if current.isHidden || (current.layer.presentation()?.opacity ?? current.layer.opacity) < 0.01 { return false }
            ancestor = current.superview
        }
        return true
    }

    private func visibleHeight(_ rect: CGRect, in viewport: CGRect) -> CGFloat {
        rect.intersection(viewport).height
    }

    private func descendants(_ view: UIView) -> [UIView] {
        [view] + view.subviews.flatMap(descendants)
    }

    private func chatScroll(in tree: [UIView]) -> UIScrollView? {
        tree.compactMap { $0 as? UIScrollView }.first { !($0 is UITextView) && $0.bounds.height > 100 }
    }

    private struct TraceFrame: Encodable {
        let timeFromEvent: Double
        let epoch: String?
        let pinned: Bool
        let dragging: Bool
        let viewport: CGRect?
        let composer: CGRect?
        let distanceFromFoot: CGFloat?
        let visible: [String: CGRect]

        init(_ sample: Sample, since origin: CFTimeInterval) {
            timeFromEvent = sample.time - origin
            epoch = sample.epoch
            pinned = sample.pinned
            dragging = sample.dragging
            viewport = sample.viewport.isNull ? nil : sample.viewport
            composer = sample.composer
            distanceFromFoot = sample.distanceFromFoot.isFinite ? sample.distanceFromFoot : nil
            visible = sample.visible.reduce(into: [:]) { $0[String(describing: $1.key)] = $1.value }
        }
    }
}
