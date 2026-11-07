import Darwin
import Foundation

/// Whether this device is on a tailnet right now: the Tailscale app's tunnel is a `utun`
/// interface holding an address in 100.64.0.0/10.
public enum PhoneTailnet {
    public struct Address: Equatable, Sendable {
        public var interface: String
        /// The IPv4 address in host byte order.
        public var ipv4: UInt32

        public init(interface: String, ipv4: UInt32) {
            self.interface = interface
            self.ipv4 = ipv4
        }
    }

    public static func isConnected() -> Bool {
        isTailnet(addresses())
    }

    public static func isTailnet(_ addresses: [Address]) -> Bool {
        addresses.contains { $0.interface.hasPrefix("utun") && $0.ipv4 & 0xFFC0_0000 == 0x6440_0000 }
    }

    /// Every IPv4 address on an interface that is up.
    static func addresses() -> [Address] {
        var first: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&first) == 0 else { return [] }
        defer { freeifaddrs(first) }
        var found: [Address] = []
        var cursor = first
        while let entry = cursor?.pointee {
            defer { cursor = entry.ifa_next }
            guard entry.ifa_flags & UInt32(IFF_UP) != 0, let address = entry.ifa_addr,
                  address.pointee.sa_family == sa_family_t(AF_INET) else { continue }
            let ipv4 = address.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { UInt32(bigEndian: $0.pointee.sin_addr.s_addr) }
            found.append(Address(interface: String(cString: entry.ifa_name), ipv4: ipv4))
        }
        return found
    }
}
