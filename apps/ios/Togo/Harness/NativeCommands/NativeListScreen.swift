import Foundation
import TogoKit

/// The program's plain selector is a list, not a picker node. Choosing an
/// item commits immediately; its selected id is keyboard focus only.
enum NativeListScreen {
    static let choose = "native.listChoose"

    static func list(in root: SurfaceNode) -> SurfaceNode? {
        guard !NativeFormScreen.containsControls(root) else { return nil }
        func find(_ node: SurfaceNode) -> SurfaceNode? {
            guard node.properties["hidden"] != .bool(true) else { return nil }
            if node.properties["role"] == .string("omp.select") {
                return node.children.first { $0.kind == "list" && $0.properties["hidden"] != .bool(true) }
            }
            return node.children.lazy.compactMap(find).first
        }
        return find(root)
    }

    static func map(_ root: SurfaceNode, title: String) throws -> HarnessScreen {
        guard let list = list(in: root) else { throw NativeStructuredScreen.Unsupported.invalid }
        let items = list.children.filter { $0.kind == "item" && $0.properties["hidden"] != .bool(true) }.map { item in
            HarnessScreen.Picker.Item(id: item.id,
                title: SurfaceValues.text(item.properties["label"]),
                subtitle: NativeStructuredScreen.text(item.properties["detail"]),
                disabled: SurfaceValues.disabled(item.properties["disabled"]))
        }
        return .init(id: root.id, primitive: .picker, title: SurfaceValues.heading(root) ?? title, fetchedAt: WireDate(.now),
            picker: .init(items: items, searchable: true, onSelect: choose,
                presentation: .init(emptyText: NativeStructuredScreen.text(list.properties["empty"]))))
    }
}
