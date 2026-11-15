import Foundation

/// Maps a picker the running program describes into the shared picker screen,
/// so the program's own lists (conversations, models, sign-in providers) open
/// in the same native picker as the rest of the app.
///
/// The mapping is pure: it reads one node and returns a screen. Sending the
/// program's events (activate, an action, a query) is the caller's job; the
/// action ids here say which original event each control stands for.
public enum NativePickerScreen {
    /// Run when a row is chosen: the caller sends the program `activate` for it.
    public static let activate = "native.activate"
    /// Prefix of an action that stands for one of the program's own picker
    /// actions; `args["act"]` holds the program's exact action id.
    public static let actionPrefix = "native.action."
    /// Prefix of an action that switches the picker's tab or scope;
    /// `args["tab"]` or `args["scope"]` holds the id, `args["selected"]`
    /// whether it is the current one.
    public static let tabPrefix = "native.tab."
    public static let scopePrefix = "native.scope."

    public enum Unsupported: Error, Equatable, Sendable {
        /// The node is not a picker.
        case notAPicker(kind: String)
        /// The picker has actions whose target (a row or the whole list) is
        /// not known for this workflow. Nothing is guessed from a label.
        case unknownActions(workflow: String, actions: [String])
        /// The picker holds a control of its own (a rename field, a
        /// confirmation) that a picker screen cannot show.
        case nestedControl(kind: String)
        /// An item has no id or no label.
        case malformedItem
    }

    /// Which of a workflow's actions act on the highlighted row, which on the
    /// whole list, and which row actions lose data. From the program's source
    /// (omp 18.20) and recorded screens; a workflow with other actions is
    /// unsupported. Deleting a conversation is the only one that loses data.
    struct Scope: Sendable {
        var row: Set<String> = []
        var list: Set<String> = []
        var destructive: Set<String> = []
    }

    static let scopes: [String: Scope] = [
        // The session selector: delete, rename and pin act on the highlighted
        // conversation; scope switches between this folder and all projects.
        "resume": Scope(row: ["delete", "rename", "pin"], list: ["scope"], destructive: ["delete"]),
        "model": Scope(),
        "login": Scope(),
        // The session model switch: task flips the whole picker to choosing
        // the model subagents use.
        "switch": Scope(list: ["task"]),
        // The conversation tree: summarize and label act on the highlighted
        // entry; filter cycles the list's filter.
        "tree": Scope(row: ["summarize", "label"], list: ["filter"]),
        // Agents: toggle and model act on the highlighted agent; new and
        // reload on the list.
        "agents": Scope(row: ["toggle", "model"], list: ["new", "reload"]),
        // Extensions: expand only changes how the inspector is shown.
        "extensions": Scope(list: ["expand"]),
        // The agent hub: follow, the agents scope and the search field all
        // belong to the list.
        "hub": Scope(list: ["follow", "agents", "filter"]),
    ]

