internal import SwiftTerm
import UIKit

extension SwiftTermEngine {
    /// A key's bytes in the terminal's current cursor and extended key modes.
    func bytes(for key: KeyStroke) -> [UInt8] {
        let applicationCursor = terminalView.terminalInputStateSnapshot()?.applicationCursor ?? false
        return KeyEncoder.encode(key, applicationCursor: applicationCursor, mode: extendedKeyMode)
    }

    /// Typed text, sent as if keyed: never bracketed, unlike `paste`.
    func sendText(_ text: String) {
        write(Array(text.utf8))
    }

    func endEditing() {
        _ = terminalView.resignFirstResponder()
    }
}
