import CoreGraphics
import SwiftUI
import Testing
import UIKit
@testable import Togo

/// The workspace's motion rules: the terminal's grid is fitted once per arrangement change,
/// never per frame, and the compose bar animates only when its layout changes.
@MainActor struct WorkspaceMotionTests {
    // MARK: Pane settles

    private static let iPad = CGSize(width: 1032, height: 1376)
    private static let book = FoldInfo.from(active: CGRect(x: 463, y: 0, width: 25, height: 669), inactive: nil)

    private func key(size: CGSize, fold: FoldInfo = .flat, secondary: Bool = false, fraction: CGFloat = 0.5) -> PaneSettle.Key {
        PaneSettle.Key(PanesReport(
            size: size, sizeClass: .regular, fold: fold, hasSecondary: secondary, primaryFraction: fraction
        ))
    }

    @Test func aSettleBeginsOncePerArrangementChange() {
        var settle = PaneSettle()
        let single = key(size: Self.iPad)
        let split = key(size: Self.iPad, secondary: true)
        #expect(settle.begin(from: single, to: split))
        #expect(settle.generation == 1)
        // The same arrangement measured again is not a change.
        #expect(!settle.begin(from: split, to: split))
        #expect(settle.generation == 1)
    }

    @Test func theFirstLayoutLandsWithoutACover() {
        var settle = PaneSettle()
        #expect(!settle.begin(from: key(size: .zero), to: key(size: Self.iPad)))
        #expect(settle.generation == 0)
    }

    @Test func aFoldTheSidebarAndADividerEachSettle() {
        var settle = PaneSettle()
        let flat = key(size: CGSize(width: 951, height: 669))
        #expect(settle.begin(from: flat, to: key(size: CGSize(width: 951, height: 669), fold: Self.book)))
        // The sidebar narrows the panes.
        #expect(settle.begin(from: key(size: Self.iPad), to: key(size: CGSize(width: 712, height: 1376))))
        // A divider released at a new split.
        #expect(settle.begin(from: key(size: Self.iPad, secondary: true), to: key(size: Self.iPad, secondary: true, fraction: 0.6)))
        #expect(settle.generation == 3)
    }

    // MARK: The terminal's grid

    @Test func aTerminalIsFittedOncePerSizeChange() async throws {
        let container = TerminalContainer()
        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 800, height: 600))
        window.rootViewController = container
        window.makeKeyAndVisible()
        defer { window.isHidden = true }
        let terminal = FittingTerminal()
        container.show(terminal)
        container.refit(for: CGSize(width: 800, height: 600))
        // Let the first fit's quiet period pass, so the next change is a single one.
        try await Task.sleep(for: .milliseconds(300))
        let before = container.fits

        container.view.frame = CGRect(x: 0, y: 0, width: 600, height: 600)
        container.view.layoutIfNeeded()
        container.refit(for: CGSize(width: 600, height: 600))
        // SwiftUI hands the same size over again on later passes; none of them fits again.
        container.refit(for: CGSize(width: 600, height: 600))
        container.refit(for: CGSize(width: 600, height: 600))
        try await Task.sleep(for: .milliseconds(300))

        #expect(container.fits - before == 1)
    }

    // MARK: Compose bar

    @Test func theComposeBarAnimatesOnlyWhenItsRowsChange() {
        let heard = ComposeShape(dictation: .listening(partial: "run the", level: 0.2))
        let more = ComposeShape(dictation: .listening(partial: "run the tests", level: 0.6))
        #expect(heard.stage == more.stage)
        #expect(heard.stage != ComposeShape().stage)
        #expect(ComposeShape(image: .uploading).stage != ComposeShape(image: .tooLarge).stage)
        #expect(ComposeShape(image: .annotating(PendingImage.sample)).stage == ComposeShape().stage)
    }
}

/// A terminal the container fits; the container counts the fits.
private final class FittingTerminal: UIViewController, TerminalRefitting {
    func refit() {}
    func holdFit(_ on: Bool) {}
}

private extension PendingImage {
    static var sample: PendingImage {
        PendingImage(preview: UIImage(), original: Data())
    }
}
