import SwiftUI
import TogoKit

/// What a primitive can do besides drawing: run an action through the harness's own route,
/// open a screen, or fall back to the terminal. Primitives never own state; they show what the
/// adapter read, run the action, and redraw from the result's screen.
struct PrimitiveContext {
    /// Runs an action; nil when it failed in a way the caller already reported.
    var run: @MainActor (_ action: String, _ target: String?, _ args: [String: JSONValue]?) async -> HarnessActionResult?
    var openScreen: @MainActor (_ id: String) -> Void
    var open: @MainActor (_ target: HarnessOpen) -> Void
    var showTerminal: @MainActor () -> Void

    @MainActor static let inert = PrimitiveContext(
        run: { _, _, _ in nil }, openScreen: { _ in }, open: { _ in }, showTerminal: {}
    )
}

/// Draws a screen with the primitive it names, or the terminal hand-off for one this build does
/// not know.
struct PrimitiveScreenView: View {
    let screen: HarnessScreen
    let context: PrimitiveContext

    var body: some View {
        EmptyView()
    }
}
