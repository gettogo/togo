import SwiftUI
import TogoKit

struct SurfacePickerEntry: Identifiable {
    var id: String
    var properties: [String: JSONValue]
}

enum SurfacePickerContent {
    static func entries(_ properties: [String: JSONValue]) -> [SurfacePickerEntry] {
        let items = SurfaceValues.array(properties["items"]).compactMap { value -> SurfacePickerEntry? in
            let item = SurfaceValues.object(value)
            guard let id = SurfaceValues.string(item["id"]) else { return nil }
            return SurfacePickerEntry(id: id, properties: item)
        }
        // The producer's order is authoritative; group headings occupy the
        // spaces between items rather than being selectable rows.
        guard case .array(let order)? = properties["order"] else { return items }
        let byID = Dictionary(items.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return order.compactMap { value in
            if let id = SurfaceValues.string(value) { return byID[id] }
            let group = SurfaceValues.object(value)
            guard let id = SurfaceValues.string(group["group"]) else { return nil }
            return SurfacePickerEntry(id: "group:\(id)", properties: group)
        }
    }
}

struct SurfacePickerView: View {
    let node: SurfaceNode
    let context: SurfaceContext
    private var properties: [String: JSONValue] { node.properties }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            let title = SurfaceValues.text(properties["title"])
            if !title.isEmpty { Text(verbatim: title).font(.title2).bold() }
            let subtitle = SurfaceValues.text(properties["subtitle"])
            if !subtitle.isEmpty { Text(verbatim: subtitle).foregroundStyle(.secondary) }
            regions("scopes", action: "scope")
            regions("tabs", action: "tab")
            if let query = SurfaceValues.string(properties["query"]), !query.isEmpty {
                Text(verbatim: query).font(.subheadline)
            }
            if properties["state"] == .string("loading") { ProgressView(SurfaceCopy.waiting) }
            let message = SurfaceValues.text(properties["message"])
            if !message.isEmpty { Text(verbatim: message).foregroundStyle(.secondary) }
            let entries = SurfacePickerContent.entries(properties)
            if entries.isEmpty { Text(verbatim: SurfaceValues.text(properties["empty"])) }
            ForEach(entries) { item in
                if item.properties["group"] != nil {
                    Text(verbatim: SurfaceValues.text(item.properties["label"])).font(.headline)
                } else {
                    row(item)
                }
            }
            let confirm = SurfaceValues.object(properties["confirm"])
            if let action = SurfaceValues.string(confirm["act"]) {
                Text(verbatim: SurfaceValues.text(confirm["text"]))
                Button(role: .destructive) {
                    context.send(node, "action", ["act": .string(action)])
                } label: { Text(verbatim: SurfaceValues.string(confirm["label"]) ?? SurfaceCopy.apply) }
            }
            ForEach(SurfaceValues.array(properties["actions"]), id: \.self) { value in
                let action = SurfaceValues.object(value)
                if let id = SurfaceValues.string(action["id"]) {
                    Button(role: action["danger"] == .bool(true) ? .destructive : nil) {
                        context.send(node, "action", ["act": .string(id)])
                    } label: {
                        Text(verbatim: SurfaceValues.text(action["label"]))
                            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    }
                    .disabled(SurfaceValues.disabled(action["disabled"]))
                }
            }
            ForEach(node.children) { child in AnyView(SurfaceNodeView(node: child, context: context)) }
        }
    }

    private func row(_ item: SurfacePickerEntry) -> some View {
        Button {
            context.send(node, "select", ["item": .string(item.id)])
        } label: {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: SurfaceValues.text(item.properties["label"]))
                    let detail = SurfaceValues.text(item.properties["detail"])
                    if !detail.isEmpty { Text(verbatim: detail).font(.caption).foregroundStyle(.secondary) }
                    ForEach(SurfaceValues.array(item.properties["badges"]), id: \.self) { badge in
                        Text(verbatim: SurfaceValues.text(SurfaceValues.object(badge)["text"])).font(.caption)
                    }
                }
                Spacer(minLength: 8)
                if properties["selected"] == .string(item.id) { Image(systemName: "checkmark") }
            }
            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
            .contentShape(.rect)
        }
        .disabled(SurfaceValues.disabled(item.properties["disabled"]))
        .accessibilityAddTraits(properties["selected"] == .string(item.id) ? .isSelected : [])
    }

    @ViewBuilder private func regions(_ key: String, action: String) -> some View {
        let items = SurfaceValues.array(properties[key])
        if !items.isEmpty {
            Menu {
                ForEach(items, id: \.self) { value in
                    let item = SurfaceValues.object(value)
                    if let id = SurfaceValues.string(item["id"]) {
                        Button {
                            context.send(node, "action", ["act": .string(action), "value": .string(id)])
                        } label: { Text(verbatim: SurfaceValues.text(item["label"])) }
                        .disabled(SurfaceValues.disabled(item["disabled"]))
                    }
                }
            } label: {
                let selected = SurfaceValues.string(properties[action])
                let item = items.map(SurfaceValues.object).first { SurfaceValues.string($0["id"]) == selected }
                Text(verbatim: SurfaceValues.text(item?["label"]))
            }
        }
    }
}
