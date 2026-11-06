import SwiftUI
import TogoTerminal

private struct HardwareKeyboardKey: EnvironmentKey {
    static var defaultValue: KeyboardPresence { MainActor.assumeIsolated { KeyboardPresence.shared } }
}

extension EnvironmentValues {
    /// Whether a hardware keyboard is attached right now, from GameController's connect and
    /// disconnect notifications. Not size class, not idiom: an iPad has none until one is paired.
    /// Every shortcut hint, command chip and keyboard-only line reads this.
    var hardwareKeyboard: KeyboardPresence {
        get { self[HardwareKeyboardKey.self] }
        set { self[HardwareKeyboardKey.self] = newValue }
    }
}

extension KeyboardPresence {
    /// A shortcut hint: the text when a keyboard is attached, otherwise nil.
    func hint(_ text: @autoclosure () -> String) -> String? {
        hardwareKeyboardConnected ? text() : nil
    }
}
