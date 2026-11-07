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
    /// Reads a screen again, for a Live list that refreshes while it shows. nil where the host
    /// cannot read screens, such as a preview of a mission control section.
    var reload: (@MainActor () async -> Void)? = nil
    /// The agent is in a turn: a change that restarts it asks first (`ActionGate`).
    var working: @MainActor () -> Bool = { false }
    /// The harness's name, for "Restart Claude now?".
    var agent = ""
    /// The composer line "With instructions…" leaves for an action id; nil when it takes none.
    var instructionsLine: @MainActor (String) -> String? = { _ in nil }
    /// Puts a line in the composer, focused, leaving this screen. nil where there is no composer.
    var compose: (@MainActor (String) -> Void)? = nil
    /// Holds an action for the end of the turn ("After this reply"); `note` is the chat's line
    /// once it ran.
    var runAfterTurn: (@MainActor (_ action: String, _ target: String?, _ args: [String: JSONValue]?, _ note: String) -> Void)? = nil
    /// A pick or a setting landed: the host closes (a sheet, the palette). nil where the screen
    /// stays, as in mission control's pushes.
    var settled: (@MainActor () -> Void)? = nil
    /// Leaves a line in the chat about a change, with Undo when one is offered.
    var note: (@MainActor (_ text: String, _ undo: SessionUndo?) -> Void)? = nil

    @MainActor static let inert = PrimitiveContext(
        run: { _, _, _ in nil }, openScreen: { _ in }, open: { _ in }, showTerminal: {}
    )
}

extension PrimitiveContext {
    /// A context for a screen a session store can read and act on. Errors become a failed
    /// result, so the primitive shows its failure line; `fallback` refusals open the terminal.
    @MainActor static func store(
        _ store: HarnessSessionStore, openScreen: @escaping @MainActor (String) -> Void,
        open: @escaping @MainActor (HarnessOpen) -> Void, showTerminal: @escaping @MainActor () -> Void
    ) -> PrimitiveContext {
        var context = PrimitiveContext(
            run: { action, target, args in
                do {
                    return try await store.run(action: action, target: target, args: args)
                } catch is CancellationError {
                    return nil
                } catch let error as HelperError where error.isFallback {
                    return HarnessActionResult(result: .fallback)
                } catch {
                    return HarnessActionResult(result: .failed, message: PrimitiveCopy.noAnswer)
                }
            },
            openScreen: openScreen, open: open, showTerminal: showTerminal
        )
        context.working = { store.working }
        context.agent = store.descriptor?.adapter?.name ?? ""
        context.instructionsLine = { store.instructionsLine(for: $0) }
        context.compose = { store.compose($0) }
        context.runAfterTurn = { action, target, args, note in
            store.runAfterTurn(action: action, target: target, args: args, note: note)
        }
        context.note = { text, undo in store.note(text, code: "model", undo: undo) }
        return context
    }
}

extension HelperError {
    /// The helper refused because no native route serves this: open the terminal instead.
    var isFallback: Bool {
        let reasons: Set<String> = ["fallback", "no_adapter"]
        switch self {
        case .refused(let reason):
            return reasons.contains(reason)
        case .rpc(let object):
            guard object.code == RPCErrorCode.refused,
                  case let .object(data)? = object.data, case let .string(reason)? = data["reason"] else { return false }
            return reasons.contains(reason)
        default:
            return false
        }
    }
}
