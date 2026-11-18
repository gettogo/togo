import Foundation
import TogoKit

extension NativeCommandContext {
    func cancel() async throws {
        let node = try current()
        func endAction(_ node: SurfaceNode) -> (String, String)? {
            guard node.properties["hidden"] != .bool(true) else { return nil }
            if let end = SurfaceValues.array(node.properties["actions"]).map(SurfaceValues.object)
                .first(where: { $0["end"] == .bool(true) }), let id = SurfaceValues.string(end["id"]) {
                return (node.id, id)
            }
            return node.children.lazy.compactMap(endAction).first
        }
        if let (target, action) = endAction(node) {
            try await event(node: target, kind: "action", fields: ["act": .string(action)])
        } else if node.kind == "prefs" {
            try await event(node: node.id, kind: "action", fields: ["act": .string("close")])
        } else if model.workflow == "resume", let picker = model.blocks.first(where: { $0.node.kind == "picker" }) {
            // OMP's rename field lives in another view; its owner remains the
            // original session picker, whose cancel dismisses the field.
            let owner = NativeCommandContext(model: model, block: picker, store: store)
            try await owner.event(node: picker.node.id, kind: "action", fields: ["act": .string("cancel")])
        } else {
            func closeButton(_ node: SurfaceNode) -> SurfaceNode? {
                guard node.properties["hidden"] != .bool(true) else { return nil }
                if node.kind == "button", let action = SurfaceValues.string(SurfaceValues.object(node.properties["actions"])["click"]),
                   ["close", "cancel"].contains(action) { return node }
                return node.children.lazy.compactMap(closeButton).first
            }
            if let button = closeButton(node), let action = SurfaceValues.object(button.properties["actions"])["click"] {
                try await event(node: button.id, kind: "action", fields: ["act": action])
            } else if model.session.companionFeatures.contains("escape") {
                try await event(node: node.id, kind: "action", fields: ["act": .string("togo.escape")])
            } else {
                throw NativeStructuredScreen.Unsupported.feature("escape")
            }
        }
        // An acknowledgement or a changed label is not a close. The original
        // block must leave, or give way to its next dialog, before returning.
        let deadline = ContinuousClock.now + .seconds(5)
        while !model.retired && model.blocks.last?.id == block.id {
            try Task.checkCancellation()
            guard ContinuousClock.now < deadline, model.session.notice == nil else {
                throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.unconfirmed)
            }
            try await Task.sleep(for: .milliseconds(20))
        }
    }

}
