import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct TerminalRequestMoreTests {
    private func card(_ id: String = "request", host: HostID = Samples.studio) -> RequestCard {
        RequestCard(id: id, hostID: host, sessionID: "s-04", bib: "04", agentName: "OMP", title: "Choose or reply",
            detail: nil, options: [.init(n: 1, label: "Yes", role: .approve), .init(n: 2, label: "No", role: .deny),
                                  .init(n: 3, label: "A longer choice", role: .other)], allowText: true, phase: .open)
    }

    @Test func moreOpensTheExactRequestsNativeControls() throws {
        let services = FakeAppServices()
        let app = AppStore(services: services)
        let selected = card()
        app.controlCenter.requests = [selected]
        app.open(hostID: selected.hostID, sessionID: "s-04")
        let terminals = TerminalPool()
        terminals.sync(with: app)
        defer { terminals.release(window: .single) }
        let handle = try #require(terminals.handle(for: TabID(hostID: selected.hostID, sessionID: "s-04")))
        handle.onPromptKey(PromptKeys.more)
        #expect(app.sheet == .requestPrompt(selected.hostID, selected.id))
    }

    @Test func sameSessionIDOnAnotherMachineCannotSupplyTheKeys() {
        let own = card("own"), foreign = card("foreign", host: UUID())
        #expect(PromptKeys.card(for: "s-04", hostID: own.hostID, in: [foreign, own])?.id == own.id)
    }

    @Test func morePreservesAnExistingForm() throws {
        let app = AppStore(services: FakeAppServices())
        let request = card()
        app.controlCenter.requests = [request]
        app.open(hostID: request.hostID, sessionID: "s-04")
        app.sheet = .editMachine(request.hostID)
        let terminals = TerminalPool()
        terminals.sync(with: app)
        defer { terminals.release(window: .single) }
        try #require(terminals.handle(for: TabID(hostID: request.hostID, sessionID: "s-04"))).onPromptKey(PromptKeys.more)
        #expect(app.sheet == .editMachine(request.hostID))
    }

    @Test func nativePromptUsesTheExistingAnswerRoute() async throws {
        let services = FakeAppServices()
        let app = AppStore(services: services)
        let request = card()
        app.controlCenter.requests = [request]
        app.sheet = .requestPrompt(request.hostID, request.id)
        let model = RequestPromptModel(store: app, hostID: request.hostID, requestID: request.id, armDelay: .zero)
        model.update()
        await model.bar.arming?.value
        #expect(model.request?.more.map(\.label) == ["A longer choice"])
        model.bar.answer(.yes, to: request.id)
        let deadline = ContinuousClock.now + .seconds(3)
        while !services.calls.contains(.answer(request.hostID, request.id, .option(1))) {
            guard ContinuousClock.now < deadline else { throw HarnessUnavailable() }
            await Task.yield()
        }
    }

    @Test func aWithdrawnReplyKeepsItsDraftAndCannotSend() {
        let services = FakeAppServices()
        let app = AppStore(services: services)
        let request = card()
        app.controlCenter.requests = [request]
        app.sheet = .requestPrompt(request.hostID, request.id)
        let model = RequestPromptModel(store: app, hostID: request.hostID, requestID: request.id)
        model.update()
        model.bar.beginReply()
        model.bar.replyDraft = "keep these words"
        app.controlCenter.requests = []
        model.update()
        model.bar.sendReply()
        #expect(model.bar.replyDraft == "keep these words")
        #expect(model.bar.stage.request?.state == .withdrawn)
        #expect(services.calls.isEmpty)
        model.close()
        app.sheet = .newSession(request.hostID)
        model.close()
        #expect(app.sheet == .newSession(request.hostID))
    }
}
