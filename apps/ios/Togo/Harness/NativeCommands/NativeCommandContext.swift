import Foundation
import TogoKit

/// Adapts shared native controls to events on the same original program.
/// Every operation rechecks its view instance and node; nothing is retried.
@MainActor struct NativeCommandContext {
    let model: ChatSurfaceInteraction
    let block: ChatSurfaceInteraction.Block
    let store: HarnessSessionStore

    func primitive() -> PrimitiveContext {
        var context = PrimitiveContext(run: { action, target, args in
            do { return try await run(action, target: target, args: args ?? [:]) }
            catch is CancellationError { return nil }
            catch { return .init(result: .failed, message: (error as? ChatSurfaceInteraction.Failure)?.message ?? PrimitiveCopy.failed) }
        }, openScreen: { _ in }, open: { _ in }, showTerminal: {
            store.retireInteraction()
            store.layer = .terminal
        })
        context.searchScreen = { query in
            let node = try current()
            let before = SurfaceValues.string(node.properties["query"]) ?? ""
            if before != query {
                try await event(node: node.id, kind: "action", fields: [
                    "act": .string("togo.query"), "before": .string(before), "query": .string(query)
                ])
            }
            return try screen()
        }
        context.preview = { id in
            let node = try current()
            if node.properties["selected"] != .string(id) {
                try await event(node: node.id, kind: "select", fields: ["item": .string(id)])
            }
        }
        context.readScreen = { try? screen() }
        context.agent = store.agentName
        context.showsTerminal = false
        context.subject = store.subject
        // The original program's observed close, not the dispatch ACK or a
        // generic action outcome, dismisses this presentation.
        return context
    }

    func screen() throws -> HarnessScreen {
        let node = try current()
        if NativeFormScreen.containsControls(node), node.kind != "prefs" {
            return try NativeFormScreen.map(node, title: model.title, workflow: model.workflow, features: model.session.companionFeatures)
        }
        switch node.kind {
        case "picker":
            return try NativePickerScreen.map(node, workflow: model.workflow, fallbackTitle: model.title, fetchedAt: WireDate(.now))
        case "prefs": return try NativeStructuredScreen.preferences(node, title: model.title)
        default: return try NativeStructuredScreen.detail(node, title: model.title)
        }
    }

    func current() throws -> SurfaceNode {
        guard !model.retired, let scene = model.session.scene,
              let view = scene.views.first(where: { $0.id == block.view.id && $0.instance == block.view.instance }),
              !view.closed, let node = view.document.root.node(id: block.node.id) else {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
        }
        return node
    }

