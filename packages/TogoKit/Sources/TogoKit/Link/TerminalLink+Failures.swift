import Foundation
@preconcurrency import LocalAuthentication

extension TerminalLink {
    /// Failures the user has to act on; retrying them would only repeat the prompt or the block.
    static func isPermanent(_ error: any Error) -> Bool {
        if CredentialAccess.isTemporary(error) { return false }
        return switch error {
        case SSHError.hostKeyChanged, SSHError.hostKeyRejected, SSHError.authFailed, SSHError.cancelled:
            true
        // Reaches here only when Mosh was chosen explicitly; Auto falls back to the next kind.
        // No UDP reply means a firewall or network in between blocks Mosh; retrying cannot help.
        case MoshBootstrapError.serverMissing, MoshConnectError.noUDPReply, TransportAddressError.linkLocalOnly:
            true
        // Thrown only when no kind may run for this host: an explicit kind that is
        // unavailable, or Auto with every kind skipped.
        case is ConnectorError:
            true
        // Rejected authentication needs the user; temporary access failures retry above.
        case is SecretStoreError, is LAError:
            true
        default:
            false
        }
    }

    /// The machine did not answer at all, after the wake probe already waited for it. On the
    /// first attach that is reported at once; a machine that answers but whose helper or
    /// session is not ready yet is retried with backoff like a dropped link.
    static func isUnreachable(_ error: any Error) -> Bool {
        switch error {
        case SSHError.unreachable, SSHError.timeout: true
        default: false
        }
    }
}
