import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

/// Render the shared native components with recordings from the real program.
/// These captures verify presentation only; source actions have separate tests.
@Suite(.serialized) @MainActor struct NativeCommandLayoutTests {
    @Test(arguments: ["resume", "rename", "role-strip", "login", "preferences"])
    func recordedCommandUsesNativeNavigationAndContent(_ command: String) async throws {
        let screen = try screen(command)
        let deadline = ContinuousClock.now + .seconds(3)
        while UIApplication.shared.connectedScenes.compactMap({ $0 as? UIWindowScene }).isEmpty,
              ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(20))
        }
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let previous = scene.keyWindow
        var context = PrimitiveContext.inert
        context.showsTerminal = false
        context.searchScreen = { _ in screen }
        let host = UIHostingController(rootView:
            NativeCommandNavigation(title: screen.title, close: {}, terminal: {}) {
                PrimitiveScreenView(screen: screen, context: context)
            }
                .environment(\.colorScheme, .dark)
                .environment(\.loopsPaused, true)
        )
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 430, height: 932)
        window.rootViewController = host
        window.makeKeyAndVisible()
        defer { window.isHidden = true; previous?.makeKeyAndVisible() }
        host.view.frame = window.bounds
        try await Task.sleep(for: .milliseconds(100))
        host.view.layoutIfNeeded()
        let image = UIGraphicsImageRenderer(bounds: host.view.bounds).image { _ in
            host.view.drawHierarchy(in: host.view.bounds, afterScreenUpdates: true)
        }
        #expect(image.size.width == 430)
        Attachment.record(image, named: "native-command-" + command, as: .png)
    }

    private func screen(_ command: String) throws -> HarnessScreen {
        let fixture = command == "resume" ? "omp-native-resume" : command == "preferences" ? "omp-native-preferences-pages" : "omp-native-" + command
        let url = try #require(Bundle(for: NativeCommandLayoutBundle.self).url(forResource: fixture, withExtension: "json"))
        let data = try Data(contentsOf: url)
        if command == "preferences" {
            let pages = try JSONDecoder().decode([SurfaceNode].self, from: data)
            return try NativeStructuredScreen.preferences(try #require(pages.first), title: "Settings")
        }
        let node = try JSONDecoder().decode(SurfaceNode.self, from: data)
        if command == "resume" {
            return try NativePickerScreen.map(node, workflow: "resume", fallbackTitle: "Resume", fetchedAt: WireDate(.now))
        }
        return try NativeFormScreen.map(node, title: command == "login" ? "Sign in" : "Models",
            workflow: command == "rename" ? "resume" : command == "login" ? "login" : "model", features: ["submit"])
    }
}

private final class NativeCommandLayoutBundle {}
