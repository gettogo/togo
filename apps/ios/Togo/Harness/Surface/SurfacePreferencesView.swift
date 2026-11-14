import SwiftUI
import TogoKit

struct SurfacePreferencesView: View {
    let node: SurfaceNode
    let context: SurfaceContext

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(verbatim: SurfaceValues.text(node.properties["title"])).font(.title2).bold()
            Menu {
                ForEach(SurfaceValues.array(node.properties["pages"]), id: \.self) { value in
                    let page = SurfaceValues.object(value)
                    if let id = SurfaceValues.string(page["id"]) {
                        Button { context.send(node, "action", ["act": .string("page"), "value": .string(id)]) } label: {
                            Text(verbatim: SurfaceValues.text(page["label"]))
                        }
                        .disabled(SurfaceValues.disabled(page["disabled"]))
                    }
                }
            } label: {
                let page = SurfaceValues.array(node.properties["pages"]).map(SurfaceValues.object)
                    .first { $0["id"] == node.properties["page"] }
                Text(verbatim: SurfaceValues.text(page?["label"]))
            }
            let lead = SurfaceValues.text(node.properties["lead"])
            if !lead.isEmpty { Text(verbatim: lead).foregroundStyle(.secondary) }
            ForEach(SurfaceValues.entries(node.properties["sections"])) { entry in
                let section = entry.properties
                Text(verbatim: SurfaceValues.text(section["title"])).font(.headline)
                ForEach(SurfaceValues.entries(section["rows"])) { entry in
                    SurfacePreferenceRow(node: node, row: entry.properties, id: entry.id, context: context)
                        .disabled(SurfaceValues.disabled(entry.properties["disabled"]))
                }
            }
            ForEach(node.children) { child in AnyView(SurfaceNodeView(node: child, context: context)) }
        }
    }
}

private struct SurfacePreferenceRow: View {
    let node: SurfaceNode
    let row: [String: JSONValue]
    let id: String
    let context: SurfaceContext
    private var control: [String: JSONValue] { SurfaceValues.object(row["control"]) }
    private var label: String { SurfaceValues.text(row["label"]) }
    private var key: SurfaceDrafts.Key {
        .init(surface: context.view.id, instance: context.view.instance, node: node.id, field: id, anchor: node.id)
    }
    private var producer: String {
        SurfaceValues.string(control["value"]) ?? SurfaceValues.number(control["value"]).map { String($0) } ?? ""
    }
    private var draft: SurfaceDrafts.Draft? { context.drafts.draft(key) }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            field
            let hint = SurfaceValues.text(row["hint"])
            if !hint.isEmpty { Text(verbatim: hint).font(.footnote).foregroundStyle(.secondary) }
            let warning = SurfaceValues.text(row["warning"])
            if !warning.isEmpty { Text(verbatim: warning).font(.footnote).foregroundStyle(PrimitiveTint.warn) }
        }
    }

    @ViewBuilder private var field: some View {
        switch SurfaceValues.string(control["k"]) {
        case "switch":
            Toggle(label, isOn: Binding(get: { control["on"] == .bool(true) }, set: { change(.bool($0)) }))
        case "choice":
            Picker(label, selection: Binding(get: { SurfaceValues.string(control["value"]) ?? "" }, set: { change(.string($0)) })) {
                ForEach(SurfaceValues.array(control["options"]), id: \.self) { value in
                    let option = SurfaceValues.object(value)
                    if let id = SurfaceValues.string(option["value"]) {
                        Text(verbatim: SurfaceValues.text(option["label"])).tag(id)
                    }
                }
            }
        case "multi":
            Text(verbatim: label)
            ForEach(SurfaceValues.array(control["options"]), id: \.self) { value in
                let option = SurfaceValues.object(value)
                if let id = SurfaceValues.string(option["value"]) {
                    Toggle(SurfaceValues.text(option["label"]), isOn: Binding(get: {
                        SurfaceValues.strings(control["values"]).contains(id)
                    }, set: { on in
                        var values = SurfaceValues.strings(control["values"])
                        values.removeAll { $0 == id }
                        if on { values.append(id) }
                        change(.array(values.map(JSONValue.string)))
                    }))
                }
            }
        case "number", "text":
            Text(verbatim: label)
            if control["secret"] == .bool(true) {
                SecureField(label, text: binding)
            } else {
                TextField(label, text: binding)
                    .keyboardType(control["k"] == .string("number") ? .numbersAndPunctuation : .default)
            }
            if let draft {
                if draft.base != producer { Text(SurfaceCopy.inputChanged).font(.footnote).foregroundStyle(.secondary) }
                Button(SurfaceCopy.apply) {
                    if control["k"] == .string("number") {
                        if let number = Double(draft.text), number.isFinite { change(.number(number)) }
                    } else { change(.string(draft.text)) }
                }
                .disabled(draft.base != producer)
            }
        case "action":
            Button(SurfaceValues.text(control["label"])) {
                if let act = SurfaceValues.string(control["act"]) {
                    context.send(node, "action", ["act": .string(act), "value": .string(id)])
                }
            }
        default:
            Text(verbatim: label)
            Text(SurfaceCopy.unsupported).font(.footnote).foregroundStyle(.secondary)
        }
    }

    private var binding: Binding<String> {
        Binding(get: { draft?.text ?? producer }, set: { context.drafts.set($0, key: key, producer: producer) })
    }

    private func change(_ value: JSONValue) {
        context.send(node, "change", ["item": .string(id), "value": value])
    }
}
