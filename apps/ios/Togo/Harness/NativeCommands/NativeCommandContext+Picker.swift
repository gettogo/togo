import Foundation
import TogoKit

extension NativeCommandContext {
    func pickerRow(_ id: String, in node: SurfaceNode) throws {
        guard node.kind == "picker",
              let row = SurfaceValues.entries(node.properties["items"]).first(where: { $0.id == id }),
              row.properties["hidden"] != .bool(true), !SurfaceValues.disabled(row.properties["disabled"]) else {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
        }
        if let order = node.properties["order"], !SurfaceValues.array(order).contains(.string(id)) {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
        }
    }

    func pickerPrimary(in node: SurfaceNode, args: [String: JSONValue]) throws {
        let primary = SurfaceValues.entries(node.properties["actions"]).first { $0.properties["primary"] == .bool(true) }
        if let expected = args[NativePickerScreen.primaryKey], expected != (primary.map { .string($0.id) } ?? .null) {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
        }
        if let label = args[NativePickerScreen.primaryLabelKey], label != (primary?.properties["label"] ?? .null) {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
        }
        if let primary { try enabled(primary.properties) }
    }

    func pickerAction(_ act: String, target: String?, args: [String: JSONValue]) async throws {
        var node = try current()
        if let target {
            try pickerRow(target, in: node)
            if node.properties["selected"] != .string(target) {
                try await event(node: node.id, kind: "select", fields: ["item": .string(target)])
                node = try current()
            }
            guard node.properties["selected"] == .string(target) else {
                throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
            }
        } else if let selected = args[NativePickerScreen.selectedKey], selected != (node.properties["selected"] ?? .null) {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
        }
        guard let action = SurfaceValues.entries(node.properties["actions"]).first(where: { $0.id == act }),
              action.properties["end"] != .bool(true) else {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
        }
        if let label = args[NativePickerScreen.labelKey], label != action.properties["label"] {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
        }
        if target == nil, let on = args[NativePickerScreen.onKey], on != action.properties["on"] {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
        }
        try enabled(action.properties)
        try await event(node: node.id, kind: "action", fields: ["act": .string(act)])
    }

    private func enabled(_ properties: [String: JSONValue]) throws {
        guard !SurfaceValues.disabled(properties["disabled"]) else {
            throw ChatSurfaceInteraction.Failure(message: NativeStructuredScreen.text(properties["disabled"]) ?? SurfaceCopy.inputChanged)
        }
    }
}
