import Foundation
import TogoKit

/// Reads protocol values without turning machine text into markup or code.
enum SurfaceValues {
    /// OMP hoists active dialogs into its layer region. Their native sheet
    /// replaces the underlay instead of placing the dialog below chat history.
    static func presentedNode(_ root: SurfaceNode) -> SurfaceNode {
        let layer = root.children.first { $0.id == "layer" }
        if let modal = layer?.children.last(where: { $0.properties["hidden"] != .bool(true) }) { return modal }
        return form(root) ?? root
    }

    static func chatInteraction(_ root: SurfaceNode, mode: SurfaceView.Mode) -> SurfaceNode? {
        guard root.properties["hidden"] != .bool(true) else { return nil }
        if mode != .inline { return root }
        if let layer = root.children.first(where: { $0.id == "layer" }),
           let modal = layer.children.last(where: { $0.properties["hidden"] != .bool(true) }) { return modal }
        return form(root)
    }

    private static func form(_ node: SurfaceNode) -> SurfaceNode? {
        guard node.properties["hidden"] != .bool(true) else { return nil }
        if node.properties["role"] == .string("omp.editor.ask") { return node }
        for child in node.children {
            if let found = form(child) { return found }
        }
        return nil
    }

    static func draftAnchor(for id: String, in root: SurfaceNode) -> String {
        func find(_ node: SurfaceNode, field: String?) -> String? {
            let field = node.properties["role"] == .string("omp.ask.field") ? node.id : field
            if node.id == id { return field ?? id }
            for child in node.children {
                if let found = find(child, field: field) { return found }
            }
            return nil
        }
        return find(root, field: nil) ?? id
    }

    static func string(_ value: JSONValue?) -> String? {
        if case .string(let text) = value { return text }
        return nil
    }

    static func number(_ value: JSONValue?) -> Double? {
        if case .number(let number) = value, number.isFinite { return number }
        return nil
    }

    static func object(_ value: JSONValue?) -> [String: JSONValue] {
        if case .object(let properties) = value { return properties }
        return [:]
    }

    static func array(_ value: JSONValue?) -> [JSONValue] {
        if case .array(let values) = value { return values }
        return []
    }

    static func text(_ value: JSONValue?) -> String {
        if let text = string(value) { return text }
        return array(value).map { span in
            let properties = object(span)
            let styles = (string(properties["s"]) ?? "").split(separator: " ")
            return styles.contains("hide") ? "" : string(properties["t"]) ?? ""
        }.joined()
    }

    static func disabled(_ value: JSONValue?) -> Bool {
        value == .bool(true) || !(string(value) ?? "").isEmpty
    }

    static func strings(_ value: JSONValue?) -> [String] { array(value).compactMap { string($0) } }

    static func entries(_ value: JSONValue?) -> [SurfacePickerEntry] {
        var seen = Set<String>()
        return array(value).compactMap { value in
            let properties = object(value)
            guard let id = string(properties["id"]), seen.insert(id).inserted else { return nil }
            return SurfacePickerEntry(id: id, properties: properties)
        }
    }

    static func event(_ kind: String, _ fields: [String: JSONValue] = [:]) -> [String: JSONValue] {
        var value = fields
        value["ev"] = .string(kind)
        return value
    }

    static func edit(from current: String, to replacement: String) -> [String: JSONValue] {
        event("edit", ["from": .number(0), "to": .number(Double(current.utf16.count)),
                       "text": .string(replacement), "cursor": .number(Double(replacement.utf16.count)),
                       "len": .number(Double(current.utf16.count))])
    }
}

enum SurfaceCopy {
    static let open = String(localized: "surface.open", defaultValue: "Open")
    static let openForm = String(localized: "surface.openForm", defaultValue: "Open this form in chat.")
    static let deskDraft = String(localized: "surface.deskDraft", defaultValue: "Text is waiting in Terminal. Send or clear it there before running another command.")
    static let agentWorking = String(localized: "surface.agentWorking", defaultValue: "The agent is working. Run this command after the turn finishes.")
    static let commandSent = String(localized: "surface.commandSent", defaultValue: "Command sent")
    static let openingForm = String(localized: "surface.openingForm", defaultValue: "Opening form…")
    static let openingCommand = String(localized: "surface.openingCommand", defaultValue: "Opening command…")
    static let finishCurrent = String(localized: "surface.finishCurrent", defaultValue: "Finish or close the current screen before sending another command.")
    static let interface = String(localized: "surface.interface", defaultValue: "Harness interface")
    static let close = String(localized: "surface.close", defaultValue: "Close")
    static let terminal = String(localized: "surface.terminal", defaultValue: "Terminal")
    static let loading = String(localized: "surface.loading", defaultValue: "Opening the harness interface…")
    static let unavailable = String(localized: "surface.unavailable", defaultValue: "This interface couldn't open here. Continue in Terminal.")
    static let unconfirmed = String(localized: "surface.unconfirmed", defaultValue: "Change not confirmed. It may already have reached the agent. Check Terminal before trying again.")
    static let empty = String(localized: "surface.empty", defaultValue: "The harness hasn't shown an interface yet.")
    static let apply = String(localized: "surface.apply", defaultValue: "Apply")
    static let send = String(localized: "surface.send", defaultValue: "Send")
    static let waiting = String(localized: "surface.waiting", defaultValue: "Waiting for the harness…")
    static let unsupported = String(localized: "surface.unsupported", defaultValue: "Continue with this control in Terminal.")
    static let inputChanged = String(localized: "surface.inputChanged", defaultValue: "The input changed at the desk. Your draft is kept here.")
    static let selected = String(localized: "surface.selected", defaultValue: "Selected")
    static let choose = String(localized: "surface.choose", defaultValue: "Choose")
    static let search = String(localized: "surface.search", defaultValue: "Search")
    static let searchTooLong = String(localized: "surface.searchTooLong", defaultValue: "Search is too long. Shorten it to apply.")
}
