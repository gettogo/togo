import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor
struct ReviewDiffSnapshotTests {
    private func loaded(_ source: DiffSource, services: FakeReviewServices = FakeReviewServices()) async -> DiffModel {
        let model = DiffModel.preview(source, services: services)
        await model.load()
        return model
    }

    private func screen(_ model: DiffModel, onOpen: ((ReviewRoute) -> Void)? = nil) -> some View {
        NavigationStack {
            DiffScreen(model: model, onOpen: onOpen)
        }
    }

    @Test func unified() async {
        let model = await loaded(.worktree(path: "src/handlers/items.ts"))
        assertScreen(named: "unified") { screen(model) }
    }

    @Test func hiddenCharacters() async {
        let model = await loaded(.worktree(path: ReviewFixtures.hiddenDiffPath))
        assertScreen(named: "hidden") { screen(model) }
    }

    @Test func truncated() async {
        let model = await loaded(.worktree(path: ReviewFixtures.bigDiffPath))
        assertScreen(named: "truncated") { screen(model) }
    }

    @Test func binary() async {
        let model = await loaded(.worktree(path: "assets/logo.png"))
        assertScreen(named: "binary") { screen(model, onOpen: { _ in }) }
    }

    @Test func empty() async {
        let model = await loaded(.worktree(path: "src/unchanged.ts"))
        assertScreen(named: "empty") { screen(model) }
    }

    @Test func error() async {
        let services = FakeReviewServices()
        services.failures[.diff] = ReviewFixtureError.offline
        let model = await loaded(.worktree(path: "src/router.ts"), services: services)
        assertScreen(named: "error") { screen(model) }
    }

    @Test func gitBusy() async {
        let services = FakeReviewServices()
        let model = await loaded(.worktree(path: "src/handlers/items.ts"), services: services)
        services.failures[.stage] = HelperError.locked
        await model.perform(.stage)
        assertScreen(named: "busy") { screen(model) }
    }

    @Test func comment() {
        let draft = DiffModel.CommentDraft(row: 4, quote: "  app.get(\"/health\", health)", location: "src/router.ts:12")
        assertScreen(named: "comment") {
            CommentSheet(draft: draft, actionTitle: DiffCopy.sendToSession, canSend: true) { _ in }
        }
        assertScreen(named: "comment-no-session") {
            CommentSheet(draft: draft, actionTitle: DiffCopy.openSessionToSend, canSend: false) { _ in }
        }
    }

    /// Side by side on an iPad Pro 13-inch in landscape, every file of a commit.
    @Test func splitOnIPad() async {
        let model = await loaded(.commit(hash: ReviewFixtures.log[0].hash, path: nil))
        for variant in ScreenVariant.allCases {
            let view = screen(model)
                .environment(\.colorScheme, variant.colorScheme)
                .environment(\.dynamicTypeSize, variant.largeText ? .accessibility5 : .large)
            assertSnapshot(
                of: renderWide(UIHostingController(rootView: view), variant: variant),
                as: .image(precision: 0.99, perceptualPrecision: 0.98, scale: 2),
                named: "split-\(variant.rawValue)"
            )
        }
    }

    /// Draws `controller` in an on-screen window the size of an iPad Pro 13-inch in landscape,
    /// with regular width, so bar glyphs composite as they do on a device.
    private func renderWide(_ controller: UIViewController, variant: ScreenVariant) -> UIImage {
        let scene = UIApplication.shared.connectedScenes.lazy.compactMap { $0 as? UIWindowScene }.first!
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 1376, height: 1032)
        window.windowLevel = .normal + 1
        window.overrideUserInterfaceStyle = variant.style
        window.traitOverrides.horizontalSizeClass = .regular
        window.traitOverrides.verticalSizeClass = .regular
        window.traitOverrides.userInterfaceIdiom = .pad
        window.traitOverrides.preferredContentSizeCategory = variant.largeText ? .accessibilityExtraExtraExtraLarge : .large
        window.rootViewController = controller
        window.isHidden = false
        defer {
            window.isHidden = true
            window.rootViewController = nil
        }
        window.layoutIfNeeded()
        RunLoop.main.run(until: .now + 0.3)
        let format = UIGraphicsImageRendererFormat(for: window.traitCollection)
        format.scale = 2
        return UIGraphicsImageRenderer(bounds: window.bounds, format: format).image { _ in
            window.drawHierarchy(in: window.bounds, afterScreenUpdates: true)
        }
    }
}
