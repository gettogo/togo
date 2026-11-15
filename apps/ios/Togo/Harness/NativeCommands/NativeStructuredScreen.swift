import Foundation
import TogoKit

/// Semantic source data becomes the app's native primitives. Layout nodes
/// never choose fonts, spacing, controls or navigation in the product UI.
enum NativeStructuredScreen {
    enum Unsupported: Error { case node(String), field(String), feature(String), invalid }

    static func primary(_ root: SurfaceNode) -> SurfaceNode {
        func dialog(_ node: SurfaceNode) -> SurfaceNode? {
            guard node.properties["hidden"] != .bool(true) else { return nil }
            if node.kind == "card", node.properties["role"] == .string("omp.overlay"), NativeFormScreen.containsControls(node) { return node }
            return node.children.lazy.compactMap(dialog).first
        }
        if let dialog = dialog(root) { return dialog }
        func find(_ node: SurfaceNode) -> SurfaceNode? {
            guard node.properties["hidden"] != .bool(true) else { return nil }
            if ["picker", "prefs", "overlay"].contains(node.kind) || node.properties["role"] == .string("omp.editor.ask") { return node }
            for child in node.children { if let result = find(child) { return result } }
            return nil
        }
        return find(root) ?? root
    }

    static func preferences(_ node: SurfaceNode, title: String) throws -> HarnessScreen {
        guard node.kind == "prefs" else { throw Unsupported.node(node.kind) }
        let sections = try SurfaceValues.entries(node.properties["sections"]).map { entry in
            let section = entry.properties
            let fields = try SurfaceValues.entries(section["rows"]).map { try field($0) }
            return HarnessScreen.Settings.Section(title: text(section["title"]), footer: text(section["lead"]), fields: fields)
        }
        return HarnessScreen(id: node.id, primitive: .settings,
            title: text(node.properties["title"]) ?? title, fetchedAt: WireDate(.now), settings: .init(sections: sections))
    }

    private static func field(_ entry: SurfacePickerEntry) throws -> HarnessScreen.Settings.Field {
        let row = entry.properties
        let control = SurfaceValues.object(row["control"])
        let options = SurfaceValues.array(control["options"]).compactMap { option -> HarnessChoice? in
            let option = SurfaceValues.object(option)
            guard let id = SurfaceValues.string(option["value"]), let label = text(option["label"]) else { return nil }
            return HarnessChoice(id: id, label: label)
        }
        let label = text(row["label"]) ?? entry.id
        let kind: HarnessScreen.Settings.FieldKind
        let value: JSONValue
        switch SurfaceValues.string(control["k"]) {
        case "switch": kind = .toggle; value = control["on"] ?? .bool(false)
        case "choice": kind = .choice; value = control["value"] ?? .null
        case "multi": kind = .multi; value = control["values"] ?? .array([])
        case "number": kind = .number; value = control["value"] ?? .null
        case "text":
            kind = control["secret"] == .bool(true) ? .secret : .text
            value = kind == .secret ? .null : control["value"] ?? .string("")
        case "action": kind = .action; value = .null
        default: throw Unsupported.field(entry.id)
        }
        let disabled = SurfaceValues.disabled(row["disabled"]) || SurfaceValues.disabled(control["disabled"])
        return .init(id: entry.id, label: label, help: text(row["hint"]), kind: kind, value: value,
            isSet: kind == .secret ? (control["value"] != nil && control["value"] != .string("")) : nil,
            options: options.isEmpty ? nil : options, min: SurfaceValues.number(control["min"]), max: SurfaceValues.number(control["max"]),
            readOnly: disabled, action: kind == .action ? "native.preferenceAction" : "native.change",
            warning: text(row["warning"]), actionLabel: text(control["label"]))
    }

    static func detail(_ root: SurfaceNode, title: String) throws -> HarnessScreen {
        var sections: [HarnessScreen.Detail.Section] = []
        var actions: [HarnessActionRef] = []
        func visit(_ node: SurfaceNode, heading: String? = nil) throws {
            let p = node.properties
            guard p["hidden"] != .bool(true) else { return }
            let role = SurfaceValues.string(p["role"]) ?? ""
            if ["omp.hints", "omp.actions.hints"].contains(role) || node.kind == "kbd" { return }
            switch node.kind {
            case "kv":
                let facts = SurfaceValues.array(p["items"]).compactMap { value -> HarnessFact? in
                    let value = SurfaceValues.object(value)
                    guard let label = text(value["k"]), let content = text(value["v"]) else { return nil }
                    return HarnessFact(label: label, value: content)
                }
                if !facts.isEmpty { sections.append(.init(title: heading, body: .init(kind: "kv", kv: facts))) }
            case "text", "md", "code", "diff":
                if let content = text(p["text"] ?? p["spans"]) {
                    sections.append(.init(title: heading, body: .init(kind: node.kind == "md" ? "markdown" : node.kind, text: content)))
                }
            case "table":
                let columns = SurfaceValues.entries(p["cols"])
                let facts = SurfaceValues.entries(p["rows"]).compactMap { row -> HarnessFact? in
                    let cells = SurfaceValues.object(row.properties["cells"])
                    guard let first = columns.first, let label = text(cells[first.id]) else { return nil }
                    let values = columns.dropFirst().compactMap { text(cells[$0.id]) }
                    return HarnessFact(label: label, value: values.joined(separator: " · "))
                }
                if !facts.isEmpty { sections.append(.init(title: heading, body: .init(kind: "kv", kv: facts))) }
            case "button":
                let click = SurfaceValues.string(SurfaceValues.object(p["actions"])["click"])
                if let click, !["close", "cancel"].contains(click) {
                    actions.append(.init(id: "native.node." + node.id, label: text(p["text"]) ?? click,
                        args: ["node": .string(node.id), "act": .string(click)]))
                }
            case "icon", "spacer", "rule": break
            case "overlay", "col", "row", "section", "card":
                if role == "omp.info.field", let labelNode = node.children.first(where: { $0.properties["role"] == .string("omp.info.label") }) {
                    let label = text(labelNode.properties["text"] ?? labelNode.properties["spans"]) ?? ""
                    let values = node.children.filter { $0.id != labelNode.id }.flatMap(leafText)
                    if !label.isEmpty, !values.isEmpty {
                        sections.append(.init(body: .init(kind: "kv", kv: [.init(label: label, value: values.joined(separator: " "))])))
                    }
                } else {
                    let heading = text(p["head"]) ?? heading
                    for child in node.children { try visit(child, heading: heading) }
                }
            default: throw Unsupported.node(node.kind)
            }
        }
        try visit(root)
        guard !sections.isEmpty else { throw Unsupported.invalid }
        return HarnessScreen(id: root.id, primitive: .detail, title: text(root.properties["head"]) ?? title,
            fetchedAt: WireDate(.now), detail: .init(body: sections[0].body, actions: actions,
                sections: Array(sections.dropFirst())))
    }

    private static func leafText(_ node: SurfaceNode) -> [String] {
        if ["text", "md", "code"].contains(node.kind), let value = text(node.properties["text"] ?? node.properties["spans"]) { return [value] }
        return node.children.flatMap(leafText)
    }

    static func text(_ value: JSONValue?) -> String? {
        let text = SurfaceValues.text(value)
        return text.isEmpty ? nil : text
    }
}
