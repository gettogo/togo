import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct TerminalNativeInteractionTests {
    @MainActor final class Connection: TerminalNativeConnection {
        var results: [HarnessActionResult] = []
        var readResult: Result<HarnessScreen, HelperError> = .failure(.closed)
        var readResults: [Result<HarnessScreen, HelperError>] = []
        var handleRead: (@MainActor ([String: String]?) async throws -> HarnessScreen)?
        var actionFailure: HelperError?
        var handle: (@MainActor (HarnessActionParams) async throws -> HarnessActionResult)?
        var calls: [HarnessActionParams] = []
        var reads: [[String: String]?] = []
        var closes = 0

        func harnessAction(_ params: HarnessActionParams) async throws -> HarnessActionResult {
            calls.append(params)
            if let actionFailure { throw actionFailure }
            if let handle { return try await handle(params) }
            guard !results.isEmpty else { throw HelperError.malformedResponse }
            return results.removeFirst()
        }

        func harnessScreen(id: String, screen: String, query: [String: String]?) async throws -> HarnessScreen {
            reads.append(query)
            if let handleRead { return try await handleRead(query) }
            if !readResults.isEmpty { return try readResults.removeFirst().get() }
            return try readResult.get()
        }

        func close() async { closes += 1 }
    }

    private func menu(_ token: String, title: String = "Select model", query: String? = "") -> HarnessScreen {
        .init(id: "terminal.menu/" + token, primitive: .picker, title: title, fetchedAt: WireDate(.now),
            picker: .init(items: [.init(id: token + "/0", title: "DeepSeek"), .init(id: token + "/1", title: "High")],
                selected: token + "/0", searchable: true, onSelect: "terminal.select", searchMode: "remote",
                onSelectArgs: ["interaction": .string(token)], query: query), interaction: token)
    }

    private func model(_ connection: Connection) -> TerminalNativeInteraction {
        TerminalNativeInteraction(sessionID: "session", pollInterval: .seconds(1000)) { connection }
    }

    @Test func nextPickerChangesTokenAndCompletionNeverSendsEscape() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("models")),
            .init(result: .applied, screen: menu("variants", title: "Select variant")), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.start("/models")
        #expect(model.screen?.picker?.selectionBehavior == "reselect")
        _ = await model.run("terminal.select", target: "models/0", args: ["interaction": .string("models")])
        #expect(model.phase == .active)
        #expect(model.screen?.interaction == "variants")
        #expect(connection.closes == 0)
        _ = await model.run("terminal.select", target: "variants/1", args: ["interaction": .string("variants")])
        #expect(model.phase == .ended && model.confirmedEnd)
        #expect(connection.closes == 1)
        await model.close(cancel: true)
        #expect(connection.calls.map(\.action) == ["terminal.command", "terminal.select", "terminal.select"])
    }

    @Test func releaseHandsBackWithoutCancelAndDoesNotPumpAsCompletion() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a")), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.start("/models")
        await model.close(cancel: false)
        #expect(!model.confirmedEnd)
        #expect(connection.calls.map(\.action) == ["terminal.command", "terminal.release"])
        #expect(connection.closes == 1)
    }

    @Test func attachmentClaimsExistingMenuWithoutTypingACommand() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a")), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.attach()
        #expect(connection.calls.first?.action == "terminal.attach")
        #expect(connection.calls.first?.args == nil)
        await model.close(cancel: false)
    }

    @Test func ordinaryErrorWordsDoNotPretendTheMenuClosed() {
        let error = HelperError.rpc(.init(code: RPCErrorCode.refused, message: "closed moved",
            data: .object(["reason": .string("fallback"), "detail": .string("not-a-menu")])) )
        #expect(TerminalNativeInteraction.endReason(error) == nil)
    }

    @Test func cancelMustBeConfirmedAndIsNotRetriedAutomatically() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a")),
            .init(result: .failed, message: "Not confirmed"), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.start("/models")
        #expect(await model.close(cancel: true) == false)
        #expect(model.phase == .ended && connection.closes == 1)
        #expect(connection.calls.count == 2)
        #expect(await model.close(cancel: true) == true)
        #expect(!model.confirmedEnd)
        #expect(connection.calls.map(\.action) == ["terminal.command", "terminal.cancel"])
    }

    @Test func oldPanelActionCannotDriveTheNextPanel() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a")), .init(result: .applied, screen: menu("b")), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.start("/models")
        _ = await model.run("terminal.select", target: "a/0", args: ["interaction": .string("a")])
        let result = await model.run("terminal.select", target: "a/0", args: ["interaction": .string("a")])
        #expect(result?.result == .failed)
        #expect(connection.calls.count == 2)
        await model.close(cancel: false)
    }

    @Test func pollingCannotSilentlyTakeANewToken() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a")), .init(result: .applied)]
        connection.readResult = .success(menu("b"))
        let model = model(connection)
        _ = try await model.start("/models")
        await #expect(throws: (any Error).self) { _ = try await model.read() }
        #expect(model.screen?.interaction == "a")
        await model.close(cancel: false)
    }

    @Test func movedReadEndsWithoutAnyFurtherKey() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a"))]
        connection.readResult = .failure(.rpc(.init(code: RPCErrorCode.refused, message: "Changed",
            data: .object(["reason": .string("fallback"), "detail": .string("moved")]))))
        let model = model(connection)
        _ = try await model.start("/models")
        await #expect(throws: (any Error).self) { _ = try await model.read() }
        #expect(model.phase == .ended && !model.confirmedEnd)
        #expect(connection.calls.map(\.action) == ["terminal.command"])
        #expect(connection.closes == 1)
    }

    @Test func failedSelectionEndsOwnershipAndNeverReplaysEnter() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a")),
            .init(result: .failed, message: "Not confirmed"), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.start("/models")
        let result = await model.run("terminal.select", target: "a/1", args: nil)
        #expect(result?.result == .failed)
        #expect(model.phase == .ended)
        #expect(connection.closes == 1)
        #expect(connection.calls.count == 2)
        await model.close(cancel: false)
    }

    @Test func aReadReturnsTheNormalizedReselectionContract() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a")), .init(result: .applied)]
        connection.readResult = .success(menu("a"))
        let model = model(connection)
        _ = try await model.start("/models")
        let result = try await model.read(query: "deep", before: "")
        #expect(result.picker?.selectionBehavior == "reselect")
        #expect(result.picker?.onSelectArgs?["interaction"] == .string("a"))
        await model.close(cancel: false)
    }

    @Test func aLateConnectionAfterHandbackIsClosedWithoutTyping() async throws {
        let connection = Connection()
        let gate = AsyncStream<Void>.makeStream()
        var connecting = false
        let model = TerminalNativeInteraction(sessionID: "session") {
            connecting = true
            var iterator = gate.stream.makeAsyncIterator()
            _ = await iterator.next()
            return connection
        }
        let start = Task { try await model.start("/models") }
        try await until { connecting }
        await model.close(cancel: false)
        gate.continuation.yield(())
        _ = try? await start.value
        #expect(connection.calls.isEmpty)
        #expect(connection.closes == 1)
        gate.continuation.finish()
    }

    @Test func aLateCommandReplyAfterHandbackStaysVisiblyUnconfirmed() async throws {
        let connection = Connection()
        let gate = AsyncStream<Void>.makeStream()
        let next = menu("a")
        connection.handle = { _ in
            var iterator = gate.stream.makeAsyncIterator()
            _ = await iterator.next()
            return .init(result: .applied, screen: next)
        }
        let model = model(connection)
        let start = Task { try await model.start("/models") }
        try await until { !connection.calls.isEmpty }
        await model.close(cancel: false)
        gate.continuation.yield(())
        do {
            _ = try await start.value
            Issue.record("A late command reply must not reopen its interaction")
        } catch {
            #expect((error as? TerminalNativeInteraction.Failure)?.message == SurfaceCopy.unconfirmed)
        }
        #expect(model.phase == .ended && model.screen == nil)
        #expect(connection.closes == 1 && connection.calls.count == 1)
        gate.continuation.finish()
    }

    @Test func aSelectionCarriesThePanelTokenAndCanReselectCurrent() {
        let screen = menu("a")
        var picker = screen.picker!
        picker.selectionBehavior = "reselect"
        let row = picker.items[0]
        let state = PickerModel(picker)
        #expect(state.tap(row, in: picker) == .select)
        let change = PickerModel.change(selecting: row, in: picker, setting: screen.title)
        #expect(change.target == "a/0")
        #expect(change.args?["interaction"] == .string("a"))
    }

    @Test func sourceQuerySeedsTheFieldAndAnIdleDeskEditIsAdopted() {
        var state = PickerModel(menu("a", query: "desk").picker!)
        #expect(state.query == "desk")
        let idleConflict = state.synchronizeQuery("new desk", previous: "desk")
        #expect(!idleConflict)
        #expect(state.query == "new desk")
        state.query = "phone draft"
        let pendingConflict = state.synchronizeQuery("desk edit", previous: "new desk")
        #expect(pendingConflict)
        #expect(state.query == "phone draft")
    }

    @Test func refreshDoesNotReplaceTheSearchAlreadyOnScreen() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a", query: "desk")), .init(result: .applied)]
        connection.readResult = .success(menu("a", query: "desk"))
        let model = model(connection)
        _ = try await model.start("/models")
        _ = try await model.read(expected: "a")
        #expect(connection.reads.count == 1)
        #expect(connection.reads[0]?["query"] == nil)
        #expect(model.screen?.picker?.query == "desk")
        await model.close(cancel: false)
    }

    @Test func anOldPanelSearchIsRefusedBeforeAnyReadOrKey() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a")), .init(result: .applied, screen: menu("b")), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.start("/models")
        _ = await model.run("terminal.select", target: "a/0", args: ["interaction": .string("a")])
        await #expect(throws: (any Error).self) { _ = try await model.read(query: "deep", expected: "a", before: "") }
        #expect(connection.reads.isEmpty)
        await model.close(cancel: false)
    }

    @Test func queryConflictRefreshesWithoutTypingAndKeepsOwnership() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a")), .init(result: .applied)]
        connection.readResults = [.failure(.rpc(.init(code: RPCErrorCode.refused, message: "Query changed",
            data: .object(["reason": .string("fallback"), "detail": .string("query")])))), .success(menu("a", query: "desk edit"))]
        let model = model(connection)
        _ = try await model.start("/models")
        await #expect(throws: (any Error).self) { _ = try await model.read(query: "phone", expected: "a", before: "") }
        #expect(model.phase == .active && model.screen?.picker?.query == "desk edit")
        #expect(connection.closes == 0 && connection.reads.count == 2)
        #expect(connection.reads[0]?["before"] == "")
        #expect(connection.reads[1]?["query"] == nil)
        await model.close(cancel: false)
    }

    @Test func unknownSearchTextCannotBeOverwrittenFromThePhone() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a", query: nil)), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.start("/models")
        #expect(model.screen?.picker?.searchable == false)
        await #expect(throws: (any Error).self) { _ = try await model.read(query: "phone", expected: "a", before: "") }
        #expect(connection.reads.isEmpty)
        await model.close(cancel: false)
    }

    @Test func supersededSearchWaitsForItsDispatchedQueryBeforeTheNextOne() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: menu("a")), .init(result: .applied)]
        let gate = AsyncStream<Void>.makeStream()
        let deep = menu("a", query: "deep")
        let glm = menu("a", query: "glm")
        connection.handleRead = { values in
            if values?["query"] == "deep" {
                var iterator = gate.stream.makeAsyncIterator()
                _ = await iterator.next()
                return deep
            }
            return glm
        }
        let model = model(connection)
        _ = try await model.start("/models")
        let first = Task { try await model.read(query: "deep", expected: "a", before: "") }
        try await until { connection.reads.count == 1 }
        first.cancel()
        let second = Task { try await model.read(query: "glm", expected: "a", before: "") }
        await Task.yield()
        #expect(connection.reads.count == 1)
        gate.continuation.yield(())
        _ = try? await first.value
        let result = try await second.value
        #expect(connection.reads.count == 2)
        #expect(connection.reads[1]?["before"] == "deep")
        #expect(result.picker?.query == "glm")
        gate.continuation.finish()
        await model.close(cancel: false)
    }

    private func until(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(2)
        while !condition(), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        try #require(condition())
    }
}
