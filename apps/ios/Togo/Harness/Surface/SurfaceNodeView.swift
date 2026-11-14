import SwiftUI
import TogoKit

struct SurfaceContext {
    let session: SurfaceSession
    let drafts: SurfaceDrafts
    let view: SurfaceView
    let generation: String

    func send(_ node: SurfaceNode, _ kind: String, _ fields: [String: JSONValue] = [:], expectedText: String? = nil) {
        let event = SurfaceValues.event(kind, fields)
        Task { @MainActor in
            guard session.generation == generation,
                  let current = session.scene?.views.first(where: { $0.id == view.id }), current.instance == view.instance,
                  current.document.root.node(id: node.id) == node else { return }
            await session.input(surface: view.id, node: node.id, event: event, expectedText: expectedText)
        }
    }
}

/// Only these implemented kinds are negotiated. Unsupported producer content
/// ends the lease visibly rather than becoming a misleading partial interface.
struct SurfaceNodeView: View {
    static let kinds: Set<String> = [
        "col", "row", "card", "section", "rule", "spacer", "text", "md", "code", "diff", "badge", "kbd",
        "icon", "spinner", "progress", "list", "item", "tabs", "editor", "input", "button", "status", "seg",
        "overlay", "toast", "picker", "prefs"
    ]
    let node: SurfaceNode
    let context: SurfaceContext

    var body: some View {
        if node.properties["hidden"] != .bool(true) {
            content
                .disabled(!context.session.canInteract || SurfaceValues.disabled(node.properties["disabled"]))
                .accessibilityIdentifier("surface.node.\(node.id)")
        }
    }

    private var properties: [String: JSONValue] { node.properties }

    @ViewBuilder private var content: some View {
        switch node.kind {
        case "col": children
        case "row", "status":
            if let click {
                Button { context.send(node, "action", ["act": .string(click)]) } label: { rowContent }
                    .buttonStyle(.plain)
            } else { rowContent }
        case "card", "section", "overlay":
            VStack(alignment: .leading, spacing: 12) {
                let head = SurfaceValues.text(properties["head"])
                if !head.isEmpty { Text(verbatim: head).font(.headline) }
                children
            }
        case "rule": Divider()
        case "spacer": Color.clear.frame(height: 8).accessibilityHidden(true)
        case "text", "seg", "badge", "md", "code", "diff", "toast": text
        case "kbd":
            // Keyboard hints stay out of a touch interface. A declared action
            // still has a labeled native button (for example the composer Send).
            if click != nil { actionButton(SurfaceValues.string(properties["aria"]) ?? SurfaceValues.string(properties["title"]) ?? SurfaceCopy.apply) }
        case "icon":
            if let label = SurfaceValues.string(properties["aria"]) {
                Text(verbatim: label).font(.caption).foregroundStyle(.secondary)
            }
        case "spinner": ProgressView(SurfaceValues.text(properties["label"]))
        case "progress":
            ProgressView(value: SurfaceValues.number(properties["value"]).map { min(1, max(0, $0)) }) {
                Text(verbatim: SurfaceValues.text(properties["label"]))
            }
        case "list": list
        case "item": Text(verbatim: SurfaceValues.text(properties["label"]))
        case "tabs": tabs
        case "button": actionButton(SurfaceValues.text(properties["text"]))
        case "input", "editor": SurfaceTextInput(node: node, context: context)
        case "picker": SurfacePickerView(node: node, context: context)
        case "prefs": SurfacePreferencesView(node: node, context: context)
        default: Text(SurfaceCopy.unsupported).foregroundStyle(.secondary)
        }
    }

    private var children: some View {
        VStack(alignment: .leading, spacing: 12) { childViews }
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var childViews: some View {
        ForEach(node.children) { child in
            AnyView(SurfaceNodeView(node: child, context: context))
        }
    }

    private var rowContent: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 8) { childViews }
            children
        }
    }

    private var click: String? {
        SurfaceValues.string(SurfaceValues.object(properties["actions"])["click"])
    }

    @ViewBuilder private var text: some View {
        let value = SurfaceValues.text(properties["text"] ?? properties["spans"])
        if let click {
            Button { context.send(node, "action", ["act": .string(click)]) } label: { Text(verbatim: value) }
        } else {
            Text(verbatim: value)
                .font(node.kind == "code" || node.kind == "diff" ? .system(.body, design: .monospaced) : .body)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        }
        childViews
    }

    private func actionButton(_ label: String) -> some View {
        Button(role: properties["variant"] == .string("danger") ? .destructive : nil) {
            context.send(node, "action", ["act": .string(click ?? "click")])
        } label: {
            Text(verbatim: label.isEmpty ? SurfaceValues.string(properties["aria"]) ?? SurfaceCopy.apply : label)
                .frame(minHeight: 44)
        }
        .disabled(properties["busy"] == .bool(true))
    }

    private var list: some View {
        VStack(alignment: .leading, spacing: 8) {
            if node.children.isEmpty { Text(verbatim: SurfaceValues.text(properties["empty"])) }
            ForEach(node.children) { child in
                Button {
                    context.send(node, "activate", ["item": .string(child.id)])
                } label: {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(verbatim: SurfaceValues.text(child.properties["label"]))
                        let detail = SurfaceValues.text(child.properties["detail"])
                        if !detail.isEmpty { Text(verbatim: detail).font(.caption).foregroundStyle(.secondary) }
                    }
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                }
                .disabled(SurfaceValues.disabled(child.properties["disabled"]))
            }
        }
    }

    private var tabs: some View {
        VStack(alignment: .leading) {
            ForEach(SurfaceValues.array(properties["items"]), id: \.self) { value in
                let item = SurfaceValues.object(value)
                if let id = SurfaceValues.string(item["id"]) {
                    Button { context.send(node, "select", ["item": .string(id)]) } label: {
                        HStack {
                            Text(verbatim: SurfaceValues.text(item["label"]))
                            if properties["active"] == .string(id) { Image(systemName: "checkmark") }
                        }.frame(minHeight: 44)
                    }
                }
            }
            childViews
        }
    }
}
