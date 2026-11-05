import Foundation

/// Whether an address belongs to a Tailscale network: a MagicDNS name, the CGNAT range
/// Tailscale assigns (100.64.0.0/10) or its IPv6 prefix (fd7a:115c:a1e0::/48).
public enum TailnetHint {
    public static func isTailnet(_ host: String) -> Bool {
        var name = host.lowercased()
        if name.hasSuffix(".") { name.removeLast() }
        if name.hasSuffix(".ts.net") { return true }
        if let octets = ipv4(name) {
            return octets[0] == 100 && octets[1] & 0xC0 == 64
        }
        if let bytes = ipv6(name) {
            return bytes.starts(with: [0xFD, 0x7A, 0x11, 0x5C, 0xA1, 0xE0])
        }
        return false
    }

    private static func ipv4(_ text: String) -> [UInt8]? {
        let parts = text.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 4 else { return nil }
        let octets = parts.compactMap { $0.allSatisfy(\.isASCII) ? UInt8($0) : nil }
        return octets.count == 4 ? octets : nil
    }

    private static func ipv6(_ text: String) -> [UInt8]? {
        var address = Substring(text)
        if address.hasPrefix("["), address.hasSuffix("]") { address = address.dropFirst().dropLast() }
        if let zone = address.firstIndex(of: "%") { address = address[..<zone] }
        var storage = in6_addr()
        guard inet_pton(AF_INET6, String(address), &storage) == 1 else { return nil }
        return withUnsafeBytes(of: storage) { Array($0) }
    }
}
