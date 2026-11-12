import UIKit

/// Where an imported key's text came from. The import itself is identical for both.
enum KeyImportSource: Equatable {
    case file
    case clipboard(ClipboardMark)
}

/// What the pasteboard looked like when the user pasted. Comparing change counts tells
/// whether it still holds the pasted key without reading its content, so iOS cannot show
/// an "Allow Paste" prompt for the check.
struct ClipboardMark: Equatable {
    let changeCount: Int
}

protocol KeyPasteboard {
    var changeCount: Int { get }
    func clear()
}

struct SystemKeyPasteboard: KeyPasteboard {
    var changeCount: Int { UIPasteboard.general.changeCount }
    func clear() { UIPasteboard.general.items = [] }
}

extension ClipboardMark {
    init(_ pasteboard: some KeyPasteboard) {
        self.init(changeCount: pasteboard.changeCount)
    }

    /// Empties the pasteboard only if nothing was copied since the paste, so unrelated
    /// content the user copied afterwards stays. Returns whether it cleared.
    @discardableResult
    func clearIfUnchanged(on pasteboard: some KeyPasteboard) -> Bool {
        guard pasteboard.changeCount == changeCount else { return false }
        pasteboard.clear()
        return true
    }
}
