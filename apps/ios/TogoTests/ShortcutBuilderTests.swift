import Foundation
import Testing
import TogoKit
import TogoTerminal
@testable import Togo

@MainActor struct ShortcutBuilderTests {
    @Test func builderCtrlUpArrow() throws {
        var draft = ShortcutDraft()
        draft.ctrl = true
        draft.key = .up
        #expect(draft.steps == (try KeySequence.parse("C-Up")))
        #expect(draft.preview == "ESC [ 1 ; 5 A")
        #expect(draft.item?.title == "Ctrl Up")
    }

    @Test func builderCustomTextEscapes() throws {
        var draft = ShortcutDraft()
        draft.key = .tab
        draft.customText = #"say "hi" \ now"#
        #expect(draft.key == nil)
        #expect(draft.steps == (try KeySequence.parse(#"text:"say \"hi\" \\ now""#)))
        #expect(KeySequence.format(draft.steps) == #"text:"say \"hi\" \\ now""#)
        #expect(draft.preview == #""say \"hi\" \\ now""#)

        draft.customText = "\u{3}x\n"
        #expect(draft.preview == #"^C "x" LF"#)

        draft.customText = "ESC"
        #expect(draft.preview == #""ESC""#)
    }

    @Test func shortcutsAreAddedForEveryAgentOrOne() {
        var editor = TerminalKeysEditor(config: TerminalKeysConfig())
        let deploy = KeyBarItem(title: "Deploy", action: .steps([.text("make deploy")]))
        let compact = KeyBarItem(title: "/compact", action: .steps([.text("/compact")]))
        editor.add(deploy, for: nil)
        editor.add(compact, for: "Claude")
        #expect(editor.config.shortcuts == [deploy])
        #expect(editor.config.shortcutsByAgent == ["claude": [compact]])
        editor.remove(at: IndexSet(integer: 0), for: "claude")
        #expect(editor.config.shortcutsByAgent.isEmpty, "an agent with no shortcuts left is dropped")
        editor.add(compact, for: nil)
        editor.move(from: IndexSet(integer: 1), to: 0, for: nil)
        #expect(editor.config.shortcuts == [compact, deploy])
    }

    @Test func removeMachineAsksFirst() {
        let machine = SettingsMachine(
            id: Samples.studio, name: "Studio", address: "studio.local", symbol: .desktop,
            helper: .present(version: "0.3.0", live: true)
        )
        var removal = MachineRemoval()
        var removed: [HostID] = []

        removal.request(machine)
        #expect(removed.isEmpty)
        #expect(removal.pending == machine)

        removal.cancel()
        removal.confirm { removed.append($0) }
        #expect(removed.isEmpty)

        removal.request(machine)
        removal.confirm { removed.append($0) }
        removal.confirm { removed.append($0) }
        #expect(removed == [Samples.studio])
        #expect(removal.pending == nil)
    }
}
