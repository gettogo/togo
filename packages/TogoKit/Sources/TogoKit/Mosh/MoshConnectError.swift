import Foundation

public enum MoshConnectError: Error, Equatable, LocalizedError {
    /// mosh-server started but no datagram came back, usually because a firewall drops UDP.
    case noUDPReply(address: String)

    public var errorDescription: String? {
        switch self {
        case .noUDPReply(let address):
            "mosh-server started, but nothing came back over UDP from \(address). "
                + "A firewall may be blocking UDP ports 60000 to 61000."
        }
    }
}
