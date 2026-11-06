import Foundation
import Network
import Synchronization

/// Where to send a magic packet for a machine: its network card's address and the broadcast
/// addresses of the networks it was last seen on. Public facts about the machine, no secrets.
public struct WakeTarget: Codable, Hashable, Sendable {
    public var mac: String
    public var broadcast: [String]
    /// Whether the machine said it answers wake packets; nil when it could not tell.
    public var enabled: Bool?

    public init(mac: String, broadcast: [String], enabled: Bool? = nil) {
        self.mac = mac
        self.broadcast = broadcast
        self.enabled = enabled
    }

    /// The first physical interface in `info` with a valid hardware address and a private
    /// IPv4 address; nil when the machine reports none.
    public init?(from info: HostInfo) {
        for interface in info.interfaces.prefix(WakeOnLAN.interfaceLimit) where !Self.isVirtual(interface.name) {
            guard let mac = WakeOnLAN.normalized(mac: interface.mac) else { continue }
            let broadcast = WakeOnLAN.broadcasts(for: Array(interface.ipv4.prefix(WakeOnLAN.addressLimit)))
            guard broadcast.count > 1 else { continue }
            self.init(mac: mac, broadcast: broadcast, enabled: info.wakeOnLan)
            return
        }
        return nil
    }

    private static let virtualPrefixes = [
        "lo", "utun", "awdl", "llw", "bridge", "br-", "br0", "docker", "veth", "vmnet", "vboxnet", "virbr",
        "tun", "tap", "gif", "stf", "anpi", "ap", "wg", "zt", "tailscale", "vnet", "vethernet",
    ]

    static func isVirtual(_ name: String) -> Bool {
        let lowered = name.lowercased()
        return virtualPrefixes.contains { lowered.hasPrefix($0) }
    }
}

public enum WakeError: Error, Equatable, Sendable {
    case badMAC
    case noBroadcast
    case failed
}

/// Wake-on-LAN magic packets over UDP port 9.
public enum WakeOnLAN {
    static let port: NWEndpoint.Port = 9
    static let limitedBroadcast = "255.255.255.255"
    static let timeout: Duration = .seconds(2)
    /// A machine's report is untrusted: only this much of it is read, stored or sent to.
    static let interfaceLimit = 8
    static let addressLimit = 4
    static let broadcastLimit = 4

    /// The 102-byte magic packet: six 0xFF bytes and the address sixteen times.
    public static func packet(mac: String) throws -> Data {
        guard let octets = octets(of: mac) else { throw WakeError.badMAC }
        var data = Data(repeating: 0xFF, count: 6)
        for _ in 0..<16 { data.append(contentsOf: octets) }
        return data
    }

    /// The address as lowercase colon-separated hex, or nil when it is not a usable unicast address.
    static func normalized(mac: String) -> String? {
        octets(of: mac).map { $0.map { String(format: "%02x", $0) }.joined(separator: ":") }
    }

    /// Six hex octets joined by one kind of `:` or `-`. All-zero, broadcast and multicast
    /// addresses are not machines.
    private static func octets(of mac: String) -> [UInt8]? {
        let separator: Character
        if mac.contains(":") && !mac.contains("-") { separator = ":" } else if mac.contains("-") && !mac.contains(":") { separator = "-" } else { return nil }
        let parts = mac.split(separator: separator, omittingEmptySubsequences: false)
        guard parts.count == 6 else { return nil }
        var octets: [UInt8] = []
        for part in parts {
            guard part.utf8.count == 2, part.utf8.allSatisfy(\.isHexDigit), let value = UInt8(part, radix: 16) else { return nil }
            octets.append(value)
        }
        guard octets.contains(where: { $0 != 0 }), octets[0] & 1 == 0 else { return nil }
        return octets
    }

