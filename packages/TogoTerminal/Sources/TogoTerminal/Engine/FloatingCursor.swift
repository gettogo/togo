internal import SwiftTerm
import ObjectiveC
import UIKit

/// Turns the keyboard trackpad's vertical travel (hold the space bar, or two fingers on an iPad
/// keyboard) into Up and Down presses: one per terminal row of travel, carrying the remainder,
/// so a slow drag and a fast one move the same distance.
struct FloatingCursorRows {
    private(set) var anchorY: CGFloat?
    /// Travel since the last step that has not made a whole row yet.
    private var pending: CGFloat = 0
    private var lastY: CGFloat = 0

    /// Rows shorter than this still need this much travel, so a tiny font is not twitchy.
    static let minimumStep: CGFloat = 12

    mutating func begin(at point: CGPoint) {
        anchorY = point.y
        lastY = point.y
        pending = 0
    }

    /// Rows to move: negative is up, positive is down.
    mutating func move(to point: CGPoint, rowHeight: CGFloat) -> Int {
        guard anchorY != nil else { return 0 }
        let step = max(rowHeight, Self.minimumStep)
        pending += point.y - lastY
        lastY = point.y
        let rows = Int(pending / step)
        pending -= CGFloat(rows) * step
        return rows
    }

    mutating func end() {
        anchorY = nil
        pending = 0
    }
}

extension TogoTerminalView {
    private typealias PointIMP = @convention(c) (AnyObject, Selector, CGPoint) -> Void
    private typealias PlainIMP = @convention(c) (AnyObject, Selector) -> Void

    /// SwiftTerm's floating cursor methods are public, not open, so Togo's versions are added to
    /// this class at run time. Each still calls SwiftTerm's, which moves left and right and
    /// keeps its input buffer in step; it is handed the starting height, so the vertical travel
    /// is Togo's alone. Installed once; a SwiftTerm without these methods is left as it is.
    static let installFloatingCursor: Void = {
        let s = (
            begin: NSSelectorFromString("beginFloatingCursorAtPoint:"),
            update: NSSelectorFromString("updateFloatingCursorAtPoint:"),
            end: NSSelectorFromString("endFloatingCursor")
        )
        let target: AnyClass = TogoTerminalView.self
        guard let beginMethod = class_getInstanceMethod(TerminalView.self, s.begin),
              let updateMethod = class_getInstanceMethod(TerminalView.self, s.update),
              let endMethod = class_getInstanceMethod(TerminalView.self, s.end) else { return }
        let superBegin = unsafeBitCast(method_getImplementation(beginMethod), to: PointIMP.self)
        let superUpdate = unsafeBitCast(method_getImplementation(updateMethod), to: PointIMP.self)
        let superEnd = unsafeBitCast(method_getImplementation(endMethod), to: PlainIMP.self)

        // UIKit calls these on the main thread, as it does every text input method.
        let begin: @convention(block) (AnyObject, CGPoint) -> Void = { object, point in
            superBegin(object, s.begin, point)
            let view = object as? TogoTerminalView
            MainActor.assumeIsolated { view?.floatingCursorBegan(at: point) }
        }
        let update: @convention(block) (AnyObject, CGPoint) -> Void = { object, point in
            let view = object as? TogoTerminalView
            let anchor = MainActor.assumeIsolated { view?.floatingRows.anchorY }
            superUpdate(object, s.update, CGPoint(x: point.x, y: anchor ?? point.y))
            MainActor.assumeIsolated { view?.floatingCursorMoved(to: point) }
        }
        let end: @convention(block) (AnyObject) -> Void = { object in
            superEnd(object, s.end)
            let view = object as? TogoTerminalView
            MainActor.assumeIsolated { view?.floatingCursorEnded() }
        }
        class_replaceMethod(target, s.begin, imp_implementationWithBlock(begin), method_getTypeEncoding(beginMethod))
        class_replaceMethod(target, s.update, imp_implementationWithBlock(update), method_getTypeEncoding(updateMethod))
        class_replaceMethod(target, s.end, imp_implementationWithBlock(end), method_getTypeEncoding(endMethod))
    }()

    func floatingCursorBegan(at point: CGPoint) {
        floatingRows.begin(at: point)
        floatingPin = FloatingPin(offset: contentOffset, atBottom: contentOffset.y >= maximumOffsetY - 1)
    }

    func floatingCursorMoved(to point: CGPoint) {
        let rows = floatingRows.move(to: point, rowHeight: floatingRowHeight?() ?? 0)
        if rows != 0 { onFloatingRows?(rows) }
    }

    func floatingCursorEnded() {
        floatingRows.end()
        floatingPin = nil
    }

    var maximumOffsetY: CGFloat {
        max(0, contentSize.height - bounds.height + adjustedContentInset.bottom)
    }

    /// While the trackpad is active the scrollback holds still: UIKit scrolls toward the caret
    /// otherwise. A view at the bottom keeps following new output.
    func pinnedOffset(for proposed: CGPoint) -> CGPoint {
        guard let pin = floatingPin else { return proposed }
        if pin.atBottom, proposed.y >= contentOffset.y { return proposed }
        return CGPoint(x: proposed.x, y: pin.atBottom ? contentOffset.y : pin.offset.y)
    }
}

struct FloatingPin {
    var offset: CGPoint
    var atBottom: Bool
}