    public static func map(_ node: SurfaceNode, workflow: String, fallbackTitle: String, fetchedAt: WireDate) throws -> HarnessScreen {
        guard node.kind == "picker" else { throw Unsupported.notAPicker(kind: node.kind) }
        let p = node.properties
        if let control = node.children.first(where: { ["input", "editor", "button", "overlay", "prefs", "picker"].contains($0.kind) }) {
            throw Unsupported.nestedControl(kind: control.kind)
        }

        // Actions: the primary one is what choosing a row does, the end one is
        // the system's Done or Cancel, the rest belong to a row or to the list.
        var primaryLabel: String?
        var rowActions: [HarnessActionRef] = []
        var listActions: [HarnessActionRef] = []
        var unknown: [String] = []
        let scope = scopes[workflow]
        for action in array(p["actions"]) {
            guard let fields = object(action), let id = string(fields["id"]), !id.isEmpty else { continue }
            let label = string(fields["label"]) ?? id
            if fields["end"] == .bool(true) { continue }
            if fields["primary"] == .bool(true) {
                if primaryLabel == nil { primaryLabel = label }
                continue
            }
            let args: [String: JSONValue] = ["act": .string(id)]
            if let scope, scope.row.contains(id) {
                rowActions.append(HarnessActionRef(id: actionPrefix + id, label: label,
                                                   role: scope.destructive.contains(id) ? .destructive : .normal, args: args))
            } else if let scope, scope.list.contains(id) {
                listActions.append(HarnessActionRef(id: actionPrefix + id, label: label, args: args))
            } else {
                unknown.append(id)
            }
        }
        guard unknown.isEmpty else { throw Unsupported.unknownActions(workflow: workflow, actions: unknown) }

        // Tabs and scopes the program offers, with the current one marked.
        for (key, prefix, current) in [("tabs", tabPrefix, string(p["tab"])), ("scopes", scopePrefix, string(p["scope"]))] {
            for entry in array(p[key]) {
                guard let fields = object(entry), let id = string(fields["id"]), !id.isEmpty else { continue }
                var args: [String: JSONValue] = [String(key.dropLast()): .string(id), "selected": .bool(id == current)]
                if let group = string(fields["group"]) { args["group"] = .string(group) }
                if let count = fields["count"], case .number = count { args["count"] = count }
                if let reason = string(fields["disabled"]) { args["disabled"] = .string(reason) }
                listActions.append(HarnessActionRef(id: prefix + id, label: string(fields["label"]) ?? id, args: args))
            }
        }

        // Items in the program's order, under its group headings.
        var byID: [String: HarnessScreen.Picker.Item] = [:]
        var listed: [String] = []
        for entry in array(p["items"]) {
            guard let fields = object(entry), let id = string(fields["id"]), !id.isEmpty,
                  let label = plain(fields["label"]), !label.isEmpty else { throw Unsupported.malformedItem }
            var facets: [String: String] = [:]
            if let facts = object(fields["facts"]) {
                for (name, value) in facts { if let text = plain(value), !text.isEmpty { facets[name] = text } }
            }
            for name in ["dot", "tone", "title", "icon"] {
                if let text = string(fields[name]), !text.isEmpty { facets["native." + name] = text }
            }
            if fields["mono"] == .bool(true) { facets["native.mono"] = "true" }
            let badge = array(fields["badges"]).compactMap { object($0).flatMap { string($0["text"]) } }.first
            var disabled: Bool?
            if let value = fields["disabled"], value != .bool(false), value != .null { disabled = true }
            if let depth = fields["depth"], case .number(let level) = depth { facets["native.depth"] = String(Int(level)) }
            if let kind = string(fields["node"]), !kind.isEmpty { facets["native.node"] = kind }
            byID[id] = .init(id: id, title: label, subtitle: plain(fields["detail"]), badge: badge,
                             disabled: disabled, facets: facets.isEmpty ? nil : facets, appearance: appearance(fields))
            listed.append(id)
        }
        var order: [String] = []
        var group: String?
        var seen: Set<String> = []
        for entry in array(p["order"]) {
            if let heading = object(entry) {
                group = string(heading["label"]) ?? string(heading["group"])
            } else if let id = string(entry), byID[id] != nil, seen.insert(id).inserted {
                byID[id]?.group = group
                order.append(id)
            }
        }
        // An explicitly empty order is the program's empty search result,
        // even when items still holds its unfiltered catalog.
        if p["order"] == nil {
            for id in listed where seen.insert(id).inserted { order.append(id) }
        }

        // The preview the program has drawn belongs to its highlighted item.
        if let highlighted = string(p["selected"]), byID[highlighted] != nil, let preview = preview(node.children) {
            byID[highlighted]?.preview = preview
        }

        // What is in use now is `current`; the highlight is only a cursor.
        let current = array(p["current"]).compactMap(string).first { order.contains($0) }
        let searchable = p["query"] != nil
        let picker = HarnessScreen.Picker(
            items: order.compactMap { byID[$0] }, selected: current, searchable: searchable,
            onSelect: activate, onSelectLabel: primaryLabel, searchMode: searchable ? "remote" : nil,
            contextActions: rowActions.isEmpty ? nil : rowActions,
            selectionBehavior: p["preview"] != nil ? "preview" : nil,
            presentation: presentation(p), query: string(p["query"])
        )
        let title = string(p["title"]).flatMap { $0.isEmpty ? nil : $0 } ?? fallbackTitle
        return HarnessScreen(id: node.id, primitive: .picker, title: title, fetchedAt: fetchedAt, picker: picker, actions: listActions)
    }

    /// What the program says about the list as a whole: its empty text, the
    /// search prompt, a subtitle, what it calls its rows, whether it is still
    /// loading, the row count, and which facts it shows as columns and how.
    static func presentation(_ p: [String: JSONValue]) -> HarnessPickerPresentation? {
        var columns: [HarnessPickerPresentation.Column] = []
        for entry in array(p["columns"]) {
            guard let fields = object(entry), let id = string(fields["id"]), !id.isEmpty else { continue }
            columns.append(.init(id: id, format: string(fields["format"])))
        }
        var total: Int?
        if case .number(let count)? = p["total"], count >= 0, count < 1e9 { total = Int(count) }
        let result = HarnessPickerPresentation(
            emptyText: nonEmpty(plain(p["empty"])), searchPrompt: nonEmpty(plain(p["placeholder"])),
            subtitle: nonEmpty(plain(p["subtitle"])), noun: nonEmpty(string(p["noun"])), state: nonEmpty(string(p["state"])),
            message: nonEmpty(plain(p["message"])), total: total, columns: columns.isEmpty ? nil : columns
        )
        return result == HarnessPickerPresentation() ? nil : result
    }

