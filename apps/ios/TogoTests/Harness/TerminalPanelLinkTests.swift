import Foundation
import Testing
import TogoKit
@testable import Togo

@Suite(.serialized) @MainActor struct TerminalPanelLinkTests {
    private func panel(_ url: String = "https://github.com/login/device", token: String = "panel-a") -> HarnessScreen {
        .init(id: "terminal.panel/" + token, primitive: .detail, title: "Login with GitHub Copilot",
            fetchedAt: WireDate(.now),
            detail: .init(body: .init(kind: "text", text: url + "\nEnter code: WXYZ-1234\nWaiting for authorization…"),
                actions: [.init(id: "terminal.openLink.0", label: "Open link", role: .primary,
                    args: ["interaction": .string(token), "url": .string(url)])]), actions: [], interaction: token)
    }

    private func start(_ screen: HarnessScreen) async throws -> (TerminalNativeInteraction, TerminalNativeInteractionTests.Connection) {
        let connection = TerminalNativeInteractionTests.Connection()
        connection.results = [.init(result: .applied, screen: screen)]
        let model = TerminalNativeInteraction(sessionID: "owned", pollInterval: .seconds(60), makeConnection: { connection })
        _ = try await model.start("/connect")
        return (model, connection)
    }

    @Test func offeredLinkOpensLocallyWithoutReportingAuthenticationSuccess() async throws {
        let screen = panel()
        let (model, connection) = try await start(screen)
        var opened: [URL] = []
        let result = model.openPanelLink("terminal.openLink.0", args: screen.detail?.actions.first?.args) { opened.append($0) }
        #expect(result.result == .typed)
        #expect(opened.map(\.absoluteString) == ["https://github.com/login/device"])
        #expect(connection.calls.map(\.action) == ["terminal.command"])
        #expect(model.phase == .active)
        await model.close(cancel: false)
    }

    @Test(arguments: ["file:///tmp/key", "javascript:alert(1)", "togo://session", "https://user:secret@example.com/"])
    func unsafeURLDoesNotOpen(_ url: String) async throws {
        let screen = panel(url)
        let (model, _) = try await start(screen)
        var opened = false
        let result = model.openPanelLink("terminal.openLink.0", args: screen.detail?.actions.first?.args) { _ in opened = true }
        #expect(!opened && result.result == .failed)
        await model.close(cancel: false)
    }

    @Test func staleOrChangedArgumentsCannotOpenAnotherLink() async throws {
        let screen = panel()
        let (model, connection) = try await start(screen)
        for args in [nil, ["interaction": .string("old"), "url": .string("https://github.com/login/device")],
                     ["interaction": .string("panel-a"), "url": .string("https://example.com/other")]] as [[String: JSONValue]?] {
            let result = model.openPanelLink("terminal.openLink.0", args: args) { _ in Issue.record("Un-offered link opened") }
            #expect(result.result == .failed)
        }
        #expect(connection.calls.count == 1)
        await model.close(cancel: false)
        let result = model.openPanelLink("terminal.openLink.0", args: screen.detail?.actions.first?.args) { _ in Issue.record("Link opened after handback") }
        #expect(result.result == .failed)
    }

    @Test func unofferedActionIsNeverSentToTheDesktop() async throws {
        let screen = panel()
        let (model, connection) = try await start(screen)
        let result = model.openPanelLink("terminal.openLink.1", args: screen.detail?.actions.first?.args) { _ in Issue.record("Unknown link opened") }
        #expect(result.result == .failed && connection.calls.count == 1)
        await model.close(cancel: false)
    }

    @Test func panelPollCanHandOverToTheNextObservedPicker() async throws {
        let first = panel()
        let (model, connection) = try await start(first)
        let next = HarnessScreen(id: "terminal.menu/next", primitive: .picker, title: "Choose a model",
            fetchedAt: WireDate(.now), picker: .init(items: [.init(id: "next/one", title: "A model")], searchable: false,
                onSelect: "terminal.select"), interaction: "next")
        connection.readResult = .success(next)
        _ = try await model.read(expected: "panel-a")
        #expect(model.screen?.interaction == "next" && model.phase == .active)
        let stale = model.openPanelLink("terminal.openLink.0", args: first.detail?.actions.first?.args) { _ in Issue.record("Old link opened") }
        #expect(stale.result == .failed)
        await #expect(throws: (any Error).self) { _ = try await model.read(expected: "panel-a") }
        #expect(connection.calls.map(\.action) == ["terminal.command"])
        #expect(connection.reads.count == 1)
        await model.close(cancel: false)
    }

    @Test func panelPollCanHandOverToAnObservedPromptWithoutTyping() async throws {
        let (model, connection) = try await start(panel())
        let next = HarnessScreen(id: "terminal.prompt/next", primitive: .settings, title: "Enter code",
            fetchedAt: WireDate(.now), settings: .init(sections: []), interaction: "next")
        connection.readResult = .success(next)
        _ = try await model.read()
        #expect(model.screen?.primitive == .settings && model.screen?.interaction == "next")
        #expect(connection.calls.map(\.action) == ["terminal.command"])
        await model.close(cancel: false)
    }

    @Test func sourceClosingPanelEndsWithoutClaimingAuthentication() async throws {
        let (model, connection) = try await start(panel())
        connection.readResult = .failure(.rpc(.init(code: RPCErrorCode.refused, message: "This panel has closed.",
            data: .object(["reason": .string("fallback"), "detail": .string("closed")]))))
        await #expect(throws: (any Error).self) { _ = try await model.read() }
        #expect(model.phase == .ended && !model.confirmedEnd && model.failure == nil)
        #expect(connection.calls.map(\.action) == ["terminal.command"] && connection.closes == 1)
    }
}
