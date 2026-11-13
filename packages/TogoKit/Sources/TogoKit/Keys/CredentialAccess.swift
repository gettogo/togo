@preconcurrency import LocalAuthentication
import Security

/// Access can be interrupted even though the saved credential is still valid.
/// Keep these failures on the existing reconnect path instead of asking for sign-in.
enum CredentialAccess {
    static func isTemporary(_ error: any Error) -> Bool {
        switch error {
        case let error as SecretStoreError:
            [errSecNotAvailable, errSecInteractionNotAllowed].contains(error.status)
        case let error as LAError:
            [.systemCancel, .appCancel, .notInteractive, .invalidContext].contains(error.code)
        default:
            false
        }
    }
}
