import UIKit

/// How the remote asked for modified keys that legacy encoding cannot tell apart, such as
/// Shift-Enter, which is a bare CR without one of these.
enum ExtendedKeyMode: Equatable, Sendable {
    case legacy
    /// xterm `modifyOtherKeys` level 2: `CSI 27 ; mod ; code ~`.
    case modifyOtherKeys
    /// The kitty keyboard protocol with at least the disambiguate flag: `CSI code ; mod u`.
    case kitty
}

/// Follows xterm's `XTMODKEYS` requests (`CSI > 4 ; Pv m`) in the output stream.
///
/// SwiftTerm parses and drops these, so the engine reads them on the way in. The scan keeps
/// its state between feeds because a sequence can arrive split across reads.
struct ModifyKeysScanner {
    private(set) var level = 0

    private enum State { case ground, escape, csi, modifierRequest }
    private var state = State.ground
    private var parameters: [UInt8] = []
    private static let maximumParameterBytes = 16

    mutating func scan(_ bytes: ArraySlice<UInt8>) {
        var rest = bytes
        while !rest.isEmpty {
            // Most output is text; skip straight to the next ESC rather than stepping each byte.
            if state == .ground {
                guard let next = rest.firstIndex(of: 0x1B) else { return }
                rest = rest[next...]
            }
            step(rest.removeFirst())
        }
    }

    private mutating func step(_ byte: UInt8) {
        if byte == 0x1B {
            state = .escape
            return
        }
        switch state {
        case .ground:
            break
        case .escape:
            if byte == UInt8(ascii: "c") { level = 0 }
            state = byte == UInt8(ascii: "[") ? .csi : .ground
        case .csi:
            parameters.removeAll(keepingCapacity: true)
            state = byte == UInt8(ascii: ">") ? .modifierRequest : .ground
        case .modifierRequest:
            if (byte >= UInt8(ascii: "0") && byte <= UInt8(ascii: "9")) || byte == UInt8(ascii: ";") {
                if parameters.count < Self.maximumParameterBytes {
                    parameters.append(byte)
                } else {
                    state = .ground
                }
                return
            }
            if byte == UInt8(ascii: "m") { apply(disable: false) }
            if byte == UInt8(ascii: "n") { apply(disable: true) }
            state = .ground
        }
    }

    /// `CSI > m` resets every resource, `CSI > 4 m` resets this one, `CSI > 4 n` disables it.
    private mutating func apply(disable: Bool) {
        let values = String(decoding: parameters, as: UTF8.self)
            .split(separator: ";", omittingEmptySubsequences: false)
            .map { Int($0) }
        guard let resource = values.first else { return }
        guard resource != nil else {
            if !disable { level = 0 }
            return
        }
        guard resource == 4 else { return }
        level = disable ? 0 : (values.count > 1 ? values[1] ?? 0 : 0)
    }

    var mode: ExtendedKeyMode { level >= 2 ? .modifyOtherKeys : .legacy }
}

extension KeyEncoder {
    /// Encodes with the remote's extended key mode, falling back to xterm's legacy bytes for
    /// keys the mode leaves alone (unmodified keys, Shift with a printable, cursor and
    /// function keys, whose legacy forms already carry modifiers).
    static func encode(_ k: KeyStroke, applicationCursor: Bool, mode: ExtendedKeyMode) -> [UInt8] {
        extended(k, mode: mode) ?? encode(k, applicationCursor: applicationCursor)
    }

    /// A hardware press under `modifyOtherKeys`, or nil to take the usual path. Kitty presses
    /// never come here: SwiftTerm reports those itself.
    static func extendedHardware(
        charactersIgnoringModifiers: String, modifiers: UIKeyModifierFlags, keyCode: Int, optionAsMeta: Bool,
        mode: ExtendedKeyMode
    ) -> [UInt8]? {
        guard mode == .modifyOtherKeys, !modifiers.contains(.command) else { return nil }
        var keyModifiers: KeyModifiers = []
        if modifiers.contains(.shift) { keyModifiers.insert(.shift) }
        if modifiers.contains(.control) { keyModifiers.insert(.ctrl) }
        if optionAsMeta && modifiers.contains(.alternate) { keyModifiers.insert(.alt) }
        let key: TerminalKey
        switch UIKeyboardHIDUsage(rawValue: keyCode) {
        case .keyboardReturnOrEnter, .keypadEnter: key = .enter
        case .keyboardTab: key = .tab
        case .keyboardDeleteOrBackspace: key = .backspace
        case .keyboardEscape: key = .escape
        default:
            guard charactersIgnoringModifiers.count == 1, let c = charactersIgnoringModifiers.first else { return nil }
            key = .char(c)
        }
        return extended(KeyStroke(key, keyModifiers), mode: mode)
    }

    private static func extended(_ k: KeyStroke, mode: ExtendedKeyMode) -> [UInt8]? {
        let mods = k.modifiers
        let parameter = 1 + (mods.contains(.shift) ? 1 : 0) + (mods.contains(.alt) ? 2 : 0)
            + (mods.contains(.ctrl) ? 4 : 0)
        let shiftedPrintable: Bool = {
            if case .char = k.key { return mods == [.shift] }
            return false
        }()
        switch mode {
        case .legacy:
            return nil
        case .kitty:
            if k.key == .escape && mods.isEmpty { return csi("27u") }
            guard parameter > 1, !shiftedPrintable, let code = kittyCode(k.key) else { return nil }
            return csi("\(code);\(parameter)u")
        case .modifyOtherKeys:
            guard parameter > 1, !shiftedPrintable, let code = xtermCode(k) else { return nil }
            return csi("27;\(parameter);\(code)~")
        }
    }

    private static func csi(_ body: String) -> [UInt8] {
        [0x1B, UInt8(ascii: "[")] + Array(body.utf8)
    }

    private static func namedCode(_ key: TerminalKey) -> UInt32? {
        switch key {
        case .enter: 13
        case .tab: 9
        case .backspace: 127
        case .escape: 27
        default: nil
        }
    }

    /// Kitty reports the unshifted key and carries Shift in the modifier.
    private static func kittyCode(_ key: TerminalKey) -> UInt32? {
        if case .char(let c) = key {
            let base = String(c).lowercased()
            return base.unicodeScalars.count == 1 ? base.unicodeScalars.first?.value : nil
        }
        return namedCode(key)
    }

    /// xterm reports the character the key produced, so Shift has already applied.
    private static func xtermCode(_ k: KeyStroke) -> UInt32? {
        if case .char(let c) = k.key {
            let typed = k.modifiers.contains(.shift) ? String(c).uppercased() : String(c)
            return typed.unicodeScalars.count == 1 ? typed.unicodeScalars.first?.value : nil
        }
        return namedCode(k.key)
    }
}
