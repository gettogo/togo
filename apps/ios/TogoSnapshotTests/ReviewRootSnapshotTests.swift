import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor
struct ReviewRootSnapshotTests {
    private static let capabilities: Set<String> = [Capability.git, Capability.gitWrite, Capability.ports]

    private var subject: ReviewSubject {
        ReviewSubject(
            hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
            managed: true, capabilities: Self.capabilities
        )
    }

    // MARK: Root

    @Test func rootOnIPhone() {
        assertScreen(named: "root-changes") {
            ReviewRoot(
                subject: subject, services: FakeReviewServices(), initialTab: .changes, machine: "Studio",
                sendToSession: { _ in }
            )
            .tint(Color.togoSnow)
        }
    }

    @Test func rootHistoryOnIPhone() {
        assertScreen(named: "root-history") {
            ReviewRoot(
                subject: subject, services: FakeReviewServices(), initialTab: .history, machine: "Studio",
                sendToSession: { _ in }
            )
            .tint(Color.togoSnow)
        }
    }

    /// iPad Pro 13-inch in landscape: the changes list in the sidebar, a diff in the detail.
    @Test func rootOnIPad() async {
        var navigation = ReviewNavigation(tab: .changes)
        navigation.open(.diff(.worktree(path: "src/handlers/items.ts")), layout: .split)
        for variant in ScreenVariant.allCases {
            let view = ReviewRoot(
                subject: subject, services: FakeReviewServices(), initialTab: .changes, machine: "Studio",
                sendToSession: { _ in }, navigation: navigation
            )
            .tint(Color.togoSnow)
            .environment(\.colorScheme, variant.colorScheme)
            .environment(\.dynamicTypeSize, variant.largeText ? .accessibility5 : .large)
            .environment(\.loopsPaused, true)
            let image = await renderIPad(UIHostingController(rootView: view), variant: variant)
            assertSnapshot(
                of: image,
                as: .image(precision: 0.99, perceptualPrecision: 0.98, scale: 2),
                named: "root-ipad-\(variant.rawValue)"
            )
        }
    }

    /// An on-screen window the size of an iPad Pro 13-inch in landscape, with regular width, so
    /// the split view lays out and bar glyphs composite as they do on a device.
    private func renderIPad(_ controller: UIViewController, variant: ScreenVariant) async -> UIImage {
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
        // The diff is read and coloured off the main thread; sleeping lets its result reach the
        // main actor, which a nested run loop would hold up.
        try? await Task.sleep(for: .seconds(1))
        window.layoutIfNeeded()
        let format = UIGraphicsImageRendererFormat(for: window.traitCollection)
        format.scale = 2
        return UIGraphicsImageRenderer(bounds: window.bounds, format: format).image { _ in
            window.drawHierarchy(in: window.bounds, afterScreenUpdates: true)
        }
    }
}
