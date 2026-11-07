import Foundation

/// One address a machine answers on. The helper reports most of them after each connect;
/// an address the user typed is kept as a manual route and never replaced by a report.
public struct HostRoute: Codable, Hashable, Sendable, Identifiable {
    public enum Kind: String, Codable, Sendable {
        /// The machine's mDNS name, such as `studio.local`.
        case local
        /// A private address on the machine's own network.
        case lan
        /// A Tailscale MagicDNS name or tailnet address.
        case tailscale
        /// Anything else, such as a public name.
        case other
    }

    public var host: String
    public var kind: Kind
    public var manual: Bool

    public var id: String { host.lowercased() }

    public init(host: String, kind: Kind, manual: Bool = false) {
        self.host = host
        self.kind = kind
        self.manual = manual
    }

    /// An address the user typed, with its kind read from its shape.
    public static func manual(_ host: String) -> HostRoute {
        HostRoute(host: host, kind: kind(of: host), manual: true)
    }

    /// The route a helper reported, or nil for a kind this build does not know.
    public init?(reported route: HostInfoRoute) {
        let host = route.host.trimmingCharacters(in: .whitespaces)
        guard !host.isEmpty else { return nil }
        switch route.kind {
        case "mdns": self.init(host: host, kind: .local)
        case "lan": self.init(host: host, kind: .lan)
        case "tailscale": self.init(host: host, kind: .tailscale)
        default: return nil
        }
    }

    public static func kind(of host: String) -> Kind {
        var name = host.lowercased()
        if name.hasSuffix(".") { name.removeLast() }
        if TailnetHint.isTailnet(name) { return .tailscale }
        if name.hasSuffix(".local") { return .local }
        if isPrivate(name) { return .lan }
        return .other
    }

    var isIPv6: Bool { host.contains(":") }

    var isName: Bool { !isIPv6 && host.contains(where: \.isLetter) }

    /// 10/8, 172.16/12, 192.168/16, link-local IPv4, and IPv6 ULA (fc00::/7) or link-local.
    private static func isPrivate(_ host: String) -> Bool {
        let parts = host.split(separator: ".", omittingEmptySubsequences: false)
        if parts.count == 4 {
            let octets = parts.compactMap { UInt8($0) }
            guard octets.count == 4 else { return false }
            switch (octets[0], octets[1]) {
            case (10, _), (192, 168), (169, 254): return true
            case (172, let b): return b & 0xF0 == 16
            default: return false
            }
        }
        guard host.contains(":"), let first = host.split(separator: ":", omittingEmptySubsequences: false).first,
              let value = UInt16(first.isEmpty ? "0" : first, radix: 16) else { return false }
        return value & 0xFE00 == 0xFC00 || value & 0xFFC0 == 0xFE80
    }
}

/// Which routes to try, in what order, and how a report from the helper updates them.
public enum RoutePlan {
    /// At most this many routes are raced, so a machine with many addresses does not open
    /// a connection per address.
    public static let limit = 8

    /// The last route that worked first, then the local network (mDNS name, IPv4, IPv6),
    /// then other names, then Tailscale (name before addresses). The saved address always
    /// takes part. Duplicates are dropped without regard to case.
    public static func ordered(_ routes: [HostRoute], saved: String, last: String?) -> [String] {
        var all = routes
        if !all.contains(where: { $0.id == saved.lowercased() }) { all.append(.manual(saved)) }
        let ranked = all.enumerated().sorted { a, b in
            let (ra, rb) = (rank(a.element), rank(b.element))
            return ra != rb ? ra < rb : a.offset < b.offset
        }.map(\.element)
        var seen = Set<String>()
        var hosts: [String] = []
        if let last, all.contains(where: { $0.id == last.lowercased() }) {
            seen.insert(last.lowercased())
            hosts.append(last)
        }
        for route in ranked where seen.insert(route.id).inserted {
            hosts.append(route.host)
        }
        return Array(hosts.prefix(limit))
    }

    /// The routes to race for `profile`. A jump host means one route: the saved address,
    /// reached through the jump.
    public static func candidates(_ profile: HostProfile, routes: [HostRoute]? = nil, last: String? = nil) -> [String] {
        guard profile.jumps.isEmpty else { return [profile.endpoint.host] }
        return ordered(routes ?? profile.routes, saved: profile.endpoint.host, last: last ?? profile.lastRoute)
    }

    private static func rank(_ route: HostRoute) -> Int {
        switch route.kind {
        case .local: 0
        case .lan: route.isIPv6 ? 2 : 1
        case .other: 3
        case .tailscale: route.isName ? 4 : (route.isIPv6 ? 6 : 5)
        }
    }

    /// `routes` with every reported route replaced by `reported`. Manual routes stay, and a
    /// reported address the user also typed stays manual.
    public static func refreshed(_ routes: [HostRoute], reported: [HostRoute]) -> [HostRoute] {
        let manual = routes.filter(\.manual)
        let manualIDs = Set(manual.map(\.id))
        var seen = manualIDs
        let fresh = reported.filter { seen.insert($0.id).inserted }
        return manual + fresh
    }

    /// The profile with its routes updated from a helper report and the route that just
    /// worked; nil when nothing changed, so a caller stores only real changes.
    public static func learned(_ profile: HostProfile, reported: [HostRoute]?, last: String?) -> HostProfile? {
        var updated = profile
        if let reported { updated.routes = refreshed(profile.routes, reported: reported) }
        if let last { updated.lastRoute = last }
        return updated == profile ? nil : updated
    }

    /// `routes` without the learned LAN addresses in `mismatched`. Names, Tailscale routes and
    /// addresses the user typed stay: only a reused LAN address is dropped on its own.
    public static func droppingMismatched(_ routes: [HostRoute], _ mismatched: Set<String>) -> [HostRoute] {
        guard !mismatched.isEmpty else { return routes }
        return routes.filter { !($0.kind == .lan && !$0.manual && !$0.isName && mismatched.contains($0.id)) }
    }

    /// How the route in use reads to a person.
    public enum Place: Equatable, Sendable {
        case tailscale
        case localNetwork
        case address(String)
    }

    public static func place(of host: String, in routes: [HostRoute]) -> Place {
        let kind = routes.first { $0.id == host.lowercased() }?.kind ?? HostRoute.kind(of: host)
        switch kind {
        case .tailscale: return .tailscale
        case .local, .lan: return .localNetwork
        case .other: return .address(host)
        }
    }
}
