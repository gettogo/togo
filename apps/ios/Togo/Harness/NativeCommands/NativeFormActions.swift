import Foundation
import TogoKit

extension NativeCommandContext {
    func runForm(_ action: String, target: String?, args: [String: JSONValue]) async throws -> HarnessActionResult {
        guard let target, let source = block.node.node(id: target), let live = try current().node(id: target) else {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
        }
        switch action {
        case "native.formTab":
            guard let value = SurfaceValues.string(args["value"]), source.properties == live.properties,
                  let item = SurfaceValues.entries(live.properties["items"]).first(where: { $0.id == value }),
                  !SurfaceValues.disabled(item.properties["disabled"]) else {
                throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
            }
            try await event(node: target, kind: "select", fields: ["item": .string(value)])
        case "native.edit":
            guard ["input", "editor"].contains(live.kind), let replacement = SurfaceValues.string(args["value"]),
                  let before = SurfaceValues.string(source.properties["text"]),
                  live.properties["text"] == .string(before) else {
                throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
            }
            var fields = SurfaceValues.edit(from: before, to: replacement)
            fields.removeValue(forKey: "ev")
            try await event(node: target, kind: "edit", fields: fields, expectedText: before)
            guard try current().node(id: target)?.properties["text"] == .string(replacement) else {
                throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.unconfirmed)
            }
        case "native.options":
            let desired: Set<String>
            switch args["value"] {
            case .array(let ids): desired = Set(ids.compactMap(SurfaceValues.string))
            case .string(let id): desired = [id]
            default: throw NativeStructuredScreen.Unsupported.invalid
            }
            let options = live.children.filter { $0.kind == "item" && !$0.id.hasSuffix("/other") }
            guard desired.isSubset(of: Set(options.map(\.id))),
                  selected(live) == selected(source) else {
                throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
            }
            let multiple = options.contains { $0.properties["role"] == .string("omp.ask.check") }
            guard multiple || desired.count == 1 else { throw NativeStructuredScreen.Unsupported.invalid }
            var expected = selected(live)
            let changes = multiple ? expected.symmetricDifference(desired) : desired.subtracting(expected)
            for option in options where changes.contains(option.id) {
                guard !SurfaceValues.disabled(option.properties["disabled"]),
                      let fresh = try current().node(id: target), selected(fresh) == expected else {
                    throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
                }
                try await event(node: target, kind: "select", fields: ["item": .string(option.id)])
                if multiple {
                    if expected.contains(option.id) { expected.remove(option.id) } else { expected.insert(option.id) }
                } else { expected = [option.id] }
                guard let fresh = try current().node(id: target), selected(fresh) == expected else {
                    throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.unconfirmed)
                }
            }
        case "native.focusField":
            guard source == live, let click = NativeFormScreen.click(live), click.hasPrefix("edit:") else {
                throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
            }
            try await event(node: target, kind: "action", fields: ["act": .string(click)])
        case "native.optionFocus":
            guard source == live, let list = parent(of: target, in: try current()), list.kind == "list" else {
                throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
            }
            try await event(node: list.id, kind: "select", fields: ["item": .string(target)])
        default: throw NativeStructuredScreen.Unsupported.invalid
        }
        return .init(result: .applied, screen: try screen())
    }

    private func selected(_ list: SurfaceNode) -> Set<String> {
        Set(list.children.filter { $0.kind == "item" && $0.properties["tone"] == .string("accent") && !$0.id.hasSuffix("/other") }.map(\.id))
    }

    private func parent(of id: String, in root: SurfaceNode) -> SurfaceNode? {
        if root.children.contains(where: { $0.id == id }) { return root }
        return root.children.lazy.compactMap { parent(of: id, in: $0) }.first
    }
}
