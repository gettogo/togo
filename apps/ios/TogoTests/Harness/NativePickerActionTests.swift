import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct NativePickerActionTests {
    private let support = ChatSurfaceInteractionTests()

    @Test func unknownWorkflowRunsOnlyTheDeclaredActionInItsObservedContext() async throws {
        try await withContext { context, connection, _, _ in
            let ref = try #require(try context.screen().actions.first)
            #expect(ref.sourceOn == false)
            #expect(!ref.sourceDisabled)
            var seen: [String: JSONValue] = [:]
            let after = try scene(revision: 2, on: true)
            connection.onInput = { [weak connection] event in
                if case .object(let fields) = event { seen = fields }
                connection?.next = after
            }
            _ = try await context.run(ref.id, target: nil, args: ref.args ?? [:])
            #expect(connection.inputs == 1)
            #expect(seen["ev"] == .string("action"))
            #expect(seen["id"] == .string(context.block.node.id))
            #expect(seen["act"] == .string("expand"))
            #expect(seen["value"] == nil && seen["native.on"] == nil)
        }
    }

    @Test(arguments: ["highlight", "gone", "reason", "disabled", "toggle", "label"])
    func aChangedContextOrDisabledActionSendsNothing(change: String) async throws {
        try await withContext { context, connection, model, _ in
            let ref = try #require(try context.screen().actions.first)
            var after = try scene(revision: 2, selected: change == "highlight" ? "b" : "a")
            mutate(&after) { node in
                if change == "gone" { node.properties["actions"] = .array([]) }
                if change == "reason" || change == "disabled" {
                    var actions = SurfaceValues.array(node.properties["actions"])
                    var action = SurfaceValues.object(actions[0])
                    action["disabled"] = change == "reason" ? .string("Choose an entry") : .bool(true)
                    actions[0] = .object(action)
                    node.properties["actions"] = .array(actions)
                }
                if change == "toggle" || change == "label" {
                    var actions = SurfaceValues.array(node.properties["actions"])
                    var action = SurfaceValues.object(actions[0])
                    if change == "toggle" { action["on"] = .bool(true) }
                    else { action["label"] = .string("Different operation") }
                    actions[0] = .object(action)
                    node.properties["actions"] = .array(actions)
                }
            }
            connection.next = after
            try await support.wait { model.session.scene?.revision == 2 }
            await #expect(throws: ChatSurfaceInteraction.Failure.self) {
                try await context.run(ref.id, target: nil, args: ref.args ?? [:])
            }
            #expect(connection.inputs == 0)
        }
    }

    @Test(arguments: ["id", "label"]) func aPrimaryActionChangedSinceTheScreenWasReadIsNotActivated(change: String) async throws {
        try await withContext { context, connection, model, _ in
            connection.next = try scene(revision: 2, primaryLabel: "Choose")
            try await support.wait { model.session.scene?.revision == 2 }
            let args = try #require(try context.screen().picker?.onSelectArgs)
            let after = try scene(revision: 3, primaryLabel: change == "label" ? "Different operation" : "Choose",
                primaryID: change == "id" ? "different" : "confirm")
            connection.next = after
            try await support.wait { model.session.scene?.revision == 3 }
            await #expect(throws: ChatSurfaceInteraction.Failure.self) {
                try await context.run(NativePickerScreen.activate, target: "a", args: args)
            }
            #expect(connection.inputs == 0)
        }
    }

    @Test func selectingARowRechecksItsActionAfterTheHighlightMoves() async throws {
        try await withContext { context, connection, _, _ in
            let ref = try #require(try context.screen().actions.first)
            let highlighted = try scene(revision: 2, selected: "b")
            let applied = try scene(revision: 3, selected: "b", on: true)
            var kinds: [String] = []
            connection.onInput = { [weak connection] event in
                guard case .object(let fields) = event else { return }
                kinds.append(SurfaceValues.string(fields["ev"]) ?? "")
                connection?.next = kinds.count == 1 ? highlighted : applied
            }
            _ = try await context.run(ref.id, target: "b", args: ref.args ?? [:])
            #expect(kinds == ["select", "action"])
            #expect(connection.inputs == 2)
        }
    }

    @Test func sourceFlagsAreScopedToDeclaredNativeActions() {
        let args: [String: JSONValue] = [NativePickerScreen.disabledKey: .string("Not available"), NativePickerScreen.onKey: .bool(true)]
        let original = HarnessActionRef(id: "native.action.expand", label: "Expand", args: args)
        #expect(original.sourceDisabled && original.sourceOn == true)
        let api = HarnessActionRef(id: "api.action", label: "API action", args: args)
        #expect(!api.sourceDisabled && api.sourceOn == nil)
    }

    private func withContext(_ body: (NativeCommandContext, ChatSurfaceInteractionTests.Connection, ChatSurfaceInteraction, HarnessSessionStore) async throws -> Void) async throws {
        let connection = try ChatSurfaceInteractionTests.Connection()
        connection.next = try scene()
        let (store, _) = try await support.makeStore(connection)
        let session = support.makeSession(connection)
        let model = ChatSurfaceInteraction(command: nil, session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: support.options) }
        defer { store.retireInteraction(); store.stop(); run.cancel() }
        try await support.wait { session.canInteract && !model.blocks.isEmpty }
        let block = try #require(model.blocks.last)
        try await body(NativeCommandContext(model: model, block: block, store: store), connection, model, store)
        store.retireInteraction()
        await store.nativeInteractions.stopTask?.value
        await run.value
    }

    private func scene(revision: UInt64 = 1, selected: String = "a", on: Bool = false, primaryLabel: String? = nil, primaryID: String = "confirm") throws -> SurfaceScene {
        var value = try ChatSurfaceInteractionTests.scene(revision: revision, overlay: "omp-native-picker")
        mutate(&value) { node in
            node.properties["items"] = .array(["a", "b"].map { .object(["id": .string($0), "label": .string($0.uppercased())]) })
            node.properties["order"] = .array([.string("a"), .string("b")])
            node.properties["selected"] = .string(selected)
            node.properties["actions"] = .array([
                .object(["id": .string("expand"), "label": .string("Expand"), "on": .bool(on)]),
                .object(["id": .string("close"), "label": .string("Close"), "end": .bool(true)])])
            if let primaryLabel {
                var actions = SurfaceValues.array(node.properties["actions"])
                actions.append(.object(["id": .string(primaryID), "label": .string(primaryLabel), "primary": .bool(true)]))
                node.properties["actions"] = .array(actions)
            }
        }
        return value
    }

    private func mutate(_ scene: inout SurfaceScene, _ body: (inout SurfaceNode) -> Void) {
        func visit(_ node: inout SurfaceNode) {
            if node.kind == "picker" { body(&node); return }
            for i in node.children.indices { visit(&node.children[i]) }
        }
        visit(&scene.views[0].document.root)
    }
}
