import Foundation
import TogoKit

/// Maps the program's semantic form, not its desktop layout. Changes remain
/// in the original form until its own Submit validates and accepts them.
enum NativeFormScreen {
    static func containsControls(_ node: SurfaceNode) -> Bool {
        guard node.properties["hidden"] != .bool(true) else { return false }
        return ["input", "editor"].contains(node.kind) || node.properties["strip"] != nil
            || node.properties["role"] == .string("omp.ask.field") || node.children.contains(where: containsControls)
    }

    static func map(_ root: SurfaceNode, title: String, workflow: String = "", features: Set<String> = []) throws -> HarnessScreen {
        if case .object(let strip)? = root.properties["strip"] {
            let options = SurfaceValues.entries(strip["items"]).map { item in
                HarnessScreen.Picker.Item(id: item.id, title: NativeStructuredScreen.text(item.properties["label"]) ?? item.id,
                    disabled: SurfaceValues.disabled(item.properties["disabled"]))
            }
            let label = NativeStructuredScreen.text(strip["label"]) ?? title
            // strip.selected is keyboard focus, not an assigned role. Every
            // row remains tappable, including the focused row (assign/clear).
            return .init(id: root.id, primitive: .picker, title: label, fetchedAt: WireDate(.now),
                picker: .init(items: options, searchable: false, onSelect: "native.strip"))
        }
        if workflow == "resume", containsControls(root), !features.contains("submit") {
            throw NativeStructuredScreen.Unsupported.feature("submit")
        }
        let header = SurfaceValues.headingNode(root)
        let title = SurfaceValues.heading(root) ?? title
        var sections: [HarnessScreen.Settings.Section] = []
        var actions: [HarnessActionRef] = []
        func visit(_ node: SurfaceNode, heading: String? = nil) throws {
            guard node.properties["hidden"] != .bool(true), node.id != header?.id else { return }
            let role = SurfaceValues.string(node.properties["role"])
            guard role?.hasSuffix(".hints") != true,
                  !(node.kind == "row" && node.children.contains { $0.kind == "kbd" }) else { return }
            if role == "omp.ask.field" {
                sections.append(.init(title: heading, fields: try fields(node, workflow: workflow, features: features)))
            } else if ["input", "editor"].contains(node.kind) {
                sections.append(.init(fields: [editor(node, label: heading ?? title, workflow: workflow, features: features)]))
                if node.properties["sendable"] == .bool(true) || (workflow == "resume" && features.contains("submit")) {
                    actions.append(.init(id: "native.editorSubmit." + node.id, label: PrimitiveCopy.save, role: .primary,
                        args: ["node": .string(node.id)]))
                }
            } else if node.kind == "button", let act = click(node), !["close", "cancel", "hide"].contains(act) {
                actions.append(.init(id: "native.node." + node.id,
                    label: NativeStructuredScreen.text(node.properties["text"]) ?? act,
                    role: node.properties["variant"] == .string("primary") ? .primary : .normal,
                    args: ["node": .string(node.id), "act": .string(act)]))
            } else if node.kind == "tabs" {
                let choices = SurfaceValues.entries(node.properties["items"]).map { item in
                    HarnessChoice(id: item.id, label: NativeStructuredScreen.text(item.properties["label"]) ?? item.id,
                        disabled: SurfaceValues.disabled(item.properties["disabled"]))
                }
                sections.append(.init(fields: [.init(id: node.id, label: title, kind: .choice,
                    value: node.properties["active"] ?? .null, options: choices, action: "native.formTab")]))
            } else if ["text", "md", "code"].contains(node.kind),
                      !["omp.hints", "omp.actions.hints"].contains(role ?? ""),
                      let text = NativeStructuredScreen.text(node.properties["text"] ?? node.properties["spans"]) {
                sections.append(.init(footer: text, fields: []))
            } else if node.kind == "kv" {
                let fields = SurfaceValues.array(node.properties["items"]).enumerated().compactMap { index, value -> HarnessScreen.Settings.Field? in
                    let value = SurfaceValues.object(value)
                    guard let label = NativeStructuredScreen.text(value["k"]), let content = NativeStructuredScreen.text(value["v"]) else { return nil }
                    return .init(id: node.id + "/" + String(index), label: label, kind: .text, value: .string(content), readOnly: true)
                }
                sections.append(.init(title: heading, fields: fields))
            } else if node.kind == "spinner", let label = NativeStructuredScreen.text(node.properties["label"]) {
                sections.append(.init(footer: label, fields: []))
            } else {
                let labelNode = role?.hasSuffix(".field") == true ? node.children.first(where: { $0.kind == "text" }) : nil
                let fieldLabel = labelNode.flatMap { NativeStructuredScreen.text($0.properties["spans"] ?? $0.properties["text"]) }
                let heading = fieldLabel ?? NativeStructuredScreen.text(node.properties["head"]) ?? heading
                for child in node.children where child.id != labelNode?.id { try visit(child, heading: heading) }
            }
        }
        try visit(root)
        if root.kind == "picker" {
            actions += SurfaceValues.entries(root.properties["actions"]).filter { $0.properties["end"] != .bool(true) }.map { action in
                .init(id: NativePickerScreen.actionPrefix + action.id,
                    label: NativeStructuredScreen.text(action.properties["label"]) ?? action.id, args: ["act": .string(action.id)])
            }
        }
        guard !sections.isEmpty else { throw NativeStructuredScreen.Unsupported.invalid }
        return .init(id: root.id, primitive: .settings, title: NativeStructuredScreen.text(root.properties["head"]) ?? title, fetchedAt: WireDate(.now),
            settings: .init(sections: sections), actions: actions)
    }

