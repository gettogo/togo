import Foundation
import Testing
import TogoKit
import TogoTerminal
@testable import Togo

@Suite struct PromptKeysTests {
    func card(_ options: [ApprovalOption], allowText: Bool = false, phase: RequestCard.Phase = .open) -> RequestCard {
        RequestCard(
            id: "req", hostID: Samples.studio, sessionID: "s1", bib: "01", agentName: "OpenCode",
            title: "Allow this write?", detail: nil, options: options, allowText: allowText, phase: phase, signIn: nil
        )
    }

    @Test func fewOptionsAreWordsWithTheApproveOneFilled() {
        let card = card([
            ApprovalOption(n: 1, label: "Allow once", role: .approve),
            ApprovalOption(n: 2, label: "Deny", role: .deny),
        ])
        let keys = PromptKeys.plan(for: card)
        #expect(keys.map(\.label) == ["Allow once", "Deny"])
        #expect(keys.map(\.filled) == [true, false])
        #expect(PromptKeys.decision(for: keys[1], card: card) == .option(2))
    }

    @Test func textIsWelcomeSoMoreShowsTheWholeRequest() {
        let keys = PromptKeys.plan(for: card([ApprovalOption(n: 1, label: "Yes", role: .approve)], allowText: true))
        #expect(keys.map(\.id) == ["option-1", PromptKeys.moreID])
        #expect(PromptKeys.plan(for: card([], allowText: true)).map(\.kind) == [.more])
        #expect(PromptKeys.plan(for: card([])).isEmpty)
    }

    @Test func aLongMenuWithoutRolesIsDrivenWithArrows() {
        let options = (1...5).map { ApprovalOption(n: $0, label: "Choice \($0)", role: .other) }
        let keys = PromptKeys.plan(for: card(options))
        #expect(keys.map(\.id) == ["up", "down", "enter", PromptKeys.moreID])
        #expect(keys[0].kind == .stroke(KeyStroke(.up)))
    }

    @Test func aLongMenuWithRolesKeepsApproveDenyAndMore() {
        var options = (1...3).map { ApprovalOption(n: $0, label: "Choice \($0)", role: .other) }
        options.append(ApprovalOption(n: 4, label: "Allow", role: .approve))
        options.append(ApprovalOption(n: 5, label: "Deny", role: .deny))
        let keys = PromptKeys.plan(for: card(options))
        #expect(keys.map(\.label) == ["Allow", "Deny", "More"])
        #expect(keys[0].filled)
    }

    @Test func onlyOpenRequestsOfTheSessionCount() {
        let open = card([ApprovalOption(n: 1, label: "Yes", role: .approve)])
        var other = open
        other.sessionID = "s2"
        var done = open
        done.phase = .done(.approved)
        #expect(PromptKeys.card(for: "s1", in: [done, other, open])?.id == open.id)
        #expect(PromptKeys.plan(for: done).isEmpty)
        #expect(PromptKeys.decision(for: PromptKeys.more, card: open) == nil)
    }
}

@Suite struct TerminalKeysSettingsTests {
    @Test func settingsReadOldBarBytesAsTheNewConfig() throws {
        var settings = AppSettings()
        #expect(settings.terminalKeys == TerminalKeysConfig())
        var bar = KeyBarConfig.standard
        bar.items.append(KeyBarItem(title: "Deploy", action: .steps([.text("make deploy")])))
        settings.keyBar = try JSONEncoder().encode(bar)
        #expect(settings.terminalKeys.shortcuts.map(\.title) == ["Deploy"])
        var keys = settings.terminalKeys
        keys.promptKeys = false
        settings.terminalKeys = keys
        #expect(settings.terminalKeys == keys)
        settings.keyBar = Data("not json".utf8)
        #expect(settings.terminalKeys == TerminalKeysConfig())
    }
}
