import SnapshotTesting
import SwiftUI
import Testing
import UIKit
@testable import Togo

/// The four appearances every screen is recorded in.
enum ScreenVariant: String, CaseIterable, Sendable {
    case light, dark, lightAX5 = "light-ax5", darkAX5 = "dark-ax5"

    var style: UIUserInterfaceStyle { self == .light || self == .lightAX5 ? .light : .dark }
    var colorScheme: ColorScheme { style == .light ? .light : .dark }
    var largeText: Bool { self == .lightAX5 || self == .darkAX5 }
}

/// The window a screen is drawn in. References are only valid when recorded on an iPhone 17
/// simulator; the other sizes are windows of that size with their own safe area drawn in.
enum ScreenSize: String, CaseIterable, Sendable {
    case iPhone17, duoFolded, duoOpen, duoTabletop, duoBook, iPadPro13, iPadNarrow
    /// iPad Pro 13-inch held landscape, full screen and at half the width beside another app.
    case iPadPro13Landscape, iPadHalf

    var size: CGSize {
        switch self {
        case .iPhone17: CGSize(width: 402, height: 874)
        case .duoFolded: CGSize(width: 466, height: 678)
        case .duoOpen, .duoBook: CGSize(width: 951, height: 669)
        // Tabletop holds the inner display in portrait, folded across the middle.
        case .duoTabletop: CGSize(width: 669, height: 951)
        case .iPadPro13: CGSize(width: 1032, height: 1376)
        case .iPadNarrow: CGSize(width: 375, height: 1000)
        case .iPadPro13Landscape: CGSize(width: 1376, height: 1032)
        case .iPadHalf: CGSize(width: 678, height: 1032)
        }
    }

    var safeArea: UIEdgeInsets {
        switch self {
        case .iPhone17: UIEdgeInsets(top: 62, left: 0, bottom: 34, right: 0)
        // The folded Duo keeps a system bar on the trailing edge in portrait.
        case .duoFolded: UIEdgeInsets(top: 0, left: 0, bottom: 34, right: 84)
        case .duoOpen, .duoTabletop, .duoBook, .iPadPro13, .iPadNarrow, .iPadPro13Landscape, .iPadHalf:
            UIEdgeInsets(top: 24, left: 0, bottom: 20, right: 0)
        }
    }

    var sizeClass: UserInterfaceSizeClass {
        // The widths these layouts report regular at; the split view collapses below.
        size.width >= 700 ? .regular : .compact
    }

    /// The fold this size is held with when a test passes none.
    var fold: FoldInfo {
        switch self {
        case .duoBook:
            let band = CGRect(x: 463, y: 0, width: 25, height: 669)
            return .from(active: band, inactive: band)
        case .duoTabletop:
            let band = CGRect(x: 0, y: 451, width: 669, height: 25)
            return .from(active: band, inactive: band)
        case .duoOpen:
            return .from(active: nil, inactive: CGRect(x: 463, y: 0, width: 25, height: 669))
        default:
            return .flat
        }
    }

    /// A fold given in window coordinates, as `fold` and the tests give it, moved into the safe
    /// area's coordinates, where the device reports its fold regions.
    func inSafeArea(_ fold: FoldInfo) -> FoldInfo {
        self == .iPhone17 ? fold : fold.moved(by: CGPoint(x: -safeArea.left, y: -safeArea.top))
    }

    /// Appended to reference names; empty for the iPhone so existing references keep their names.
    var suffix: String { self == .iPhone17 ? "" : "-\(rawValue)" }
}

private let deviceSafeArea = ScreenSize.iPhone17.safeArea

/// Renders `view` full screen on the app background in every `ScreenVariant` and compares each
/// with its reference in `__Snapshots__`. Run `make ios-snapshots-record` to re-record.
@MainActor func assertScreen(
    _ view: some View, named name: String, sizes: [ScreenSize] = [.iPhone17], fold: FoldInfo = .flat,
    fileID: StaticString = #fileID, file filePath: StaticString = #filePath, testName: String = #function,
    line: UInt = #line, column: UInt = #column
) {
    assertScreen(
        named: name, sizes: sizes, fold: fold,
        fileID: fileID, file: filePath, testName: testName, line: line, column: column
    ) { view }
}