    static func editor(_ node: SurfaceNode, label: String, workflow: String, features: Set<String> = [], warning: String? = nil) -> HarnessScreen.Settings.Field {
        // Masking describes how the program draws, not whether an input is
        // sensitive: OMP's API-key inputs explicitly set mask=false.
        let knownPlain = ["resume", "tree"].contains(workflow)
        let secret = node.properties["mask"] == .bool(true) || node.properties["secret"] == .bool(true) || !knownPlain
        return .init(id: node.id, label: label, kind: secret ? .secret : .text,
            value: secret ? .null : node.properties["text"] ?? .string(""),
            isSet: secret ? !(SurfaceValues.string(node.properties["text"]) ?? "").isEmpty : nil,
            placeholder: NativeStructuredScreen.text(node.properties["placeholder"]),
            readOnly: node.properties["readonly"] == .bool(true) || node.properties["typeahead"] == .bool(true),
            action: "native.edit", warning: warning)
    }

    private static func fields(_ field: SurfaceNode, workflow: String, features: Set<String>) throws -> [HarnessScreen.Settings.Field] {
        let labelNode = field.children.first { $0.properties["role"] == .string("omp.ask.label") }
        let label = labelNode.flatMap { NativeStructuredScreen.text($0.properties["spans"] ?? $0.properties["text"]) }
            ?? String(localized: "native.form.answer", defaultValue: "Answer")
        let warning = field.children.first { $0.properties["role"] == .string("omp.ask.error") }
            .flatMap { NativeStructuredScreen.text($0.properties["spans"] ?? $0.properties["text"]) }
        var result: [HarnessScreen.Settings.Field] = []
        for child in field.children {
            if ["input", "editor"].contains(child.kind) {
                result.append(editor(child, label: label, workflow: workflow, features: features, warning: warning))
            } else if child.kind == "list" {
                let items = child.children.filter { $0.kind == "item" }
                let regular = items.filter { !$0.id.hasSuffix("/other") }
                let multi = regular.contains { $0.properties["role"] == .string("omp.ask.check") }
                if !regular.isEmpty { result.append(options(child, items: regular, label: label, multi: multi, warning: warning)) }
                if let other = items.first(where: { $0.id.hasSuffix("/other") }) {
                    result.append(.init(id: other.id, label: label, kind: .action, value: .null,
                        action: "native.optionFocus", actionLabel: NativeStructuredScreen.text(other.properties["label"])))
                }
            } else if let act = click(child), act.hasPrefix("edit:") {
                // A well may show a placeholder or a masked password. Do not
                // copy its display text into a new draft; focus the real input.
                result.append(.init(id: child.id, label: label, kind: .action, value: .null,
                    action: "native.focusField", warning: warning, actionLabel: String(localized: "native.form.edit", defaultValue: "Edit Answer")))
            }
        }
        guard !result.isEmpty else { throw NativeStructuredScreen.Unsupported.field(field.id) }
        return result
    }

    private static func options(_ node: SurfaceNode, items: [SurfaceNode], label: String, multi: Bool,
                                warning: String? = nil) -> HarnessScreen.Settings.Field {
        let choices = items.map { item in
            HarnessChoice(id: item.id, label: NativeStructuredScreen.text(item.properties["label"]) ?? item.id,
                detail: NativeStructuredScreen.text(item.properties["detail"]), disabled: SurfaceValues.disabled(item.properties["disabled"]))
        }
        // HtmlFormDialog.describeField (OMP 18.20) emits checked answers as
        // tone=accent; list.selected is keyboard focus, never an answer.
        // The real form fixture pins this source contract.
        let selected = items.filter { $0.properties["tone"] == .string("accent") }.map { JSONValue.string($0.id) }
        let value: JSONValue = multi ? .array(selected) : selected.first ?? .null
        return .init(id: node.id, label: label, kind: multi ? .multi : .choice, value: value, options: choices,
            action: "native.options", warning: warning)
    }

    static func click(_ node: SurfaceNode) -> String? {
        SurfaceValues.string(SurfaceValues.object(node.properties["actions"])["click"])
    }
}
