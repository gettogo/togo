import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct ChatSurfaceInteractionTests {
    @MainActor final class Connection: SurfaceConnection {
        var next: SurfaceScene?
        var inputs = 0
        var queryGate: AsyncStream<Void>?
        var onInput: ((JSONValue) -> Void)?
        var actions: [String] = []
        var closed = false
        var closing = false
        var failInput = false
        var refuseInput = false
        var holdClose = false
        var closeContinuation: CheckedContinuation<Void, Never>?
        let lease: SurfaceLease

        init(features: [String] = []) throws {
            lease = try JSONDecoder().decode(SurfaceLease.self, from: Data("""
                {"id":"s_abcdefghij","token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","features":\(String(decoding: try JSONEncoder().encode(features), as: UTF8.self))}
                """.utf8))
            next = try ChatSurfaceInteractionTests.scene()
        }
        func surfaceOpen(id: String, options: SurfaceOptions) async throws -> SurfaceLease { lease }
        func surfaceSnapshot(_ lease: SurfaceLease, revision: UInt64, kinds: Set<String>) async throws -> SurfaceScene? {
            defer { next = nil }
            return next
        }
        func surfaceInput(_ lease: SurfaceLease, surface: String, instance: UInt64, event: JSONValue, expectedText: String?) async throws -> SurfaceDispatchResult {
            inputs += 1
            var action = "send"
            if case .object(let fields) = event, case .string(let act)? = fields["act"] { action = act }
            actions.append(action)
            if action == "togo.query", let queryGate { for await _ in queryGate { break } }
            onInput?(event)
            if failInput { throw HelperError.closed }
            return SurfaceDispatchResult(dispatched: true, consumed: refuseInput ? nil : true, refused: refuseInput ? true : nil)
        }
        func surfaceAck(_ lease: SurfaceLease, view: SurfaceView) async throws {}
        func surfaceClose(_ lease: SurfaceLease) async throws {}
        func close() async {
            closing = true
            if holdClose { await withCheckedContinuation { closeContinuation = $0 } }
            closed = true
        }
    }

    @Test func delayedPickerSurvivesAcceptanceAndIntermediatePlainFrames() async throws {
        let connection = try Connection()
        let session = makeSession(connection)
        var finishes = 0
        let model = ChatSurfaceInteraction(command: "/resume", session: session) { finishes += 1 }
        let task = Task { await model.run(options: options) }
        try await wait { session.canInteract }
        try await model.submit()
        #expect(session.pending == nil)
        #expect(model.submitted)
        model.changed()
        #expect(finishes == 0)
        connection.next = try Self.scene(revision: 2)
        try await wait { session.scene?.revision == 2 }
        model.changed()
        #expect(finishes == 0)
        connection.next = try Self.scene(revision: 3, overlay: "omp-native-picker")
        try await wait { session.scene?.revision == 3 }
        model.changed()
        #expect(model.presented)
        #expect(!model.blocks.isEmpty)
        #expect(finishes == 0)
        connection.next = try Self.scene(revision: 4)
        try await wait { session.scene?.revision == 4 }
        model.changed()
        model.changed()
        #expect(finishes == 1)
        #expect(connection.inputs == 1)
        task.cancel()
        await task.value
    }

    @Test func nonemptyDesktopDraftIsNeverReplaced() async throws {
        let connection = try Connection()
        connection.next = try Self.scene(text: "desktop draft")
        let session = makeSession(connection)
        let model = ChatSurfaceInteraction(command: "/resume", session: session) {}
        let task = Task { await model.run(options: options) }
        try await wait { session.canInteract }
        await #expect(throws: ChatSurfaceInteraction.Failure.self) { try await model.submit() }
        #expect(connection.inputs == 0)
        #expect(!model.submitted)
        task.cancel()
        await task.value
    }

    @Test func failedSubmissionReleasesBeforeRetry() async throws {
        let connection = try Connection()
        connection.failInput = true
        let (store, _) = try await makeStore(connection)
        defer { store.stop() }
        let send = Task { try await store.runNativeCommand("/resume") }
        try await wait { store.interaction != nil }
        let model = try #require(store.interaction)
        let run = Task { await model.run(options: options) }
        await #expect(throws: ChatSurfaceInteraction.Failure.self) { try await send.value }
        #expect(store.interaction == nil)
        #expect(!store.nativeInteractions.stopping)
        #expect(connection.closed)
        #expect(connection.inputs == 1)
        await run.value
        connection.failInput = false
        connection.next = try Self.scene()
        let retry = Task { try await store.runNativeCommand("/resume") }
        try await wait { store.interaction != nil }
        let replacement = try #require(store.interaction)
        let replacementRun = Task { await replacement.run(options: options) }
        try await retry.value
        #expect(connection.inputs == 2)
        store.retireInteraction()
        await store.nativeInteractions.stopTask?.value
        await replacementRun.value
    }

    @Test func nextPromptWaitsForLeaseCloseAndRealFormsRefuseIt() async throws {
        let connection = try Connection()
        let (store, services) = try await makeStore(connection)
        defer { store.stop() }
        let send = Task { try await store.runNativeCommand("/resume") }
        try await wait { store.interaction != nil }
        let model = try #require(store.interaction)
        let run = Task { await model.run(options: options) }
        try await send.value
        connection.holdClose = true
        let prompt = Task { try await store.send("next prompt", mode: .now) }
        try await wait { connection.closeContinuation != nil }
        #expect(services.sent.isEmpty)
        #expect(store.nativeInteractions.stopping)
        connection.closeContinuation?.resume()
        connection.closeContinuation = nil
        try await prompt.value
        #expect(connection.closed)
        #expect(services.sent.map(\.text) == ["next prompt"])
        await run.value

        connection.holdClose = false
        connection.next = try Self.scene(overlay: "omp-native-form")
        let formSession = makeSession(connection)
        let form = ChatSurfaceInteraction(command: nil, session: formSession) {}
        store.interaction = form
        let formRun = Task { await form.run(options: options) }
        try await wait { !form.blocks.isEmpty }
        await #expect(throws: ChatSurfaceInteraction.Failure.self) { try await store.send("do not overwrite", mode: .now) }
        await #expect(throws: ChatSurfaceInteraction.Failure.self) { try await store.run(action: "mode") }
        #expect(services.actions.isEmpty)
        #expect(store.interaction === form)
        #expect(services.sent.count == 1)
        store.retireInteraction()
        await store.nativeInteractions.stopTask?.value
        await formRun.value
    }

    @Test func actionWaitsForTheWaitingInteractionToRelease() async throws {
        let connection = try Connection()
        let (store, services) = try await makeStore(connection)
        defer { store.stop() }
        let session = makeSession(connection)
        let model = ChatSurfaceInteraction(command: nil, session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: options) }
        try await wait { session.canInteract }
        connection.holdClose = true
        let action = Task { try await store.run(action: "mode") }
        try await wait { connection.closeContinuation != nil }
        #expect(services.actions.isEmpty)
        #expect(store.nativeInteractions.stopping)
        connection.closeContinuation?.resume()
        connection.closeContinuation = nil
        _ = try await action.value
        #expect(services.actions == ["mode"])
        #expect(store.interaction == nil)
        await run.value
    }

    @Test func refusedPickerSearchKeepsItsScreenAndReadsTheOriginalQuery() async throws {
        let connection = try Connection()
        let (store, _) = try await makeStore(connection)
        defer { store.stop() }
        let session = makeSession(connection)
        let model = ChatSurfaceInteraction(command: "/resume", session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: options) }
        try await wait { session.canInteract }
        try await model.submit()
        connection.refuseInput = true
        connection.next = try Self.scene(revision: 2, overlay: "omp-native-picker")
        try await wait { session.scene?.revision == 2 && !model.blocks.isEmpty }
        let block = try #require(model.blocks.last)
        let context = NativeCommandContext(model: model, block: block, store: store)
        let search = try #require(context.primitive().searchScreen)
        await #expect(throws: ChatSurfaceInteraction.Failure.self) { try await search("phone search") }
        #expect(!model.retired)
        #expect(store.interaction === model)
        #expect(session.canInteract)
        #expect(session.notice == nil)
        var next = try Self.scene(revision: 3, overlay: "omp-native-picker")
        next.views[0].document.root.children[1].children[0].properties["query"] = .string("desk search")
        connection.next = next
        try await wait { session.scene?.revision == 3 }
        #expect(try context.screen().picker?.query == "desk search")
        #expect(connection.inputs == 2)
        run.cancel()
        await run.value
    }

    @Test func controlsCannotMapBeforeTheSubmissionIsClassified() async throws {
        let connection = try Connection()
        connection.next = try Self.scene(overlay: "omp-native-picker")
        let (store, _) = try await makeStore(connection)
        defer { store.stop() }
        let session = makeSession(connection)
        let model = ChatSurfaceInteraction(command: "/resume", session: session) {}
        let run = Task { await model.run(options: options) }
        try await wait { session.canInteract && !model.blocks.isEmpty }
        #expect(!model.controlsReady)
        let block = try #require(model.blocks.last)
        let context = NativeCommandContext(model: model, block: block, store: store)
        #expect(throws: ChatSurfaceInteraction.Failure.self) { try context.screen() }
        #expect(connection.inputs == 0)
        run.cancel()
        await run.value
    }

    @Test func existingControlsAreKeptWithoutSendingOrBorrowingTheNewWorkflow() async throws {
        let connection = try Connection()
        connection.next = try Self.scene(overlay: "omp-native-picker")
        let (store, _) = try await makeStore(connection)
        defer { store.stop() }
        let send = Task { try await store.runNativeCommand("/resume") }
        try await wait { store.interaction != nil }
        let model = try #require(store.interaction)
        let run = Task { await model.run(options: options) }
        await #expect(throws: ChatSurfaceInteraction.Failure.self) { try await send.value }
        #expect(store.interaction === model)
        #expect(model.attachedExisting)
        #expect(!model.submitted)
        #expect(!model.retired)
        #expect(model.workflow == "existing")
        #expect(model.title == SurfaceCopy.currentScreen)
        #expect(connection.inputs == 0)
        #expect(!connection.closed)
        connection.next = try Self.scene(revision: 2)
        try await wait { model.session.scene?.revision == 2 }
        model.changed()
        await store.nativeInteractions.stopTask?.value
        await run.value
        #expect(store.interaction == nil)
        #expect(model.retired)
        #expect(connection.closed)
        #expect(connection.inputs == 0)
    }

    @Test func closeWaitsForPendingSearchBeforeEndingTheOriginalPicker() async throws {
        let connection = try Connection()
        let (store, _) = try await makeStore(connection)
        defer { store.stop() }
        let session = makeSession(connection)
        let model = ChatSurfaceInteraction(command: "/resume", session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: options) }
        try await wait { session.canInteract }
        try await model.submit()
        connection.next = try Self.scene(revision: 2, overlay: "omp-native-picker")
        try await wait { !model.blocks.isEmpty }
        let (gate, continuation) = AsyncStream<Void>.makeStream()
        defer { continuation.finish() }
        connection.queryGate = gate
        var afterQuery = try Self.scene(revision: 3, overlay: "omp-native-picker")
        afterQuery.views[0].document.root.children[1].children[0].properties["query"] = .string("phone search")
        let afterClose = try Self.scene(revision: 4)
        connection.onInput = { [weak connection] event in
            guard case .object(let fields) = event, case .string(let action)? = fields["act"] else { return }
            if action == "togo.query" { connection?.next = afterQuery }
            else if action == "close" { connection?.next = afterClose }
        }
        let block = try #require(model.blocks.last)
        let search = try #require(NativeCommandContext(model: model, block: block, store: store).primitive().searchScreen)
        let query = Task { try await search("phone search") }
        try await wait { connection.inputs == 2 }
        store.dismissNativeCommand()
        try await Task.sleep(for: .milliseconds(20))
        #expect(!model.retired)
        #expect(!connection.closed)
        #expect(connection.actions == ["send", "togo.query"])
        continuation.yield(())
        continuation.finish()
        _ = try? await query.value
        try await wait { model.retired }
        await store.nativeInteractions.stopTask?.value
        await run.value
        #expect(connection.actions == ["send", "togo.query", "close"])
        #expect(connection.closed)
    }

    @Test func closingRecordedDockInputSendsItsOriginalCancelButtonOnce() async throws {
        let connection = try Connection()
        connection.next = try Self.scene(overlay: "omp-native-hook-input")
        let (store, _) = try await makeStore(connection)
        defer { store.stop() }
        let session = makeSession(connection)
        let model = ChatSurfaceInteraction(command: nil, session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: options) }
        try await wait { session.canInteract && !model.blocks.isEmpty }
        #expect(model.title == "Neutral entry check")
        let block = try #require(model.blocks.last)
        let screen = try NativeCommandContext(model: model, block: block, store: store).screen()
        #expect(screen.settings?.sections.flatMap(\.fields).first?.kind == .secret)
        var seen: [String: JSONValue] = [:]
        let afterClose = try Self.scene(revision: 2)
        connection.onInput = { [weak connection] event in
            if case .object(let fields) = event { seen = fields }
            connection?.next = afterClose
        }
        store.dismissNativeCommand()
        try await wait { model.retired }
        await store.nativeInteractions.stopTask?.value
        await run.value
        #expect(connection.actions == ["cancel"])
        #expect(seen["id"] == .string("s.actions/cancel"))
        #expect(seen["ev"] == .string("action"))
        #expect(seen["act"] == .string("cancel"))
        #expect(connection.closed)
    }

    @Test func stoppingStoreRetiresTheNativeLeaseWithoutPumping() async throws {
        let connection = try Connection()
        let (store, services) = try await makeStore(connection)
        let session = makeSession(connection)
        let model = ChatSurfaceInteraction(command: nil, session: session) {}
        store.interaction = model
        let run = Task { await model.run(options: options) }
        try await wait { session.canInteract }
        store.stop()
        await store.nativeInteractions.stopTask?.value
        await run.value
        #expect(model.retired)
        #expect(store.interaction == nil)
        #expect(connection.closed)
        #expect(services.sent.isEmpty)
    }

    @Test func closedFormReopensOnlyOnExplicitRequest() async throws {
        let connection = try Connection()
        let (store, services) = try await makeStore(connection)
        defer { store.stop() }
        let wire = #"{"id":"ask-native","seq":100,"at":"2026-11-14T10:00:00Z","kind":"question","question":{"questions":[{"id":"memo","prompt":"Memo","options":[],"multi":false,"allowText":true}],"answerable":false}}"#
        let item = try JSONDecoder().decode(HarnessItem.self, from: Data(wire.utf8))
        store.feed.insertSent(item)
        store.updateNativeQuestion()
        #expect(store.interaction?.requestID == item.id)
        store.retireInteraction()
        await store.nativeInteractions.stopTask?.value
        store.updateNativeQuestion()
        #expect(store.interaction == nil)
        #expect(store.canOpenNativeQuestion(item))
        store.openNativeQuestion(item.id)
        #expect(store.interaction?.requestID == item.id)
        #expect(store.interaction?.command == nil)
        #expect(connection.inputs == 0)
        #expect(services.sent.isEmpty)
    }

    func makeSession(_ connection: Connection) -> SurfaceSession {
        SurfaceSession(id: connection.lease.id, makeConnection: { connection }, pollInterval: .milliseconds(5))
    }

    func makeStore(_ connection: Connection) async throws -> (HarnessSessionStore, FakeHarnessServices) {
        var descriptor = ChatTests.claude
        descriptor.capabilities.append(HarnessCapability.surface)
        let services = FakeHarnessServices(descriptor: descriptor)
        services.nativeConnection = connection
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        var subject = HarnessSubject(hostID: UUID(), sessionID: connection.lease.id, capabilities: [Capability.harness])
        subject.capabilities.insert(Capability.surface)
        let store = HarnessSessionStore(subject: subject, services: services)
        store.launch()
        try await wait { store.phase == .live }
        #expect(!store.working)
        return (store, services)
    }

    var options: SurfaceOptions {
        SurfaceOptions(kinds: SurfaceNodeView.kinds.sorted(), cols: 80, dark: true, reduceMotion: false)
    }

    static func scene(revision: UInt64 = 1, text: String = "", overlay: String? = nil) throws -> SurfaceScene {
        func fixture(_ name: String) throws -> [String: Any] {
            let url = try #require(Bundle(for: ChatSurfaceFixtureBundle.self).url(forResource: name, withExtension: "json"))
            return try #require(JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any])
        }
        var editor = try fixture("omp-native-editor")
        var properties = try #require(editor["p"] as? [String: Any])
        properties["text"] = text
        editor["p"] = properties
        let editorID = try #require(editor["id"] as? String)
        var children: [[String: Any]] = overlay == "omp-native-select-overlay" ? [] : [editor]
        if let overlay {
            let original = try fixture(overlay)
            if overlay == "omp-native-picker" {
                children.append(["id": "layer", "k": "col", "c": [original]])
            } else if ["omp-native-hook-input", "omp-native-select-overlay"].contains(overlay) {
                children.append(["id": "dock", "k": "col", "c": [original]])
            } else { children.append(original) }
        }
        let focus: Any = overlay == "omp-native-select-overlay" ? NSNull() : editorID
        let value: [String: Any] = [
            "program": "omp", "features": ["send", "edit"], "revision": revision,
            "options": ["kinds": SurfaceNodeView.kinds.sorted(), "cols": 80, "dark": true, "reduceMotion": false],
            "views": [["id": "s:1", "mode": "inline", "listen": true, "closed": false, "instance": 1,
                       "document": ["root": ["id": "s:1", "k": "col", "c": children], "focus": focus,
                                    "sequence": revision, "revision": revision, "suspended": false]]]
        ]
        return try SurfaceScene.decode(JSONSerialization.data(withJSONObject: value), kinds: SurfaceNodeView.kinds)
    }

    func wait(timeout: Duration = .seconds(3), _ condition: @MainActor () -> Bool) async throws {
        let deadline = ContinuousClock.now + timeout
        while !condition() {
            guard ContinuousClock.now < deadline else { throw SurfaceProtocolError.invalidSnapshot }
            try await Task.sleep(for: .milliseconds(5))
        }
    }
}

private final class ChatSurfaceFixtureBundle {}