    /// How the program draws one row beyond its title: badges, the chips of
    /// a model's roles, its avatar, its depth in a tree, its status.
    static func appearance(_ fields: [String: JSONValue]) -> HarnessPickerItemAppearance? {
        var badges: [HarnessPickerItemAppearance.Badge] = []
        for entry in array(fields["badges"]) {
            guard let badge = object(entry), let text = nonEmpty(plain(badge["text"])) else { continue }
            badges.append(.init(text: text, tone: string(badge["tone"])))
        }
        var chips: [HarnessPickerItemAppearance.Chip] = []
        for entry in array(fields["chips"]) {
            guard let chip = object(entry), let text = nonEmpty(plain(chip["text"])) else { continue }
            var on: Bool?, auto: Bool?
            if case .bool(let value)? = chip["on"] { on = value }
            if case .bool(let value)? = chip["auto"] { auto = value }
            chips.append(.init(text: text, on: on, auto: auto))
        }
        var mark: HarnessPickerItemAppearance.Mark?
        if let fields = object(fields["mark"]) {
            let value = HarnessPickerItemAppearance.Mark(text: nonEmpty(string(fields["text"])), icon: nonEmpty(string(fields["icon"])),
                                                         seed: nonEmpty(string(fields["seed"])))
            if value != .init() { mark = value }
        }
        var depth: Int?
        if case .number(let level)? = fields["depth"], level >= 0, level < 1024 { depth = Int(level) }
        let result = HarnessPickerItemAppearance(
            badges: badges.isEmpty ? nil : badges, chips: chips.isEmpty ? nil : chips, mark: mark, depth: depth,
            status: nonEmpty(string(fields["dot"])), tone: nonEmpty(string(fields["tone"])),
            icon: nonEmpty(string(fields["icon"])), nodeKind: nonEmpty(string(fields["node"]))
        )
        return result == HarnessPickerItemAppearance() ? nil : result
    }

    private static func nonEmpty(_ text: String?) -> String? {
        guard let text, !text.isEmpty else { return nil }
        return text
    }

    /// The preview pane: its title, the facts, and each section's text as the
    /// program wrote it. Nil when the pane holds nothing a detail can show.
    static func preview(_ children: [SurfaceNode]) -> HarnessScreen.Detail? {
        var subtitle: String?
        var facts: [HarnessFact] = []
        var sections: [HarnessScreen.Detail.Section] = []
        func body(_ node: SurfaceNode) -> HarnessScreen.Detail.Body? {
            switch node.kind {
            case "md": return text(node).map { .init(kind: "markdown", text: $0) }
            case "code": return text(node).map { .init(kind: "code", text: $0) }
            case "text": return text(node).map { .init(kind: "text", text: $0) }
            default: return nil
            }
        }
        for child in children {
            switch child.kind {
            case "text" where string(child.properties["role"])?.hasSuffix("picker.title") == true:
                subtitle = text(child)
            case "kv":
                for entry in array(child.properties["items"]) {
                    guard let fields = object(entry), let label = plain(fields["k"]), let value = plain(fields["v"]),
                          !label.isEmpty else { continue }
                    facts.append(HarnessFact(label: label, value: value))
                }
            case "section":
                var title = string(child.properties["head"])
                for part in child.children {
                    guard let body = body(part) else { continue }
                    sections.append(.init(title: title, body: body))
                    title = nil
                }
            default:
                if let body = body(child) { sections.append(.init(body: body)) }
            }
        }
        guard subtitle != nil || !facts.isEmpty || !sections.isEmpty else { return nil }
        return .init(subtitle: subtitle, body: .init(kind: "kv", kv: facts), sections: sections.isEmpty ? nil : sections)
    }

    private static func text(_ node: SurfaceNode) -> String? {
        if let text = string(node.properties["text"]), !text.isEmpty { return text }
        if let spans = plain(node.properties["spans"]), !spans.isEmpty { return spans }
        return nil
    }

    private static func string(_ value: JSONValue?) -> String? {
        if case .string(let text)? = value { return text }
        return nil
    }

    private static func array(_ value: JSONValue?) -> [JSONValue] {
        if case .array(let list)? = value { return list }
        return []
    }

    private static func object(_ value: JSONValue?) -> [String: JSONValue]? {
        if case .object(let fields)? = value { return fields }
        return nil
    }

    /// A value the program gives as text or as styled spans, as plain text.
    private static func plain(_ value: JSONValue?) -> String? {
        switch value {
        case .string(let text)?: return text
        case .array(let spans)?:
            let parts = spans.compactMap { span -> String? in
                if let text = string(span) { return text }
                return object(span).flatMap { string($0["t"]) }
            }
            return parts.isEmpty ? nil : parts.joined()
        default: return nil
        }
    }
}
