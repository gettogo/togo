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

    /// The height a sheet takes over its presenter: its own fitted height over the window's
    /// root or a full-screen cover; over another sheet, the full page, never a narrow form sheet
    /// floating on a page sheet.
    static func height(_ fitted: CGFloat?, presenterIsSheet: Bool) -> CGFloat? {
        presenterIsSheet ? nil : fitted
    }

    /// `height` sizes a short sheet to its content; nil takes the full height. `name` is what
    /// the window's presentation coordinator calls it: the presentation goes through its rules
    /// (it waits while a modal is leaving; it nests over a sheet by policy, a page opened from
    /// inside returning there) and is released when it closes.
    static func present(_ controller: UIViewController, height: CGFloat?, name: String) {
        let scene = activeScene()
        let coordinator = PresentationRegistry.coordinator(for: scene)
        var request: PresentationRequest?
        if let coordinator {
            let asked = PresentationRequest(
                id: "link.\(UUID().uuidString)", name: name, kind: .sheet,
                parent: coordinator.ledger.topModal?.id, nests: true
            )
            coordinator.request(asked)
            request = asked
        }
        place(controller, height: height, request: request, coordinator: coordinator, attempt: 0)
    }

    private static func activeScene() -> UIWindowScene? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        return scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
    }

    private static func place(
        _ controller: UIViewController, height: CGFloat?, request: PresentationRequest?,
        coordinator: PresentationCoordinator?, attempt: Int
    ) {
        let scene = activeScene()
        guard let root = scene?.keyWindow?.rootViewController ?? scene?.windows.first?.rootViewController else {
            if let request { coordinator?.withdraw(request.id) }
            return
        }
        var stack = [root]
        while let next = stack.last?.presentedViewController { stack.append(next) }
        let levels = stack.map { controller in
            Level(
                moving: controller.isBeingPresented || controller.transitionCoordinator != nil,
                leaving: controller.isBeingDismissed
            )
        }
        let patience = attempt < 12
        let admitted = request.map { coordinator?.isShown($0.id) ?? true } ?? true
        let anchor = admitted || !patience ? anchor(levels, patience: patience) : .wait
        switch anchor {
        case .wait:
            Task {
                try? await Task.sleep(for: .milliseconds(250))
                place(controller, height: height, request: request, coordinator: coordinator, attempt: attempt + 1)
            }
        case .on(let depth):
            let presenter = stack[depth]
            let isSheet = depth > 0 && ![.fullScreen, .overFullScreen].contains(presenter.modalPresentationStyle)
            style(controller, height: Self.height(height, presenterIsSheet: isSheet))
            presenter.present(controller, animated: true)
            if let request, let coordinator { watch(controller, request: request, coordinator: coordinator) }
        }
    }

    /// Tells the coordinator once the controller has gone, however it closed.
    private static func watch(_ controller: UIViewController, request: PresentationRequest, coordinator: PresentationCoordinator) {
        Task { [weak controller] in
            while true {
                try? await Task.sleep(for: .milliseconds(400))
                guard let controller, controller.presentingViewController != nil else { break }
            }
            coordinator.userDismissed(request.id)
            coordinator.didLeave(request.id)
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
