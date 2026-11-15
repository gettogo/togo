import Foundation
import Observation
import Testing
import TogoKit
@testable import Togo

@MainActor struct TerminalCommandWatchFlowTests {
    private typealias Connection = TerminalNativeInteractionTests.Connection

    private func menu(_ token: String) -> HarnessScreen {
        .init(id: "terminal.menu/" + token, primitive: .picker, title: "Choose", fetchedAt: WireDate(.now),
            picker: .init(items: [.init(id: token + "/0", title: "One")], searchable: false, onSelect: "terminal.select"), interaction: token)
    }

    private func make(_ connection: Connection, working: Bool = false, surface: Bool = false) async throws -> (HarnessSessionStore, FakeHarnessServices) {
        var descriptor = ChatTests.claude
        descriptor.capabilities.append(HarnessCapability.terminalNative)
        if surface { descriptor.capabilities.append(HarnessCapability.surface) }
        let services = FakeHarnessServices(descriptor: descriptor)
        services.terminalFactory = { connection }
        services.feeds = [.init(elements: [.opened(.init(epoch: "epoch", cursor: 0, items: [], more: false,
            mission: .init(rev: 1, summary: .init(working: working), sections: [])))], end: .hold)]
        let capabilities: Set<String> = surface ? [Capability.harness, Capability.surface] : [Capability.harness]
        let store = HarnessSessionStore(subject: .init(hostID: UUID(), sessionID: "session", capabilities: capabilities), services: services)
        store.layer = .chat
        store.launch()
        try await until { store.phase == .live }
        return (store, services)
    }

    private func identity(_ params: HarnessActionParams) throws -> UUID {
        guard case .string(let value) = params.args?["requestId"], let id = UUID(uuidString: value) else {
            throw TerminalNativeInteraction.Failure(message: "Missing operation identity")
        }
        return id
    }

    private func note(_ id: UUID) -> HarnessNotification {
        .init(sessionId: "session", kind: .menu, ts: WireDate(.now), epoch: "epoch", requestId: id.uuidString)
    }

    private func question(answerable: Bool = false) throws -> HarnessItem {
        let wire = #"{"id":"ask-native","seq":100,"at":"2026-11-14T10:00:00Z","kind":"question","question":{"questions":[{"id":"memo","prompt":"Memo","options":[],"multi":false,"allowText":true}],"answerable":false}}"#
        var item = try JSONDecoder().decode(HarnessItem.self, from: Data(wire.utf8))
        if answerable, case .question(var value) = item.body {
            value.answerable = true
            item.body = .question(value)
        }
        return item
    }

    private func approval() throws -> HarnessItem {
        // Captured from the delayed OMP select hook; only identities and time are neutralized.
        let wire = #"{"id":"approval-native","seq":100,"at":"2026-11-14T10:00:00Z","kind":"approval","approval":{"requestId":"r_test"}}"#
        return try JSONDecoder().decode(HarnessItem.self, from: Data(wire.utf8))
    }

    @Test func aQuestionArrivalInvalidatesPresentationWithoutAWorkChange() async throws {
        let (store, _) = try await make(Connection())
        defer { store.stop() }
        let changed = Flag()
        withObservationTracking { _ = store.terminalQuestionIDs } onChange: { changed.on = true }
        store.feed.insertSent(try question())
        #expect(changed.on)
        #expect(store.terminalQuestionIDs == ["ask-native"])
    }

