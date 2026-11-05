import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor
struct ComposeTests {
    private func model(defaults: UserDefaults, sessionID: String = "s-04", log: Log) -> ComposeModel {
        ComposeModel(
            history: ComposeHistory(hostID: Samples.studio, sessionID: sessionID, defaults: defaults),
            images: ImagePasteFlow(support: .available, upload: { _, _ in "" }, paste: { _ in }),
            paste: { log.events.append("paste:\($0)") },
            pressEnter: { log.events.append("enter") }
        )
    }

    private func freshDefaults() -> UserDefaults {
        UserDefaults(suiteName: "compose-tests-\(UUID().uuidString)")!
    }

    @Test func sendPastesTheTextThenPressesEnter() {
        let log = Log()
        let compose = model(defaults: freshDefaults(), log: log)
        compose.text = "fix the failing test\nthen run it again"
        compose.send()
        #expect(log.events == ["paste:fix the failing test\nthen run it again", "enter"])
        #expect(compose.text.isEmpty)
    }

    @Test func blankTextSendsNothing() {
        let log = Log()
        let compose = model(defaults: freshDefaults(), log: log)
        compose.text = "  \n "
        #expect(!compose.canSend)
        compose.send()
        #expect(log.events.isEmpty)
    }

    @Test func historyKeepsTheLastTwentySendsNewestFirst() {
        let defaults = freshDefaults()
        let compose = model(defaults: defaults, log: Log())
        for n in 1...25 {
            compose.text = "message \(n)"
            compose.send()
        }
        compose.text = "message 24"
        compose.send()
        #expect(compose.history.count == 20)
        #expect(compose.history.first == "message 24")
        #expect(compose.history.filter { $0 == "message 24" }.count == 1)
        #expect(compose.history.last == "message 6")
        #expect(model(defaults: defaults, log: Log()).history == compose.history)
    }

    @Test func historyIsKeptPerSession() {
        let defaults = freshDefaults()
        let first = model(defaults: defaults, sessionID: "s-01", log: Log())
        first.text = "only here"
        first.send()
        #expect(model(defaults: defaults, sessionID: "s-02", log: Log()).history.isEmpty)
    }

    @Test func recallingAnEntryFillsTheField() {
        let compose = model(defaults: freshDefaults(), log: Log())
        compose.text = "earlier"
        compose.send()
        compose.recall("earlier")
        #expect(compose.text == "earlier")
    }
}

@MainActor final class Log {
    var events: [String] = []
}