/// Like `assertScreen(_:named:)`, but builds the view once per variant. Use it for views that own
/// UIKit-backed state, such as a terminal, which would otherwise keep the first variant's traits.
/// Raise the precisions for a screen whose subject covers too few pixels for the defaults to
/// notice it changing.
@MainActor func assertScreen(
    named name: String, sizes: [ScreenSize] = [.iPhone17], fold: FoldInfo = .flat,
    precision: Float = 0.99, perceptualPrecision: Float = 0.98,
    fileID: StaticString = #fileID, file filePath: StaticString = #filePath, testName: String = #function,
    line: UInt = #line, column: UInt = #column,
    _ make: @MainActor () -> some View
) {
    for size in sizes {
        for variant in ScreenVariant.allCases {
            let content = framed(make(), in: size)
                .background(Color.togoInk)
                .environment(\.colorScheme, variant.colorScheme)
                .environment(\.dynamicTypeSize, variant.largeText ? .accessibility5 : .large)
                .environment(\.loopsPaused, true)
                .environment(\.horizontalSizeClass, size.sizeClass)
                .environment(\.fold, size.inSafeArea(fold == .flat ? size.fold : fold))
            assertSnapshot(
                of: render(UIHostingController(rootView: content), in: variant, size: size),
                as: .image(precision: precision, perceptualPrecision: perceptualPrecision, scale: 3),
                named: "\(name)\(size.suffix)-\(variant.rawValue)",
                fileID: fileID, file: filePath, testName: testName, line: line, column: column
            )
        }
    }
}

/// The iPhone's own safe area applies as is; other sizes swap it for theirs.
@MainActor @ViewBuilder private func framed(_ view: some View, in size: ScreenSize) -> some View {
    if size == .iPhone17 {
        view.frame(maxWidth: .infinity, maxHeight: .infinity)
    } else {
        let inset = size.safeArea
        view
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .safeAreaPadding(EdgeInsets(top: inset.top, leading: inset.left, bottom: inset.bottom, trailing: inset.right))
            .ignoresSafeArea()
    }
}

/// Draws `controller` from a real window on the app's scene. Navigation bars and toolbar items
/// are Liquid Glass, whose glyph colors come from compositing filters that only an on-screen
/// window applies; rendering the layer tree off-screen drops them and leaves the glyphs white.
/// Traits are set before the controller is attached, and showing the window runs the appearance
/// callbacks, so UIKit views see the variant's traits from their first layout.
@MainActor private func render(_ controller: UIViewController, in variant: ScreenVariant, size: ScreenSize) -> UIImage {
    guard let scene = UIApplication.shared.connectedScenes.lazy.compactMap({ $0 as? UIWindowScene }).first else {
        fatalError("Snapshot tests need the Togo app as their host")
    }
    let window = UIWindow(windowScene: scene)
    window.frame = CGRect(origin: .zero, size: size.size)
    window.windowLevel = .normal + 1
    window.overrideUserInterfaceStyle = variant.style
    window.traitOverrides.horizontalSizeClass = size.sizeClass == .regular ? .regular : .compact
    window.traitOverrides.preferredContentSizeCategory = variant.largeText ? .accessibilityExtraExtraExtraLarge : .large
    window.rootViewController = controller
    window.isHidden = false
    defer {
        window.isHidden = true
        window.rootViewController = nil
    }
    window.layoutIfNeeded()
    // Glass loads its materials asynchronously the first time a bar appears in the process.
    RunLoop.main.run(until: .now + 0.2)
    precondition(
        (size != .iPhone17 || window.safeAreaInsets == deviceSafeArea) && window.screen.scale == 3,
        "References are recorded on an iPhone 17 simulator; this one has different insets or scale"
    )
    let format = UIGraphicsImageRendererFormat(for: window.traitCollection)
    format.scale = 3
    let renderer = UIGraphicsImageRenderer(bounds: window.bounds, format: format)
    func draw() -> UIImage {
        renderer.image { _ in window.drawHierarchy(in: window.bounds, afterScreenUpdates: true) }
    }
    // System controls fade in on their own clock, such as a search field's clear button, and a
    // busy Mac stretches every fade. A fixed wait caught them part way and the references
    // drifted, so the screen is drawn until two frames 0.1 s apart are identical.
    var image = draw()
    for _ in 0..<30 {
        RunLoop.main.run(until: .now + 0.1)
        let next = draw()
        if next.pngData() == image.pngData() { return next }
        image = next
    }
    return image
}
