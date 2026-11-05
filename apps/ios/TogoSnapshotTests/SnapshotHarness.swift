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

/// iPhone 17 in portrait. References are only valid when recorded on that simulator.
private let screen = CGSize(width: 402, height: 874)
private let safeArea = UIEdgeInsets(top: 62, left: 0, bottom: 34, right: 0)

/// Renders `view` full screen on the app background in every `ScreenVariant` and compares each
/// with its reference in `__Snapshots__`. Run `make ios-snapshots-record` to re-record.
@MainActor func assertScreen(
    _ view: some View, named name: String,
    fileID: StaticString = #fileID, file filePath: StaticString = #filePath, testName: String = #function,
    line: UInt = #line, column: UInt = #column
) {
    assertScreen(
        named: name, fileID: fileID, file: filePath, testName: testName, line: line, column: column
    ) { view }
}

/// Like `assertScreen(_:named:)`, but builds the view once per variant. Use it for views that own
/// UIKit-backed state, such as a terminal, which would otherwise keep the first variant's traits.
/// Raise the precisions for a screen whose subject covers too few pixels for the defaults to
/// notice it changing.
@MainActor func assertScreen(
    named name: String, precision: Float = 0.99, perceptualPrecision: Float = 0.98,
    fileID: StaticString = #fileID, file filePath: StaticString = #filePath, testName: String = #function,
    line: UInt = #line, column: UInt = #column,
    _ make: @MainActor () -> some View
) {
    for variant in ScreenVariant.allCases {
        let content = make()
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            .environment(\.colorScheme, variant.colorScheme)
            .environment(\.dynamicTypeSize, variant.largeText ? .accessibility5 : .large)
            .environment(\.loopsPaused, true)
        assertSnapshot(
            of: render(UIHostingController(rootView: content), in: variant),
            as: .image(precision: precision, perceptualPrecision: perceptualPrecision, scale: 3),
            named: "\(name)-\(variant.rawValue)",
            fileID: fileID, file: filePath, testName: testName, line: line, column: column
        )
    }
}

/// Draws `controller` from a real window on the app's scene. Navigation bars and toolbar items
/// are Liquid Glass, whose glyph colors come from compositing filters that only an on-screen
/// window applies; rendering the layer tree off-screen drops them and leaves the glyphs white.
/// Traits are set before the controller is attached, and showing the window runs the appearance
/// callbacks, so UIKit views see the variant's traits from their first layout.
@MainActor private func render(_ controller: UIViewController, in variant: ScreenVariant) -> UIImage {
    guard let scene = UIApplication.shared.connectedScenes.lazy.compactMap({ $0 as? UIWindowScene }).first else {
        fatalError("Snapshot tests need the Togo app as their host")
    }
    let window = UIWindow(windowScene: scene)
    window.frame = CGRect(origin: .zero, size: screen)
    window.windowLevel = .normal + 1
    window.overrideUserInterfaceStyle = variant.style
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
        window.safeAreaInsets == safeArea && window.screen.scale == 3,
        "References are recorded on an iPhone 17 simulator; this one has different insets or scale"
    )
    let format = UIGraphicsImageRendererFormat(for: window.traitCollection)
    format.scale = 3
    return UIGraphicsImageRenderer(bounds: window.bounds, format: format).image { _ in
        window.drawHierarchy(in: window.bounds, afterScreenUpdates: true)
    }
}
