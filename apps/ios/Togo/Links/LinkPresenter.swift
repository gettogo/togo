import UIKit

/// Presents over whatever the active window shows, sheets included.
///
/// A sheet presented while another is still coming up or going away is refused or swept away
/// with it, so a presentation waits until the stack is still, then goes on top of it.
@MainActor enum LinkPresenter {
    /// One controller in the window's stack of presentations, as far as presenting goes.
    struct Level: Equatable {
        var moving = false
        var leaving = false
    }

    enum Anchor: Equatable {
        /// Present on the controller at this depth (0 is the window's root).
        case on(Int)
        /// Something is still moving: ask again shortly.
        case wait
    }

    /// Where to present over `stack` (root first): on the top controller that is not going
    /// away, once nothing is moving. After `patience` runs out it presents anyway, on the top
    /// controller that is staying.
    static func anchor(_ stack: [Level], patience: Bool = true) -> Anchor {
        guard !stack.isEmpty else { return .wait }
        if patience, stack.contains(where: { $0.moving || $0.leaving }) { return .wait }
        let staying = stack.firstIndex(where: \.leaving).map { max($0 - 1, 0) } ?? stack.count - 1
        return .on(staying)
    }

    /// `height` sizes a short sheet to its content; nil takes the full height.
    static func present(_ controller: UIViewController, height: CGFloat?, attempt: Int = 0) {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let scene = scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
        guard let root = scene?.keyWindow?.rootViewController ?? scene?.windows.first?.rootViewController else { return }
        var stack = [root]
        while let next = stack.last?.presentedViewController { stack.append(next) }
        let levels = stack.map { controller in
            Level(
                moving: controller.isBeingPresented || controller.transitionCoordinator != nil,
                leaving: controller.isBeingDismissed
            )
        }
        switch anchor(levels, patience: attempt < 12) {
        case .wait:
            Task {
                try? await Task.sleep(for: .milliseconds(250))
                present(controller, height: height, attempt: attempt + 1)
            }
        case .on(let depth):
            style(controller, height: height)
            stack[depth].present(controller, animated: true)
        }
    }

    private static func style(_ controller: UIViewController, height: CGFloat?) {
        if let height {
            controller.modalPresentationStyle = .formSheet
            if let sheet = controller.sheetPresentationController {
                sheet.detents = [.custom { context in min(max(height, 200), context.maximumDetentValue) }, .large()]
                sheet.prefersGrabberVisible = true
            }
        } else {
            controller.modalPresentationStyle = .pageSheet
            controller.sheetPresentationController?.detents = [.large()]
        }
    }
}
