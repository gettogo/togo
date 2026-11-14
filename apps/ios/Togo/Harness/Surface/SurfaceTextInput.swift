import SwiftUI
import TogoKit

struct SurfaceTextInput: View {
    @FocusState private var focused: Bool
    let node: SurfaceNode
    let context: SurfaceContext

    private var key: SurfaceDrafts.Key {
        .init(surface: context.view.id, instance: context.view.instance, node: node.id)
    }
    private var producer: String { SurfaceValues.string(node.properties["text"]) ?? "" }
    private var draft: SurfaceDrafts.Draft? { context.drafts.draft(key) }
    private var binding: Binding<String> {
        Binding(get: { draft?.text ?? producer }, set: { context.drafts.set($0, key: key, producer: producer) })
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            TextField(SurfaceValues.text(node.properties["placeholder"]), text: binding, axis: node.kind == "editor" ? .vertical : .horizontal)
                .textFieldStyle(.roundedBorder)
                .focused($focused)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .disabled(node.properties["readonly"] == .bool(true) || node.properties["typeahead"] == .bool(true))
                .accessibilityLabel(SurfaceValues.string(node.properties["aria"]) ?? SurfaceValues.text(node.properties["placeholder"]))
            if let draft {
                if draft.base != producer {
                    Text(SurfaceCopy.inputChanged).font(.footnote).foregroundStyle(.secondary)
                } else {
                    Button(SurfaceCopy.apply) {
                        var event = SurfaceValues.edit(from: draft.base, to: draft.text)
                        event.removeValue(forKey: "ev")
                        context.send(node, "edit", event, expectedText: draft.base)
                    }
                    .disabled(context.session.scene?.features.contains("edit") != true)
                }
            }
            if node.properties["sendable"] == .bool(true), context.session.scene?.features.contains("send") == true {
                Button(SurfaceCopy.send) {
                    context.send(node, "send", ["text": .string(draft?.text ?? producer)], expectedText: producer)
                }
                .disabled((draft?.text ?? producer).trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .onChange(of: context.view.document.focus, initial: true) { _, id in focused = id == node.id }
        .onChange(of: focused) { _, active in
            if active, context.view.document.focus != node.id { context.send(node, "focus") }
        }
    }
}
