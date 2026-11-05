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

    @Test func presetReplacesBar() {
        var editor = KeyBarEditor(config: .standard)
        editor.add(KeyBarItem(title: "Deploy", action: .steps([.text("make deploy")])))
        editor.move(from: IndexSet(integer: 0), to: 3)
        editor.apply(.claude)
        #expect(editor.config == KeyBarConfig.claude)
        #expect(editor.activePreset == .claude)
        editor.remove(at: IndexSet(integer: 0))
        #expect(editor.activePreset == nil)
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
