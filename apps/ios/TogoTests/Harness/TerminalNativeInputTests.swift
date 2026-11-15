import Foundation
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct TerminalNativeInputTests {
    private typealias Connection = TerminalNativeInteractionTests.Connection

    private func prompt(_ token: String) -> HarnessScreen {
        .init(id: "terminal.prompt/" + token, primitive: .settings, title: "Enter a value", fetchedAt: WireDate(.now),
            settings: .init(sections: [.init(fields: [.init(id: "terminal.input", label: "Enter a value",
                kind: .secret, value: .null, isSet: false, action: "terminal.input", actionLabel: "Continue", presentation: "prompt")])]),
            interaction: token)
    }

    private func model(_ connection: Connection) -> TerminalNativeInteraction {
        TerminalNativeInteraction(sessionID: "session", pollInterval: .seconds(1000)) { connection }
    }

    @Test func aPromptSubmissionCarriesOnlyItsOfferedFieldAndConnectionToken() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: prompt("a")), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.start("/connect")
        let field = try #require(model.screen?.settings?.sections.first?.fields.first)
        let value = "neutral <div> 100%!"
        let change = SettingsLogic.change(field, to: .string(value), action: "terminal.input")
        let result = await model.run(change.action, target: change.target, args: change.args)
        #expect(result?.result == .applied && model.confirmedEnd)
        #expect(connection.calls.count == 2)
        #expect(connection.calls[1].target == "terminal.input")
        #expect(connection.calls[1].args == ["value": .string(value), "interaction": .string("a")])
        #expect(change.value == PrimitiveCopy.set && change.previous == nil)
    }

    @Test func observedValidationFailureStaysOpenWithoutEchoingOrRetryingTheEntry() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: prompt("a")),
            .init(result: .failed, message: "invalid neutral-secret", screen: prompt("a")), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.start("/connect")
        let result = await model.run("terminal.input", target: "terminal.input", args: ["value": .string("neutral-secret")])
        #expect(result?.result == .failed && model.phase == .active)
        #expect(result?.message?.contains("neutral-secret") == false)
        #expect(model.failure == nil && model.screen?.settings?.sections.first?.fields.first?.value == .null)
        #expect(connection.calls.count == 2 && connection.closes == 0)
        _ = await model.run("terminal.input", target: "terminal.input", args: ["value": .string("explicit second entry")])
        #expect(connection.calls.count == 3 && model.confirmedEnd)
    }

    @Test func failureWithoutAnObservedEmptyPromptEndsWithoutReplayOrEcho() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: prompt("a")),
            .init(result: .failed, message: "neutral-secret")]
        let model = model(connection)
        _ = try await model.start("/connect")
        let result = await model.run("terminal.input", target: "terminal.input", args: ["value": .string("neutral-secret")])
        #expect(model.phase == .ended && !model.confirmedEnd)
        #expect(result?.message == SurfaceCopy.unconfirmed)
        #expect(connection.calls.count == 2 && connection.closes == 1)
    }

    @Test func aFieldNoLongerOfferedCannotReceiveAnEntry() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: prompt("a")), .init(result: .applied)]
        let model = model(connection)
        _ = try await model.start("/connect")
        let result = await model.run("terminal.input", target: "another-field", args: ["value": .string("neutral")])
        #expect(result?.result == .failed && model.phase == .active)
        #expect(connection.calls.count == 1)
        await model.close(cancel: false)
    }

    @Test func handbackDuringSubmissionIgnoresTheLateAnswerAndNeverRetries() async throws {
        let connection = Connection()
        connection.results = [.init(result: .applied, screen: prompt("a"))]
        let model = model(connection)
        _ = try await model.start("/connect")
        let gate = AsyncStream<Void>.makeStream()
        connection.handle = { _ in
            var iterator = gate.stream.makeAsyncIterator()
            _ = await iterator.next()
            return .init(result: .applied)
        }
        let send = Task { await model.run("terminal.input", target: "terminal.input", args: ["value": .string("neutral")]) }
        let deadline = ContinuousClock.now + .seconds(2)
        while connection.calls.count < 2, ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        try #require(connection.calls.count == 2)
        await model.close(cancel: false)
        gate.continuation.yield(())
        let late = await send.value
        gate.continuation.finish()
        #expect(late == nil && model.phase == .ended && !model.confirmedEnd)
        #expect(connection.calls.count == 2 && connection.closes == 1)
    }

    @Test func aPromptShowsAnEmptySecureInputWithoutOpeningASettingEditor() async throws {
        let deadline = ContinuousClock.now + .seconds(3)
        while UIApplication.shared.connectedScenes.compactMap({ $0 as? UIWindowScene }).isEmpty,
              ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(20)) }
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let previous = scene.keyWindow
        var screen = prompt("layout")
        // Even a malformed source value must never prefill a secure field.
        screen.settings?.sections[0].fields[0].value = .string("source-must-not-appear")
        var context = PrimitiveContext.inert
        context.showsTerminal = false
        let host = UIHostingController(rootView:
            NativeCommandNavigation(title: "Connect", close: {}, terminal: {}) {
                PrimitiveScreenView(screen: screen, context: context)
            }.environment(\.colorScheme, .dark).environment(\.loopsPaused, true)
        )
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 430, height: 932)
        window.rootViewController = host
        window.makeKeyAndVisible()
        host.view.frame = window.bounds
        defer { window.isHidden = true; previous?.makeKeyAndVisible() }
        let fieldDeadline = ContinuousClock.now + .seconds(2)
        while secureFields(in: host.view).isEmpty, ContinuousClock.now < fieldDeadline {
            try await Task.sleep(for: .milliseconds(20))
        }
        let field = try #require(secureFields(in: host.view).first)
        #expect(field.text?.isEmpty != false)
        try await Task.sleep(for: .milliseconds(100))
        host.view.layoutIfNeeded()
        #expect(field.window === window && field.bounds.width > 0)
        let image = UIGraphicsImageRenderer(bounds: host.view.bounds).image { _ in
            host.view.drawHierarchy(in: host.view.bounds, afterScreenUpdates: true)
        }
        Attachment.record(image, named: "native-terminal-prompt", as: .png)
    }

    private func secureFields(in view: UIView) -> [UITextField] {
        let own = (view as? UITextField).map { $0.isSecureTextEntry ? [$0] : [] } ?? []
        return own + view.subviews.flatMap { secureFields(in: $0) }
    }
}
