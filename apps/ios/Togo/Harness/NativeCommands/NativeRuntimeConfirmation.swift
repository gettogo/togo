import SwiftUI
import TogoKit

/// The program's confirmation becomes the app's existing system alert.
/// The row action itself does not ask a second, unrelated question.
struct NativeRuntimeConfirmation: ViewModifier {
    let store: HarnessSessionStore
    let model: ChatSurfaceInteraction
    @State private var request: ActionGateRequest?
    @State private var handled: JSONValue?

    func body(content: Content) -> some View {
        content
            .actionConfirmation($request)
            .onChange(of: model.session.scene?.revision, initial: true) { update() }
    }

    private func update() {
        guard let block = model.blocks.last,
              case .object(let confirm)? = block.node.properties["confirm"],
              let action = SurfaceValues.string(confirm["act"]), let text = NativeStructuredScreen.text(confirm["text"]) else {
            handled = nil
            return
        }
        let payload = JSONValue.object(confirm)
        guard payload != handled else { return }
        handled = payload
        let context = NativeCommandContext(model: model, block: block, store: store)
        let label = NativeStructuredScreen.text(confirm["label"]) ?? PrimitiveCopy.confirm
        request = ActionGateRequest(dialog: .init(title: text, confirmLabel: label,
            destructive: model.workflow == "resume" && action == "delete-confirm", offersAfterReply: false, instructions: nil), run: {
                await send(context, node: block.node.id, action: action)
            }, cancel: {
                Task { await send(context, node: block.node.id, action: "cancel") }
            })
    }

    private func send(_ context: NativeCommandContext, node: String, action: String) async {
        do { try await context.event(node: node, kind: "action", fields: ["act": .string(action)]) }
        catch { if store.interaction === model { store.note(SurfaceCopy.unconfirmed, code: "command") } }
    }
}
