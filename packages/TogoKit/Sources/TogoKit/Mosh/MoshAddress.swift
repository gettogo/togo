import Foundation
import Synchronization

/// SSH reached the machine only over an IPv6 link-local address with no interface to scope
/// it, and no other address of the machine is known, so Mosh or ET has nowhere to send.
public enum TransportAddressError: Error, Equatable, LocalizedError {
    case linkLocalOnly(host: String)

    public var errorDescription: String? {
        switch self {
        case .linkLocalOnly(let host):
            String(localized: "\(host) was reached only over a link-local IPv6 address, which Mosh and ET cannot use. Connect with SSH, or add the machine's IPv4 address.", bundle: .module)
        }
    }
}

/// How mosh-server picks the address it listens on.
public enum MoshBind: Sendable, Equatable {
    /// `-s`: the address the SSH connection reached, which the client also sends to.
    case sshAddress
    /// `-i <address>`: another of the machine's own addresses, chosen by the client.
    case address(String)
}

/// Where the Mosh client sends its datagrams and how the server must bind to receive them.
public struct MoshTarget: Sendable, Equatable {
    public var host: String
    public var bind: MoshBind

    public init(host: String, bind: MoshBind) {
        self.host = host
        self.bind = bind
    }
}

/// The address an SSH connection reached, and the interface it went out on when that
/// address is only meaningful on one link.
public struct ReachedAddress: Sendable, Equatable {
    public var ip: String
    public var interface: String?

    public init(ip: String, interface: String? = nil) {
        self.ip = ip
        self.interface = interface
    }
}

/// Picks the address Mosh's UDP flow uses.
///
/// A `.local` name often resolves to the machine's IPv6 link-local address (fe80::/10). SSH
/// over it works because the socket carries the interface, but the address alone names no
/// link: datagrams to it go nowhere, and a server bound to it with `-s` hears nothing on its
/// other addresses. So a link-local reach is swapped for one of the machine's IPv4 or global
/// addresses, with the server told to bind there; failing that, the link-local address with
/// its interface (`fe80::1%en0`); failing that, nothing, and Mosh is skipped.
public enum MoshAddress {
    public static func choose(
        reached: ReachedAddress?, fallbackHost: String, routes: [HostRoute], resolved: [String]
    ) -> MoshTarget? {
        let ip = reached?.ip ?? fallbackHost
        guard isLinkLocalIPv6(ip) else { return MoshTarget(host: ip, bind: .sshAddress) }
        // Addresses the name resolved to on this link come first, then the machine's own
        // LAN addresses as its helper reported them, then its tailnet addresses.
        let candidates = resolved
            + routes.filter { $0.kind == .lan }.map(\.host)
            + routes.filter { $0.kind == .tailscale }.map(\.host)
        if let v4 = candidates.first(where: isIPv4) { return MoshTarget(host: v4, bind: .address(v4)) }
        if let v6 = candidates.first(where: isRoutableIPv6) { return MoshTarget(host: v6, bind: .address(v6)) }
        if ip.contains("%") { return MoshTarget(host: ip, bind: .sshAddress) }
        if let interface = reached?.interface, !interface.isEmpty {
            return MoshTarget(host: "\(ip)%\(interface)", bind: .sshAddress)
        }
        return nil
    }

    public static func isIPv4(_ host: String) -> Bool {
        var address = in_addr()
        return inet_pton(AF_INET, host, &address) == 1
    }

    /// fe80::/10, with or without a `%zone` suffix.
    public static func isLinkLocalIPv6(_ host: String) -> Bool {
        guard let bytes = ipv6Bytes(host) else { return false }
        return bytes[0] == 0xFE && bytes[1] & 0xC0 == 0x80
    }

    /// An IPv6 address that names the same machine from any link: not link-local,
    /// loopback, multicast or unspecified.
    static func isRoutableIPv6(_ host: String) -> Bool {
        guard !host.contains("%"), let bytes = ipv6Bytes(host) else { return false }
        if bytes[0] == 0xFF || (bytes[0] == 0xFE && bytes[1] & 0xC0 == 0x80) { return false }
        let leadingZero = bytes.dropLast().allSatisfy { $0 == 0 }
        return !(leadingZero && (bytes[15] == 0 || bytes[15] == 1))
    }

    private static func ipv6Bytes(_ host: String) -> [UInt8]? {
        let bare = host.split(separator: "%", maxSplits: 1).first.map(String.init) ?? host
        var address = in6_addr()
        guard inet_pton(AF_INET6, bare, &address) == 1 else { return nil }
        return withUnsafeBytes(of: address) { Array($0) }
    }

    /// The IPv4 addresses `name` resolves to, or none after `timeout`. An mDNS name the SSH
    /// connect just resolved is normally answered from cache.
    public static func resolveIPv4(_ name: String, timeout: Duration) async -> [String] {
        guard name.contains(where: \.isLetter), !name.contains(":") else { return [] }
        let slot = ResolveSlot()
        return await withCheckedContinuation { continuation in
            slot.set(continuation)
            Thread.detachNewThread { slot.finish(lookupIPv4(name)) }
            Task {
                try? await Task.sleep(for: timeout)
                slot.finish([])
            }
        }
    }

    private static func lookupIPv4(_ name: String) -> [String] {
        var hints = addrinfo()
        hints.ai_family = AF_INET
        hints.ai_socktype = SOCK_DGRAM
        var list: UnsafeMutablePointer<addrinfo>?
        guard getaddrinfo(name, nil, &hints, &list) == 0, let first = list else { return [] }
        defer { freeaddrinfo(first) }
        var out: [String] = []
        var node: UnsafeMutablePointer<addrinfo>? = first
        while let entry = node {
            if let raw = entry.pointee.ai_addr, entry.pointee.ai_family == AF_INET {
                var address = raw.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { $0.pointee.sin_addr }
                var buffer = [CChar](repeating: 0, count: Int(INET_ADDRSTRLEN))
                if inet_ntop(AF_INET, &address, &buffer, socklen_t(buffer.count)) != nil {
                    let text = String(decoding: buffer.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }, as: UTF8.self)
                    if !out.contains(text) { out.append(text) }
                }
            }
            node = entry.pointee.ai_next
        }
        return out
    }
}

/// Resumes a lookup's continuation once, from the lookup thread or the timeout.
private final class ResolveSlot: Sendable {
    private let continuation = Mutex<CheckedContinuation<[String], Never>?>(nil)

    func set(_ value: CheckedContinuation<[String], Never>) {
        continuation.withLock { $0 = value }
    }

    func finish(_ value: [String]) {
        continuation.withLock { $0.take() }?.resume(returning: value)
    }
}
