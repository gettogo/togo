#if DEBUG
import OSLog
import UIKit
import UIKit.UIGestureRecognizerSubclass

/// Observe real window events over the key overlays without changing gesture arbitration.
/// Hit testing alone can report a button even when another recognizer consumes its touch.
@MainActor final class TerminalKeyTouchTrace: UIGestureRecognizer, UIGestureRecognizerDelegate {
    private static let logger = Logger(subsystem: "app.togo", category: "TerminalKeyTouches")
    private static var lastKeyTouch: TimeInterval = 0
    private weak var row: UIView?
    private weak var column: UIView?

    init(row: UIView, column: UIView) {
        self.row = row
        self.column = column
        super.init(target: nil, action: nil)
        delegate = self
        cancelsTouchesInView = false
        delaysTouchesBegan = false
        delaysTouchesEnded = false
        name = "TerminalKeyTouchTrace"
    }

    func gestureRecognizer(_ gestureRecognizer: UIGestureRecognizer, shouldReceive touch: UITouch) -> Bool {
        [row, column].contains { surface in
            guard let surface, !surface.isHidden, surface.window === touch.window else { return false }
            return surface.bounds.contains(touch.location(in: surface))
        }
    }

    override func canPrevent(_ preventedGestureRecognizer: UIGestureRecognizer) -> Bool { false }
    override func canBePrevented(by preventingGestureRecognizer: UIGestureRecognizer) -> Bool { false }

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent) {
        Self.lastKeyTouch = ProcessInfo.processInfo.systemUptime
        record("began", touches)
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent) {
        record("ended", touches)
        state = .failed
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent) {
        record("cancelled", touches)
        state = .failed
    }

    private func record(_ phase: String, _ touches: Set<UITouch>) {
        for touch in touches {
            let target = touch.view.map { String(describing: type(of: $0)) } ?? "nil"
            var ancestors: [String] = []
            var current = touch.view
            while let view = current {
                let gestures = (view.gestureRecognizers ?? []).filter { $0 !== self }.map {
                    String(describing: $0)
                }.joined(separator: " | ")
                ancestors.append("\(type(of: view)): \(gestures)")
                current = view.superview
            }
            let detail = ancestors.joined(separator: "\n")
            Self.logger.notice("key touch \(phase, privacy: .public), target=\(target, privacy: .public)\n\(detail, privacy: .public)")
        }
    }

    static func action(_ key: KeyRef) {
        lastKeyTouch = ProcessInfo.processInfo.systemUptime
        logger.notice("key action \(key.title, privacy: .public)")
    }

    static func focusAttempt(dismissed: Bool) {
        guard ProcessInfo.processInfo.systemUptime - lastKeyTouch < 1 else { return }
        let stack = Thread.callStackSymbols.prefix(16).joined(separator: "\n")
        logger.notice("focus during key touch, dismissed=\(dismissed, privacy: .public)\n\(stack, privacy: .public)")
    }
}
#endif