    @Test func terminalOnlyApprovalAttachesOnceAcrossFeedUpdates() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("approval")), .init(result: .applied)]
        let (store, _) = try await make(connection)
        defer { store.stop() }
        store.feed.insertSent(try approval())
        let ids = NativeCommandPresentation.terminalApprovals(in: store.feed.timeline.conversation(), card: { _ in nil })
        #expect(ids == ["approval-native"])
        store.attachTerminalQuestion(approvals: ids)
        try await until { store.terminalInteraction?.screen != nil }
        store.updateNativeQuestion()
        store.retireTerminalInteraction()
        await store.nativeInteractions.stopTask?.value
        store.updateNativeQuestion()
        store.attachTerminalQuestion(approvals: ids)
        #expect(store.terminalInteraction == nil)
        #expect(connection.calls.map(\.action) == ["terminal.attach", "terminal.release"])
    }

    @Test(arguments: [false, true])
    func approvalsThatCanBeAnsweredInlineDoNotAttach(allowText: Bool) throws {
        let item = try approval()
        let options: [ApprovalOption] = allowText ? [] : [.init(n: 1, label: "Yes", role: .approve)]
        let card = RequestCard(id: "r_test", hostID: Samples.studio, sessionID: "session", bib: "01",
            agentName: "OMP", title: "Choose", detail: nil, options: options, allowText: allowText, phase: .open)
        #expect(NativeCommandPresentation.terminalApprovals(in: [item], card: { _ in card }).isEmpty)
    }

    @Test(arguments: [false, true])
    func unansweredQuestionAttachesWithoutACommandEvenWhileWorking(working: Bool) async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("question")), .init(result: .applied)]
        let (store, _) = try await make(connection, working: working)
        defer { store.stop() }
        store.feed.insertSent(try question())
        store.updateNativeQuestion()
        #expect(store.terminalQuestionIDs == ["ask-native"])
        store.attachTerminalQuestion()
        try await until { store.terminalInteraction?.screen != nil }
        #expect(connection.calls.map(\.action) == ["terminal.attach"])
        store.retireTerminalInteraction()
        await store.nativeInteractions.stopTask?.value
        store.attachTerminalQuestion()
        #expect(store.terminalInteraction == nil)
        #expect(connection.calls.map(\.action) == ["terminal.attach", "terminal.release"])
    }

    @Test func questionSupersedesACommandWatchAndFallbackIsNotRetried() async throws {
        let connection = Connection()
        connection.results = [.init(result: .fallback)]
        let (store, _) = try await make(connection)
        defer { store.stop() }
        let id = UUID()
        store.nativeInteractions.terminalWatch.begin(requestId: id, conversation: store.conversation, epoch: "epoch")
        store.nativeInteractions.terminalWatch.finish(requestId: id, submitted: true)
        store.feed.insertSent(try question())
        store.attachTerminalQuestion()
        try await until { connection.closes == 1 }
        store.attachTerminalQuestion()
        #expect(connection.calls.map(\.action) == ["terminal.attach"])
        #expect(store.terminalInteraction == nil)
        #expect(store.terminalQuestionIDs == ["ask-native"])
        #expect(!store.nativeInteractions.terminalWatch.observing(conversation: store.conversation, epoch: "epoch"))
    }

    @Test func hiddenChatDefersAndStopBeforeDispatchDoesNotAttach() async throws {
        let connection = Connection()
        let (store, _) = try await make(connection)
        store.feed.insertSent(try question())
        store.layer = .terminal
        store.attachTerminalQuestion()
        #expect(store.terminalInteraction == nil)
        store.layer = .chat
        store.attachTerminalQuestion()
        #expect(store.terminalInteraction != nil)
        store.stop()
        await store.nativeInteractions.stopTask?.value
        #expect(connection.calls.isEmpty)
    }

    @Test func existingMenuOwnsItsQuestionAndDoesNotReopenAfterHandback() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("open")), .init(result: .applied)]
        let (store, _) = try await make(connection)
        defer { store.stop() }
        try await store.runTerminalCommand("/models")
        store.feed.insertSent(try question())
        store.updateNativeQuestion()
        store.retireTerminalInteraction()
        await store.nativeInteractions.stopTask?.value
        store.attachTerminalQuestion()
        #expect(connection.calls.map(\.action) == ["terminal.command", "terminal.release"])
        #expect(store.terminalInteraction == nil)
    }

    @Test(arguments: [false, true])
    func answerableQuestionsAndSurfaceSessionsKeepTheirExistingRoute(surface: Bool) async throws {
        let connection = Connection()
        let (store, _) = try await make(connection, surface: surface)
        defer { store.stop() }
        store.feed.insertSent(try question(answerable: !surface))
        store.attachTerminalQuestion()
        #expect(store.terminalInteraction == nil)
        #expect(connection.calls.isEmpty)
    }

    @Test func earlyMenuWaitsForReceiptThenAttachesOnceWithoutRepeatingTheCommand() async throws {
        let command = Connection(), attached = Connection()
        let (store, services) = try await make(command)
        defer { store.stop(); command.handle = nil }
        var request: UUID?
        command.handle = { params in
            let id = try identity(params)
            request = id
            #expect(store.applyNativeNotification(note(id)) == .applied)
            #expect(store.readyTerminalMenu == nil && !store.canAttachTerminalMenu)
            _ = store.queue.add("queued words")
            return .init(result: .typed)
        }
        try await store.runTerminalCommand("/models")
        let id = try #require(request)
        #expect(store.terminalInteraction == nil && store.readyTerminalMenu == id)
        #expect(store.notes.last?.text == PrimitiveCopy.sent)
        #expect(services.sent.isEmpty && !store.queue.isEmpty)
        #expect(store.applyNativeNotification(note(id)) == .ignored)
        attached.results = [.init(result: .applied, screen: menu("late")), .init(result: .applied)]
        services.terminalFactory = { attached }
        await store.attachReadyTerminalMenu(id)
        #expect(store.terminalInteraction?.screen?.interaction == "late")
        await store.attachReadyTerminalMenu(id)
        #expect(attached.calls.map(\.action) == ["terminal.attach"])
        #expect(command.calls.map(\.action) == ["terminal.command"])
    }

    @Test func terminalHandbackAndStopDiscardLateMenuInterest() async throws {
        let connection = Connection()
        connection.results = [.init(result: .typed)]
        let (store, _) = try await make(connection)
        defer { store.stop() }
        try await store.runTerminalCommand("/models")
        let id = try identity(#require(connection.calls.first))
        store.layer = .terminal
        #expect(store.applyNativeNotification(note(id)) == .ignored)
        store.layer = .chat
        await store.attachReadyTerminalMenu(id)
        #expect(connection.calls.count == 1)
        store.nativeInteractions.terminalWatch.begin(requestId: id, conversation: store.conversation, epoch: "epoch")
        store.stop()
        #expect(store.applyNativeNotification(note(id)) == .ignored)
    }

    @Test func explicitNextInputCancelsTheEarlierAttempt() async throws {
        let connection = Connection()
        connection.results = [.init(result: .typed)]
        let (store, _) = try await make(connection)
        defer { store.stop() }
        try await store.runTerminalCommand("/models")
        let id = try identity(#require(connection.calls.first))
        try await store.prepareHarnessInput()
        #expect(store.applyNativeNotification(note(id)) == .ignored)
        #expect(store.readyTerminalMenu == nil)
    }

    @Test func eachAttemptHasItsOwnIdentityAndTypedNeverConfirmsCompletion() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("first")), .init(result: .typed)]
        let model = TerminalNativeInteraction(sessionID: "session", pollInterval: .seconds(1000)) { connection }
        var began: [UUID] = [], receipts: [HarnessActionResult.Outcome] = []
        model.onDispatch = { began.append($0) }
        model.onReceipt = { _, result in if let result { receipts.append(result.result) } }
        _ = try await model.start("/models")
        _ = await model.run("terminal.select", target: "first/0", args: nil)
        #expect(began.count == 2 && Set(began).count == 2)
        #expect(receipts == [.applied, .typed])
        #expect(model.phase == .ended && !model.confirmedEnd)
        #expect(connection.calls.map(\.action) == ["terminal.command", "terminal.select"])
        #expect(connection.closes == 1)
    }

    private func until(_ predicate: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(2)
        while !predicate(), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        try #require(predicate())
    }
}
