import SwiftUI
import UIKit

extension View {
    /// For a secure field whose value Togo passes to a machine or keeps in its own keychain: a
    /// machine password, a key passphrase, a keyboard-interactive answer.
    ///
    /// With `.password`, iOS reads the field as a website login and offers to save it once the
    /// form leaves the screen. Togo has no associated domain, so the system and password managers
    /// can only refuse ("no permission to save"). That happens even though the User field has no
    /// `.username` type: `.password` on its own is enough. `.oneTimeCode` is the content type iOS
    /// does not save from. Paste still works, and AutoFill in the edit menu (Passwords, or a password
    /// manager) still fills the field.
    func keptSecret() -> some View {
        textContentType(.oneTimeCode)
    }
}
