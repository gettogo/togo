import Foundation

/// Picks the address a discovered machine is saved under. The Bonjour host name (`studio.local`)
/// comes first because it survives the machine changing networks, then an IPv4 address, then
/// an IPv6 address that routes. A link-local address (`fe80::`, `169.254.`) is never chosen: it
/// only works with an interface zone such as `%en0`, which means nothing typed into the form.
public enum DiscoveredAddress {
    public static func preferred(hostName: String?, addresses: [String]) -> String? {
        if let name = usableName(hostName) { return name }
        let cleaned = addresses.map(stripZone).filter { !$0.isEmpty }
        if let v4 = cleaned.first(where: { isIPv4($0) && !$0.hasPrefix("169.254.") }) { return v4 }
        return cleaned.first { $0.contains(":") && !isLinkLocalIPv6($0) }
    }

    /// What the discovery list shows for a machine: the preferred address as soon as there is
    /// one. Once the host name lookup has finished without one, an empty address, so the machine
    /// is still listed and the form asks for its address. Nil means keep waiting.
    public static func listed(hostName: String?, addresses: [String], lookupFinished: Bool) -> String? {
        if let address = preferred(hostName: hostName, addresses: addresses) { return address }
        return lookupFinished ? "" : nil
    }

    static func usableName(_ name: String?) -> String? {
        guard var name = name?.trimmingCharacters(in: .whitespaces) else { return nil }
        while name.hasSuffix(".") { name.removeLast() }
        guard !name.isEmpty, !name.contains(":"), !name.contains("%"), !isIPv4(name) else { return nil }
        return name
    }

    static func stripZone(_ address: String) -> String {
        String(address.prefix { $0 != "%" })
    }

    private static func isIPv4(_ text: String) -> Bool {
        let parts = text.split(separator: ".", omittingEmptySubsequences: false)
        return parts.count == 4 && parts.allSatisfy { UInt8($0) != nil }
    }

    /// `fe80::/10`: the first group runs from fe80 to febf.
    static func isLinkLocalIPv6(_ address: String) -> Bool {
        guard let first = address.split(separator: ":", omittingEmptySubsequences: false).first,
              let value = UInt16(first, radix: 16) else { return false }
        return value & 0xFFC0 == 0xFE80
    }
}