    func run(_ action: String, target: String?, args: [String: JSONValue]) async throws -> HarnessActionResult {
        let node = try current()
        if action.hasPrefix(NativePickerScreen.actionPrefix), node.kind == "picker", let target,
           SurfaceValues.string(node.properties["selected"]) != target {
            try await event(node: node.id, kind: "select", fields: ["item": .string(target)])
            guard try current().properties["selected"] == .string(target) else {
                throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
            }
        }
        switch action {
        case "native.strip":
            let strip = SurfaceValues.object(node.properties["strip"])
            guard let value = target ?? SurfaceValues.string(args["value"]),
                  let item = SurfaceValues.entries(strip["items"]).first(where: { $0.id == value }),
                  !SurfaceValues.disabled(item.properties["disabled"]) else { throw NativeStructuredScreen.Unsupported.invalid }
            // In OMP this event chooses AND applies; no second Apply is sent.
            try await event(node: node.id, kind: "action", fields: ["act": .string("strip"), "value": .string(value)])
        case "native.edit", "native.options", "native.focusField", "native.optionFocus", "native.formTab":
            return try await runForm(action, target: target, args: args)
        case NativePickerScreen.activate:
            guard let target else { throw NativeStructuredScreen.Unsupported.invalid }
            try await event(node: node.id, kind: "activate", fields: ["item": .string(target)])
        case "native.change":
            guard let target, let value = args["value"] else { throw NativeStructuredScreen.Unsupported.invalid }
            try await event(node: node.id, kind: "change", fields: ["item": .string(target), "value": value])
        case "native.preferenceAction":
            guard let target else { throw NativeStructuredScreen.Unsupported.invalid }
            let row = SurfaceValues.entries(node.properties["sections"]).flatMap { SurfaceValues.entries($0.properties["rows"]) }
                .first { $0.id == target }
            let control = SurfaceValues.object(row?.properties["control"])
            guard let act = SurfaceValues.string(control["act"]) else { throw NativeStructuredScreen.Unsupported.invalid }
            try await event(node: node.id, kind: "action", fields: ["act": .string(act), "value": .string(target)])
        default:
            if action.hasPrefix(NativePickerScreen.actionPrefix), let act = SurfaceValues.string(args["act"]) {
                try await event(node: node.id, kind: "action", fields: ["act": .string(act)])
            } else if action.hasPrefix(NativePickerScreen.tabPrefix), let tab = args["tab"] {
                try await event(node: node.id, kind: "action", fields: ["act": .string("tab"), "value": tab])
            } else if action.hasPrefix(NativePickerScreen.scopePrefix), let scope = args["scope"] {
                try await event(node: node.id, kind: "action", fields: ["act": .string("scope"), "value": scope])
            } else if action.hasPrefix("native.node."), let id = SurfaceValues.string(args["node"]), let act = args["act"] {
                guard let source = try current().node(id: id), NativeFormScreen.click(source) == SurfaceValues.string(act) else {
                    throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
                }
                if let result = try localLinkAction(source) { return result }
                try await event(node: id, kind: "action", fields: ["act": act])
            } else if action.hasPrefix("native.editorSubmit."), let id = SurfaceValues.string(args["node"]) {
                let node = try current().node(id: id)
                guard let node, let text = SurfaceValues.string(node.properties["text"]) else {
                    throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged)
                }
                if node.properties["sendable"] == .bool(true) {
                    try await event(node: id, kind: "send", fields: ["text": .string(text)], expectedText: text)
                } else if model.workflow == "resume", model.session.companionFeatures.contains("submit") {
                    try await event(node: id, kind: "action", fields: ["act": .string("togo.submit"), "before": .string(text)], expectedText: text)
                    guard (try? current().node(id: id)) == nil else {
                        throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.unconfirmed)
                    }
                } else { throw NativeStructuredScreen.Unsupported.invalid }
            } else { throw NativeStructuredScreen.Unsupported.invalid }
        }
        return .init(result: .applied, screen: try? screen())
    }

    func cancel() async throws {
        let node = try current()
        if node.kind == "picker", let end = SurfaceValues.array(node.properties["actions"]).map(SurfaceValues.object)
            .first(where: { $0["end"] == .bool(true) }), let id = SurfaceValues.string(end["id"]) {
            try await event(node: node.id, kind: "action", fields: ["act": .string(id)])
        } else if node.kind == "prefs" {
            try await event(node: node.id, kind: "action", fields: ["act": .string("close")])
        } else if model.workflow == "resume", let picker = model.blocks.first(where: { $0.node.kind == "picker" }) {
            // OMP's rename field lives in another view; its owner remains the
            // original session picker, whose cancel dismisses the field.
            let owner = NativeCommandContext(model: model, block: picker, store: store)
            try await owner.event(node: picker.node.id, kind: "action", fields: ["act": .string("cancel")])
        } else {
            func closeButton(_ node: SurfaceNode) -> SurfaceNode? {
                if node.kind == "button", let action = SurfaceValues.string(SurfaceValues.object(node.properties["actions"])["click"]),
                   ["close", "cancel"].contains(action) { return node }
                return node.children.lazy.compactMap(closeButton).first
            }
            if let button = closeButton(node), let action = SurfaceValues.object(button.properties["actions"])["click"] {
                try await event(node: button.id, kind: "action", fields: ["act": action])
            }
        }
    }

    func page(_ id: String) async throws {
        let node = try current()
        if node.properties["page"] != .string(id) {
            try await event(node: node.id, kind: "action", fields: ["act": .string("page"), "value": .string(id)])
        }
    }

    func event(node: String, kind: String, fields: [String: JSONValue], expectedText: String? = nil) async throws {
        try Task.checkCancellation()
        _ = try current()
        guard model.session.canInteract else { throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.unconfirmed) }
        let outcome = await model.session.input(surface: block.view.id, node: node,
            event: SurfaceValues.event(kind, fields), expectedText: expectedText)
        guard outcome == .dispatched else {
            if outcome == .refused { throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.inputChanged) }
            let message = model.session.inputFailure.map { ServiceErrors.message($0) } ?? SurfaceCopy.unconfirmed
            throw ChatSurfaceInteraction.Failure(message: message)
        }
        let deadline = ContinuousClock.now + .seconds(5)
        while model.session.pending != nil {
            try Task.checkCancellation()
            guard ContinuousClock.now < deadline, model.session.notice == nil else {
                throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.unconfirmed)
            }
            try await Task.sleep(for: .milliseconds(20))
        }
        guard model.session.notice == nil else { throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.unconfirmed) }
    }
}