    /// The /24 broadcast address of each private IPv4 address, then the limited broadcast,
    /// at most `broadcastLimit` in all. Addresses outside 10/8, 172.16/12 and 192.168/16 are
    /// ignored. A network wider than /24 is reached by the limited broadcast instead.
    public static func broadcasts(for ipv4: [String]) -> [String] {
        var found: Set<String> = []
        for text in ipv4 {
            guard let octets = parse(text), isPrivate(octets) else { continue }
            found.insert("\(octets[0]).\(octets[1]).\(octets[2]).255")
        }
        return found.sorted { numeric($0) < numeric($1) }.prefix(broadcastLimit - 1) + [limitedBroadcast]
    }

    /// True for 255.255.255.255 and for a private /24 broadcast address.
    static func isAllowedBroadcast(_ text: String) -> Bool {
        if text == limitedBroadcast { return true }
        guard let octets = parse(text), isPrivate(octets) else { return false }
        return octets[3] == 255
    }

    private static func parse(_ text: String) -> [UInt8]? {
        let parts = text.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 4 else { return nil }
        var octets: [UInt8] = []
        for part in parts {
            guard (1...3).contains(part.utf8.count), part.utf8.allSatisfy(\.isASCIIDigit), let value = UInt8(part) else { return nil }
            octets.append(value)
        }
        return octets
    }

    private static func isPrivate(_ o: [UInt8]) -> Bool {
        o[0] == 10 || (o[0] == 172 && (16...31).contains(o[1])) || (o[0] == 192 && o[1] == 168)
    }

    private static func numeric(_ text: String) -> UInt32 {
        (parse(text) ?? [0, 0, 0, 0]).reduce(0) { $0 << 8 | UInt32($1) }
    }

    /// Sends one packet to each broadcast address and returns when every one is out or has
    /// timed out. Succeeds when at least one went out. Nothing is retried.
    public static func send(_ target: WakeTarget) async throws {
        let packet = try packet(mac: target.mac)
        let addresses = Array(Set(target.broadcast))
        guard !addresses.isEmpty, addresses.count <= broadcastLimit, addresses.allSatisfy(isAllowedBroadcast) else { throw WakeError.noBroadcast }
        let sent = await withTaskGroup(of: Bool.self) { group in
            for address in addresses {
                group.addTask { await UDPShot(address: address).send(packet, timeout: timeout) }
            }
            return await group.reduce(false) { $0 || $1 }
        }
        guard sent else { throw WakeError.failed }
    }
}

private extension UInt8 {
    var isHexDigit: Bool { (0x30...0x39).contains(self) || (0x41...0x46).contains(self) || (0x61...0x66).contains(self) }
    var isASCIIDigit: Bool { (0x30...0x39).contains(self) }
}

/// One datagram on its own connection. The first outcome wins; the rest are ignored.
private final class UDPShot: Sendable {
    private let connection: NWConnection
    private let continuation = Mutex<CheckedContinuation<Bool, Never>?>(nil)
    private let queue = DispatchQueue(label: "app.togo.wakeonlan")

    init(address: String) {
        let parameters = NWParameters.udp
        parameters.allowLocalEndpointReuse = true
        connection = NWConnection(host: NWEndpoint.Host(address), port: WakeOnLAN.port, using: parameters)
    }

    func send(_ packet: Data, timeout: Duration) async -> Bool {
        await withCheckedContinuation { continuation in
            self.continuation.withLock { $0 = continuation }
            connection.stateUpdateHandler = { [self] state in
                switch state {
                case .ready:
                    connection.send(content: packet, completion: .contentProcessed { [self] error in finish(error == nil) })
                case .failed, .waiting, .cancelled:
                    finish(false)
                default:
                    break
                }
            }
            connection.start(queue: queue)
            let seconds = Double(timeout.components.seconds) + Double(timeout.components.attoseconds) / 1e18
            queue.asyncAfter(deadline: .now() + seconds) { [self] in finish(false) }
        }
    }

    private func finish(_ sent: Bool) {
        guard let continuation = continuation.withLock({ $0.take() }) else { return }
        connection.stateUpdateHandler = nil
        connection.cancel()
        continuation.resume(returning: sent)
    }
}
