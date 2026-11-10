import Foundation
import Testing
import UIKit
@testable import TogoTerminal

@MainActor
@Suite struct ShortcutTests {
    @Test func optionMetaKeepsShift() {
        let shifted = KeyEncoder.hardware(
            characters: ":", charactersIgnoringModifiers: ";", modifiers: [.alternate, .shift], optionAsMeta: true)
        #expect(shifted == [0x1b, 0x3a])
        #expect(KeyEncoder.hardware(
            characters: ":", charactersIgnoringModifiers: ";", modifiers: [.alternate, .shift], optionAsMeta: false) == nil)
    }

    @Test func optionComposedCharacterFallsBackToKey() {
        #expect(KeyEncoder.hardware(
            characters: "å", charactersIgnoringModifiers: "a", modifiers: .alternate, optionAsMeta: true) == [0x1b, 0x61])
        #expect(KeyEncoder.hardware(
            characters: "Å", charactersIgnoringModifiers: "a", modifiers: [.alternate, .shift], optionAsMeta: true) == [0x1b, 0x41])
    }

    @Test func hardwareCtrlChords() {
        #expect(KeyEncoder.hardware(characters: "\u{3}", charactersIgnoringModifiers: "c", modifiers: .control, optionAsMeta: true) == [0x03])
        #expect(KeyEncoder.hardware(characters: "", charactersIgnoringModifiers: "x", modifiers: [.control, .alternate], optionAsMeta: true) == [0x1b, 0x18])
        #expect(KeyEncoder.hardware(characters: "é", charactersIgnoringModifiers: "é", modifiers: .control, optionAsMeta: true) == nil)
    }

    @Test func hardwareLeavesPlainTextAndCommandToSwiftTerm() {
        #expect(KeyEncoder.hardware(characters: "a", charactersIgnoringModifiers: "a", modifiers: [], optionAsMeta: true) == nil)
        #expect(KeyEncoder.hardware(characters: "A", charactersIgnoringModifiers: "a", modifiers: .shift, optionAsMeta: true) == nil)
        #expect(KeyEncoder.hardware(characters: "v", charactersIgnoringModifiers: "v", modifiers: .command, optionAsMeta: true) == nil)
        #expect(KeyEncoder.hardware(
            characters: UIKeyCommand.inputUpArrow, charactersIgnoringModifiers: UIKeyCommand.inputUpArrow,
            modifiers: .control, optionAsMeta: true) == nil)
    }

    @Test func shortcutsHaveTitlesAndInputs() {
        let commands = HardwareShortcuts.commands(action: #selector(UIResponder.becomeFirstResponder))
        let expected: [(String, ShortcutAction)] = (1...9).map { ("\($0)", .session($0)) } + [
            ("t", .newSession), ("k", .controlCenter), ("w", .detach), ("v", .paste),
        ]
        #expect(commands.count == expected.count + 7)
        for (input, action) in expected {
            let command = commands.first { $0.input == input }
            #expect(command != nil, "missing ⌘\(input)")
            guard let command else { continue }
            #expect(!command.title.isEmpty)
            #expect(command.modifierFlags == .command)
            #expect(command.wantsPriorityOverSystemBehavior)
            #expect(ShortcutAction(command: command) == action)
        }
        // Ctrl and Option chords without Command belong to the remote program.
        #expect(commands.allSatisfy { $0.modifierFlags.contains(.command) })
    }

    @Test func terminalPassesNewShortcutsUp() {
        let expected: [(String, UIKeyModifierFlags, ShortcutAction)] = [
            ("s", [.command, .control], .toggleSidebar),
            ("\\", .command, .splitToggle),
            (UIKeyCommand.inputLeftArrow, [.command, .alternate], .focusLeft),
            (UIKeyCommand.inputRightArrow, [.command, .alternate], .focusRight),
            ("[", [.command, .shift], .previousTab),
            ("]", [.command, .shift], .nextTab),
            ("f", [.command, .control], .toggleZen),
        ]
        let h = EngineHarness()
        var received: [ShortcutAction] = []
        h.engine.onShortcut = { received.append($0) }
        let commands = h.engine.view.keyCommands ?? []
        for (input, modifiers, action) in expected {
            let command = commands.first { $0.input == input && $0.modifierFlags == modifiers }
            #expect(command != nil, "missing \(action)")
            guard let command else { continue }
            #expect(!command.title.isEmpty)
            #expect(command.wantsPriorityOverSystemBehavior)
            (h.engine.view as? TogoTerminalView)?.performShortcut(command)
        }
        #expect(received == expected.map(\.2))
    }

    @Test func terminalViewOffersShortcuts() {
        let h = EngineHarness()
        var received: [ShortcutAction] = []
        h.engine.onShortcut = { received.append($0) }
        let commands = h.engine.view.keyCommands ?? []
        let newSession = commands.first { $0.input == "t" && $0.modifierFlags == .command }
        #expect(newSession != nil)
        let target = h.engine.view.target(forAction: #selector(TogoTerminalView.performShortcut(_:)), withSender: newSession)
        #expect(target as? UIView === h.engine.view)
        newSession.map { (h.engine.view as? TogoTerminalView)?.performShortcut($0) }
        #expect(received == [.newSession])
    }

    @Test func chordRepeatsAfterDelayThenInterval() {
        var pending: [(TimeInterval, @MainActor () -> Void)] = []
        var cancelled = 0
        let repeater = ChordRepeater { delay, body in
            pending.append((delay, body))
            return { cancelled += 1 }
        }
        var sent: [[UInt8]] = []
        repeater.send = { sent.append($0) }
        let key = HeldKey()
        repeater.start(key: 6, bytes: [0x03], isHeld: { key.held })
        #expect(pending.map(\.0) == [ChordRepeater.initialDelay])
        pending.removeFirst().1()
        pending.removeFirst().1()
        #expect(sent == [[0x03], [0x03]])
        #expect(pending.map(\.0) == [ChordRepeater.interval])

        repeater.stop(key: 7)
        #expect(repeater.heldKey == 6)
        repeater.stop(key: 6)
        #expect(repeater.heldKey == nil)
        #expect(cancelled == 1)

        pending.removeAll()
        repeater.start(key: 6, bytes: [0x03], isHeld: { key.held })
        key.held = false
        pending.removeFirst().1()
        #expect(sent.count == 2)
        #expect(repeater.heldKey == nil)
        #expect(pending.isEmpty)
    }

    @Test func handledChordSendsAndRepeatsUntilFocusLeaves() async {
        let h = EngineHarness()
        let handled = h.engine.handleChord(characters: "\u{3}", ignoring: "c", modifiers: .control, keyCode: 6, isHeld: { true })
        #expect(handled)
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data([0x03]))
        #expect(h.engine.chordRepeater.heldKey == 6)
        _ = h.engine.view.resignFirstResponder()
        #expect(h.engine.chordRepeater.heldKey == nil)

        let plain = h.engine.handleChord(characters: "a", ignoring: "a", modifiers: [], keyCode: 4, isHeld: { true })
        #expect(!plain)
        #expect(h.engine.chordRepeater.heldKey == nil)
    }
}

@MainActor private final class HeldKey {
    var held = true
}
