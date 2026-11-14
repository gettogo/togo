import Foundation
import Observation
import TogoKit

/// Drafts belong to one producer instance, not a layout or recycled row.
@MainActor @Observable final class SurfaceDrafts {
    struct Key: Hashable {
        var surface: String
        var instance: UInt64
        var node: String
        var field: String? = nil
        /// The containing form field stays mounted while its Input loses focus.
        var anchor: String? = nil
    }

    struct Draft {
        var base: String
        var text: String
    }

    private var values: [Key: Draft] = [:]
    private var generation: String?

    func bind(_ generation: String?) {
        guard self.generation != generation else { return }
        values.removeAll()
        self.generation = generation
    }

    func draft(_ key: Key) -> Draft? { values[key] }

    func set(_ text: String, key: Key, producer: String) {
        values[key] = Draft(base: values[key]?.base ?? producer, text: text)
    }

    func reconcile(_ scene: SurfaceScene) {
        values = values.filter { key, draft in
            guard let view = scene.views.first(where: { $0.id == key.surface && $0.instance == key.instance }),
                  !view.closed else { return false }
            if let anchor = key.anchor, view.document.root.node(id: anchor) == nil { return false }
            guard let node = view.document.root.node(id: key.node) else { return true }
            if let field = key.field {
                let rows = SurfaceValues.array(node.properties["sections"]).flatMap {
                    SurfaceValues.array(SurfaceValues.object($0)["rows"])
                }.map(SurfaceValues.object)
                guard let row = rows.first(where: { SurfaceValues.string($0["id"]) == field }) else { return true }
                let value = SurfaceValues.object(row["control"])["value"]
                if let number = SurfaceValues.number(value), Double(draft.text) == number { return false }
                return SurfaceValues.string(value) != draft.text
            }
            return SurfaceValues.string(node.properties["text"]) != draft.text
        }
    }

    func clear() { values.removeAll() }
}
