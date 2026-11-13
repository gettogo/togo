import Foundation
@preconcurrency import LocalAuthentication

extension HostMonitor {
    static let backoff = TerminalLink.backoff
    static let failuresBeforeOffline = 3
    static let offlineRetry: Duration = .seconds(8)
    static let pollInterval: Duration = .seconds(5)
    /// Long enough to answer a notification from the lock screen without reconnecting.
    static let backgroundGrace: Duration = .seconds(30)

    /// Errors only the user can resolve. A helper that is missing or too old cannot
    /// give a snapshot until it is installed or updated, so retrying is pointless too.
    static func heldReach(for error: any Error, helper: HelperPresence?) -> HostSnapshot.Reach? {
        if CredentialAccess.isTemporary(error) { return nil }
        switch error {
        case SSHError.authFailed, SSHError.cancelled, SSHError.hostKeyRejected:
            return .authNeeded
        case SSHError.hostKeyChanged:
            return .hostKeyChanged
        case KeyError.missing:
            return .keyMissing
        // Rejected authentication still needs the user; temporary access failures retry above.
        case is SecretStoreError, is LAError:
            return .authNeeded
        default:
            break
        }
        switch helper {
        case .missing?, .outdated?, .newer?: return .failed(error.localizedDescription)
        case .unknown?, .present?, nil: return nil
        }
    }

    /// Offline only when the machine itself did not answer. A machine that answers but whose
    /// helper does not, as while it restarts, reads as failed with the reason, and the
    /// attempts go on.
    static func awayReach(for error: any Error) -> HostSnapshot.Reach {
        switch error {
        case SSHError.unreachable, SSHError.timeout:
            .offline(error.localizedDescription)
        default:
            .failed(plainReason(error))
        }
    }

    /// Togo's own errors word themselves; a raw transport error from below (NIO, POSIX) would
    /// read as "The operation couldn't be completed (NIOCore.ChannelError error 6)", so it
    /// says what happened instead.
    static func plainReason(_ error: any Error) -> String {
        switch error {
        case is SSHError, is HelperError, is HelperSourceError, is KeyError:
            error.localizedDescription
        default:
            SSHError.channelClosed(exitStatus: nil).localizedDescription
        }
    }
}
