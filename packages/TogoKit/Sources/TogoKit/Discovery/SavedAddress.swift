import Foundation

/// Tidies an address saved before discovery preferred host names, such as `fe80::1c2b%en0`.
public enum SavedAddress {
    public enum Tidy: Equatable, Sendable {
        case unchanged
        case replaced(String)
        /// A link-local address with no name to replace it. It is kept, because with its zone
        /// it still connects on the network it was found on; the form asks for a name.
        case needsAttention
    }

    /// `hostName` looks up the name behind an address, such as `studio.local.`.
    public static func tidied(_ host: String, hostName: @Sendable (String) async -> String?) async -> Tidy {
        guard host.contains("%") else { return .unchanged }
        let bare = DiscoveredAddress.stripZone(host)
        if !DiscoveredAddress.isLinkLocalIPv6(bare) { return .replaced(bare) }
        if let name = DiscoveredAddress.usableName(await hostName(host)) { return .replaced(name) }
        return .needsAttention
    }

    /// Whether a typed or saved address carries an interface zone, which only works on one network.
    public static func hasZone(_ host: String) -> Bool {
        host.contains("%")
    }

    /// The `.local` name the machine answers to for `address`, from a reverse lookup that goes
    /// through mDNS on the local network. Nil when nothing answers.
    public static func reverseName(_ address: String) async -> String? {
        await Task.detached {
            var hints = addrinfo()
            hints.ai_flags = AI_NUMERICHOST
            var info: UnsafeMutablePointer<addrinfo>?
            guard getaddrinfo(address, nil, &hints, &info) == 0, let first = info else { return nil }
            defer { freeaddrinfo(info) }
            var name = [CChar](repeating: 0, count: Int(NI_MAXHOST))
            guard getnameinfo(first.pointee.ai_addr, first.pointee.ai_addrlen, &name, socklen_t(name.count), nil, 0, NI_NAMEREQD) == 0
            else { return nil }
            let bytes = name.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }
            return String(decoding: bytes, as: UTF8.self)
        }.value
    }
}

extension HostStore {
    /// Rewrites saved addresses that carry an interface zone: a routable one loses the zone, a
    /// link-local one becomes the machine's name when it answers to one. Returns the machines
    /// left on a link-local address, so the app can ask for a name. Safe to run every launch;
    /// records without a zone are not touched.
    public func tidyAddresses(
        hostName: @Sendable (String) async -> String? = SavedAddress.reverseName
    ) async -> [UUID] {
        var flagged: [UUID] = []
        for record in await hosts() where SavedAddress.hasZone(record.profile.endpoint.host) {
            let saved = record.profile.endpoint.host
            switch await SavedAddress.tidied(saved, hostName: hostName) {
            case .unchanged:
                break
            case .replaced(let host):
                // The lookup can take seconds, and the machine may have been edited, learned
                // routes or been removed meanwhile: only the address changes, on the record as
                // it is now, and a removed or readdressed machine is left alone.
                guard var current = await hosts().first(where: { $0.id == record.id }),
                      current.profile.endpoint.host == saved else { continue }
                current.profile.endpoint.host = host
                await save(current)
            case .needsAttention:
                flagged.append(record.id)
            }
        }
        return flagged
    }
}
