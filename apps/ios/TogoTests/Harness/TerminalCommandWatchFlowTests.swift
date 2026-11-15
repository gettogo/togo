import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct TerminalCommandWatchFlowTests {
    private typealias Connection = TerminalNativeInteractionTests.Connection

    private func menu(_ token: String) -> HarnessScreen {
        .init(id: "terminal.menu/" + token, primitive: .picker, title: "Choose", fetchedAt: WireDate(.now),
            picker: .init(items: [.init(id: token + "/0", title: "One")], searchable: false, onSelect: "terminal.select"), interaction: token)
    }

    private func make(_ connection: Connection) async throws -> (HarnessSessionStore, FakeHarnessServices) {
        var descriptor = ChatTests.claude
        descriptor.capabilities.append(HarnessCapability.terminalNative)
        let services = FakeHarnessServices(descriptor: descriptor)
        services.terminalFactory = { connection }
        services.feeds = [.init(elements: [.opened(.init(epoch: "epoch", cursor: 0, items: [], more: false,
            mission: .init(rev: 1, summary: .init(), sections: [])))], end: .hold)]
        let store = HarnessSessionStore(subject: .init(hostID: UUID(), sessionID: "session", capabilities: [Capability.harness]), services: services)
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
