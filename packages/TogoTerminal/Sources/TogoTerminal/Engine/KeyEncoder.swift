import Foundation
import UIKit

/// xterm key encoding, the dialect tmux and the agent TUIs expect from `xterm-256color`.
public enum KeyEncoder {
    private static let esc: UInt8 = 0x1B

    public static func encode(_ k: KeyStroke, applicationCursor: Bool) -> [UInt8] {
        let mods = k.modifiers
        switch k.key {
        case .char(let c):
            return encodeCharacter(c, mods)
        case .escape:
            return altPrefixed([esc], mods)
        case .tab:
            return altPrefixed(mods.contains(.shift) ? csi("Z") : [0x09], mods)
        case .enter:
            // Legacy xterm has no Shift-Enter. ESC CR (Meta-Enter) is the newline Claude Code and
            // its /terminal-setup use; a bare LF would submit in readline.
            return altPrefixed([0x0D], mods.contains(.shift) ? mods.union(.alt) : mods)
        case .backspace:
            return altPrefixed([mods.contains(.ctrl) ? 0x08 : 0x7F], mods)
        case .up: return cursorKey("A", mods, applicationCursor)
        case .down: return cursorKey("B", mods, applicationCursor)
        case .right: return cursorKey("C", mods, applicationCursor)
        case .left: return cursorKey("D", mods, applicationCursor)
        case .home: return cursorKey("H", mods, applicationCursor)
        case .end: return cursorKey("F", mods, applicationCursor)
        case .insert: return tilde(2, mods)
        case .delete: return tilde(3, mods)
        case .pageUp: return tilde(5, mods)
        case .pageDown: return tilde(6, mods)
        case .function(let n): return functionKey(n, mods)
        }
    }

    /// xterm's modifier parameter: 1 plus shift 1, alt 2, ctrl 4. Nil when no modifier is held.
    private static func parameter(_ mods: KeyModifiers) -> Int? {
        var value = 1
        if mods.contains(.shift) { value += 1 }
        if mods.contains(.alt) { value += 2 }
        if mods.contains(.ctrl) { value += 4 }
        return value == 1 ? nil : value
    }

    private static func csi(_ body: String) -> [UInt8] {
        [esc, UInt8(ascii: "[")] + Array(body.utf8)
    }

    private static func altPrefixed(_ bytes: [UInt8], _ mods: KeyModifiers) -> [UInt8] {
        mods.contains(.alt) ? [esc] + bytes : bytes
    }

    /// Application cursor mode only changes unmodified keys; a modified key always uses CSI.
    private static func cursorKey(_ final: Character, _ mods: KeyModifiers, _ application: Bool) -> [UInt8] {
        if let p = parameter(mods) { return csi("1;\(p)\(final)") }
        if application { return [esc, UInt8(ascii: "O")] + Array(String(final).utf8) }
        return csi(String(final))
    }

    private static func tilde(_ code: Int, _ mods: KeyModifiers) -> [UInt8] {
        if let p = parameter(mods) { return csi("\(code);\(p)~") }
        return csi("\(code)~")
    }

    private static let functionTildeCodes = [15, 17, 18, 19, 20, 21, 23, 24, 25, 26, 28, 29, 31, 32, 33, 34]

    private static func functionKey(_ n: Int, _ mods: KeyModifiers) -> [UInt8] {
        switch n {
        case 1...4:
            let final = ["P", "Q", "R", "S"][n - 1]
            if let p = parameter(mods) { return csi("1;\(p)\(final)") }
            return [esc, UInt8(ascii: "O")] + Array(final.utf8)
        case 5...20:
            return tilde(functionTildeCodes[n - 5], mods)
        default:
            return []
        }
    }

    private static func encodeCharacter(_ c: Character, _ mods: KeyModifiers) -> [UInt8] {
        var text = String(c)
        if mods.contains(.shift) { text = text.uppercased() }
        guard mods.contains(.ctrl) else { return altPrefixed(Array(text.utf8), mods) }
        guard let control = controlByte(for: c) else { return [] }
        return altPrefixed([control], mods)
    }

    /// The C0 byte Ctrl produces with this character, following the VT220 table xterm uses.
    private static func controlByte(for c: Character) -> UInt8? {
        guard let ascii = c.asciiValue else { return nil }
        switch ascii {
        case UInt8(ascii: "a")...UInt8(ascii: "z"): return ascii - UInt8(ascii: "a") + 1
        case UInt8(ascii: "@")...UInt8(ascii: "_"): return ascii - UInt8(ascii: "@")
        case UInt8(ascii: " "), UInt8(ascii: "2"): return 0x00
        case UInt8(ascii: "3"): return 0x1B
        case UInt8(ascii: "4"): return 0x1C
        case UInt8(ascii: "5"): return 0x1D
        case UInt8(ascii: "6"): return 0x1E
        case UInt8(ascii: "7"), UInt8(ascii: "/"): return 0x1F
        case UInt8(ascii: "8"), UInt8(ascii: "?"): return 0x7F
        default: return nil
        }
    }
}

extension KeyEncoder {
    /// Bytes for a hardware Ctrl or Option-as-Meta chord, or nil to let SwiftTerm handle the
    /// press (plain text, IME composition, named keys, anything with Command).
    ///
    /// Meta uses `characters`, which keeps Shift, so Option-Shift-; sends `ESC :` rather than
    /// `ESC ;` (SwiftTerm #591). When Option composes a non-ASCII character (Option-a gives
    /// "å" on a US layout) the unmodified key is used instead, with Shift applied to letters.
    public static func hardware(
        characters: String,
        charactersIgnoringModifiers: String,
        modifiers: UIKeyModifierFlags,
        optionAsMeta: Bool
    ) -> [UInt8]? {
        guard !modifiers.contains(.command) else { return nil }
        let ctrl = modifiers.contains(.control)
        let meta = optionAsMeta && modifiers.contains(.alternate)
        guard ctrl || meta else { return nil }

        var keyModifiers: KeyModifiers = meta ? [.alt] : []
        let character: Character
        if ctrl {
            guard let base = single(charactersIgnoringModifiers) else { return nil }
            keyModifiers.insert(.ctrl)
            character = base
        } else if let typed = single(characters), typed.isASCII {
            character = typed
        } else if let base = single(charactersIgnoringModifiers) {
            character = modifiers.contains(.shift) ? Character(base.uppercased()) : base
        } else {
            return nil
        }
        // An unmapped chord (Ctrl-1, Ctrl-é) goes back to SwiftTerm rather than vanishing here.
        let bytes = encode(KeyStroke(.char(character), keyModifiers), applicationCursor: false)
        return bytes.isEmpty ? nil : bytes
    }

    /// Named keys arrive as multi-character strings such as `UIKeyInputUpArrow`.
    private static func single(_ text: String) -> Character? {
        guard text.count == 1, let c = text.first, !c.isWhitespace || c == " " else { return nil }
        return c
    }
}
