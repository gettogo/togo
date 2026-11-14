import SwiftUI
import TogoKit

/// Original harness controls occupy a chat row, above the one existing
/// composer. Original transcript and composer nodes are never duplicated.
struct ChatSurfaceInteractionView: View {
    let store: HarnessSessionStore
    let model: ChatSurfaceInteraction

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            if let command = model.command { Text(verbatim: command).font(.callout).textSelection(.enabled) }
            if model.session.notice != nil || model.session.phase == .unavailable {
                Text(model.session.notice == .unconfirmed ? SurfaceCopy.unconfirmed : SurfaceCopy.unavailable)
                    .font(.footnote).foregroundStyle(PrimitiveTint.warn)
                Button(SurfaceCopy.terminal) { terminal() }
            } else if model.session.scene != nil, let generation = model.session.generation {
                if model.blocks.isEmpty {
                    if model.command != nil, model.submitted, model.session.pending == nil { Text(SurfaceCopy.commandSent).foregroundStyle(.secondary) }
                    else { ProgressView(model.command == nil ? SurfaceCopy.openingForm : SurfaceCopy.openingCommand) }
                }
                ForEach(model.blocks) { block in
                    SurfaceNodeView(node: block.node, context: SurfaceContext(
                        session: model.session, drafts: store.surfaceDrafts, view: block.view, generation: generation
                    ))
                    .disabled(!block.view.listen || block.view.document.suspended)
                }
            } else { ProgressView(model.command == nil ? SurfaceCopy.openingForm : SurfaceCopy.openingCommand) }
            Button(SurfaceCopy.close) { store.retireInteraction() }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            if let scene = model.session.scene, let generation = model.session.generation {
                SurfaceDisplayProbe(stamp: .init(generation: generation, revision: scene.revision)) { stamp in
                    Task { await model.session.displayed(revision: stamp.revision, generation: stamp.generation) }
                }
            }
        }
        .accessibilityIdentifier("chat.harnessInteraction")
    }

    private func terminal() {
        store.retireInteraction()
        store.layer = .terminal
    }
}
