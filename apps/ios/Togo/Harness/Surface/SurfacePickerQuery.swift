import SwiftUI
import TogoKit

/// Edits the original picker's query. Its own search grammar and row order
/// remain authoritative; closing or rebuilding the sheet retains local text.
struct SurfacePickerQuery: View {
    let node: SurfaceNode
    let context: SurfaceContext

    private var producer: String { SurfaceValues.string(node.properties["query"]) ?? "" }
    private var key: SurfaceDrafts.Key {
        .init(surface: context.view.id, instance: context.view.instance, node: node.id,
              anchor: node.id, property: "query")
    }
    private var draft: SurfaceDrafts.Draft? { context.drafts.draft(key) }
    private var canApply: Bool {
        guard context.session.canInteract, let draft else { return false }
        return draft.base == producer && draft.text != producer && draft.text.utf8.count <= 4096
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            TextField(SurfaceValues.text(node.properties["placeholder"]), text: Binding(
                get: { draft?.text ?? producer },
                set: { context.drafts.set($0, key: key, producer: producer) }
            ))
            .textFieldStyle(.roundedBorder)
            .autocorrectionDisabled()
            .textInputAutocapitalization(.never)
            .submitLabel(.search)
            .onSubmit { apply() }
            .accessibilityLabel(SurfaceCopy.search)
            if let draft {
                if draft.base != producer {
                    Text(SurfaceCopy.inputChanged).font(.footnote).foregroundStyle(.secondary)
                } else if draft.text.utf8.count > 4096 {
                    Text(SurfaceCopy.searchTooLong).font(.footnote).foregroundStyle(.secondary)
                } else if draft.text != producer {
                    Button(SurfaceCopy.search) { apply() }.disabled(!canApply)
                }
            }
        }
    }

    private func apply() {
        guard canApply, let draft else { return }
        context.send(node, "action", ["act": .string("togo.query"), "before": .string(draft.base), "query": .string(draft.text)])
    }
}
