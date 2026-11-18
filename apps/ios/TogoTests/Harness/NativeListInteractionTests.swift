import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct NativeListInteractionTests {
    private let support = ChatSurfaceInteractionTests()

    @Test func recordedListKeepsItsOuterDialogAndChoosesWithoutPreview() async throws {
        let connection = try ChatSurfaceInteractionTests.Connection()
        connection.next = try ChatSurfaceInteractionTests.scene(overlay: "omp-native-select-overlay")
        let (store, _) = try await support.makeStore(connection)
        defer { store.stop() }
        let session = support.makeSession(connection)
        let model = ChatSurfaceInteraction(command: nil, session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: support.options) }
        try await support.wait { session.canInteract && !model.blocks.isEmpty }
        let block = try #require(model.blocks.last)
        #expect(block.node.properties["head"] == .string("Debug Tools"))
        let context = NativeCommandContext(model: model, block: block, store: store)
        let picker = try #require(try context.screen().picker)
        let item = try #require(picker.items.first)
        let list = try #require(NativeListScreen.list(in: block.node))
        #expect(picker.items.count == 13)
        #expect(picker.selected == nil) // Source selection is focus, not a saved answer.
        #expect(picker.selectionBehavior != "preview")
        #expect(context.primitive().preview == nil)
        #expect(PickerModel(picker).tap(item, in: picker) == .select)
        var event: [String: JSONValue] = [:]
        let after = try ChatSurfaceInteractionTests.scene(revision: 2)
        connection.onInput = { [weak connection] value in
            if case .object(let fields) = value { event = fields }
            connection?.next = after
        }
        _ = try await context.run(NativeListScreen.choose, target: item.id, args: [:])
        #expect(connection.inputs == 1)
        #expect(event["ev"] == .string("select"))
        #expect(event["id"] == .string(list.id))
        #expect(event["item"] == .string(item.id))
        await #expect(throws: ChatSurfaceInteraction.Failure.self) {
            try await context.run(NativeListScreen.choose, target: item.id, args: [:])
        }
        #expect(connection.inputs == 1)
        store.retireInteraction()
        await store.nativeInteractions.stopTask?.value
        await run.value
    }

    @Test func closeUsesOneFeatureGatedEscapeAndObservesDeparture() async throws {
        let connection = try ChatSurfaceInteractionTests.Connection(features: ["escape"])
        connection.next = try ChatSurfaceInteractionTests.scene(overlay: "omp-native-select-overlay")
        let (store, _) = try await support.makeStore(connection)
        defer { store.stop() }
        let session = support.makeSession(connection)
        let model = ChatSurfaceInteraction(command: nil, session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: support.options) }
        try await support.wait { session.canInteract && !model.blocks.isEmpty }
        let target = try #require(model.blocks.last?.node.id)
        var event: [String: JSONValue] = [:]
        let after = try ChatSurfaceInteractionTests.scene(revision: 2)
        connection.onInput = { [weak connection] value in
            if case .object(let fields) = value { event = fields }
            connection?.next = after
        }
        store.cancelNativeCommand()
        try await support.wait { model.retired }
        await store.nativeInteractions.stopTask?.value
        await run.value
        #expect(connection.actions == ["togo.escape"])
        #expect(event["id"] == .string(target))
        #expect(event["within"] == nil) // Only the helper names the allowed subtree.
        #expect(connection.closed)
    }

    @Test(arguments: [false, true]) func unavailableOrRefusedEscapeKeepsTerminalRecovery(refused: Bool) async throws {
        let connection = try ChatSurfaceInteractionTests.Connection(features: refused ? ["escape"] : [])
        connection.refuseInput = refused
        connection.next = try ChatSurfaceInteractionTests.scene(overlay: "omp-native-select-overlay")
        let (store, _) = try await support.makeStore(connection)
        defer { store.stop() }
        let session = support.makeSession(connection)
        let model = ChatSurfaceInteraction(command: nil, session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: support.options) }
        try await support.wait { session.canInteract && !model.blocks.isEmpty }
        store.cancelNativeCommand()
        try await support.wait { model.closeFailure != nil }
        #expect(store.interaction === model)
        #expect(!model.retired && !connection.closed)
        #expect(connection.inputs == (refused ? 1 : 0))
        store.retireInteraction()
        await store.nativeInteractions.stopTask?.value
        await run.value
    }

    @Test func nestedDeclaredEndRunsOnItsOwnNodeWithoutEscape() async throws {
        let connection = try ChatSurfaceInteractionTests.Connection(features: ["escape"])
        var scene = try ChatSurfaceInteractionTests.scene(overlay: "omp-native-select-overlay")
        func addEnd(_ node: inout SurfaceNode) {
            if node.kind == "list" {
                node.properties["actions"] = .array([.object(["id": .string("close"), "end": .bool(true)])])
            }
            for i in node.children.indices { addEnd(&node.children[i]) }
        }
        addEnd(&scene.views[0].document.root)
        connection.next = scene
        let (store, _) = try await support.makeStore(connection)
        defer { store.stop() }
        let session = support.makeSession(connection)
        let model = ChatSurfaceInteraction(command: nil, session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: support.options) }
        try await support.wait { session.canInteract && !model.blocks.isEmpty }
        let block = try #require(model.blocks.last)
        let list = try #require(NativeListScreen.list(in: block.node))
        var target: JSONValue?
        let after = try ChatSurfaceInteractionTests.scene(revision: 2)
        connection.onInput = { [weak connection] value in
            if case .object(let fields) = value { target = fields["id"] }
            connection?.next = after
        }
        store.cancelNativeCommand()
        try await support.wait { model.retired }
        await store.nativeInteractions.stopTask?.value
        await run.value
        #expect(connection.actions == ["close"])
        #expect(target == .string(list.id))
    }

    @Test func changedLabelDoesNotCountAsAClosedDialog() async throws {
        let connection = try ChatSurfaceInteractionTests.Connection(features: ["escape"])
        connection.next = try ChatSurfaceInteractionTests.scene(overlay: "omp-native-select-overlay")
        let (store, _) = try await support.makeStore(connection)
        defer { store.stop() }
        let session = support.makeSession(connection)
        let model = ChatSurfaceInteraction(command: nil, session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: support.options) }
        try await support.wait { session.canInteract && !model.blocks.isEmpty }
        var after = try ChatSurfaceInteractionTests.scene(revision: 2, overlay: "omp-native-select-overlay")
        after.views[0].document.root.children[0].children[0].properties["head"] = .string("Still open")
        connection.onInput = { [weak connection] _ in connection?.next = after }
        store.cancelNativeCommand()
        try await support.wait(timeout: .seconds(7)) { model.closeFailure != nil }
        #expect(!model.retired && !connection.closed)
        #expect(connection.actions == ["togo.escape"])
        store.retireInteraction()
        await store.nativeInteractions.stopTask?.value
        await run.value
    }
    @Test func aCloseThatOpensAnotherDialogKeepsTheNativeInteraction() async throws {
        let connection = try ChatSurfaceInteractionTests.Connection(features: ["escape"])
        connection.next = try ChatSurfaceInteractionTests.scene(overlay: "omp-native-select-overlay")
        let (store, _) = try await support.makeStore(connection)
        defer { store.stop() }
        let session = support.makeSession(connection)
        let model = ChatSurfaceInteraction(command: nil, session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: support.options) }
        try await support.wait { session.canInteract && !model.blocks.isEmpty }
        let after = try ChatSurfaceInteractionTests.scene(revision: 2, overlay: "omp-native-hook-input")
        connection.onInput = { [weak connection] _ in connection?.next = after }
        store.cancelNativeCommand()
        try await support.wait { session.scene?.revision == 2 && store.nativeInteractions.dismissing == nil }
        #expect(store.interaction === model)
        #expect(!model.retired && !connection.closed)
        #expect(model.closeFailure == nil)
        #expect(model.title == "Neutral entry check")
        #expect(connection.actions == ["togo.escape"])
        store.retireInteraction()
        await store.nativeInteractions.stopTask?.value
        await run.value
    }

}
