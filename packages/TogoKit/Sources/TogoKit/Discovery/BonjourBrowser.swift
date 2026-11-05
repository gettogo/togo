import Foundation
@preconcurrency import Network

/// A machine advertising SSH nearby, resolved to an address the app can connect to.
public struct DiscoveredHost: Sendable, Hashable, Identifiable {
    public var id: String
    public var name: String
    public var host: String
    public var port: Int
    public var tailnet: Bool

    public init(id: String, name: String, host: String, port: Int, tailnet: Bool) {
        self.id = id
        self.name = name
        self.host = host
        self.port = port
        self.tailnet = tailnet
    }
}

/// Browses `_ssh._tcp` on the local network and resolves each service to an IPv4 address.
///
/// Resolving opens and immediately cancels a TCP connection to the service, because
/// Network only reports the address of a Bonjour endpoint once a connection reaches it.
/// IPv4 is asked for so the address has no interface scope and stays valid as typed.
public actor BonjourBrowser {
    public enum Status: Sendable, Equatable {
        case browsing
        /// Local Network access is off for the app.
        case denied
        case failed(String)
    }

    /// Single consumer: the discovery list.
    public nonisolated let results: AsyncStream<[DiscoveredHost]>
    /// Single consumer: the discovery list.
    public nonisolated let status: AsyncStream<Status>

    private let resultsContinuation: AsyncStream<[DiscoveredHost]>.Continuation
    private let statusContinuation: AsyncStream<Status>.Continuation
    private let queue = DispatchQueue(label: "app.togo.bonjour")
    private var browser: NWBrowser?
    private var resolving: [String: NWConnection] = [:]
    private var resolved: [String: DiscoveredHost] = [:]
    private var visible: [String: String] = [:]

    /// `kDNSServiceErr_PolicyDenied`: the user turned Local Network off for the app.
    private static let policyDenied: Int32 = -65570
    private static let resolveTimeout: Duration = .seconds(5)

    public init() {
        (results, resultsContinuation) = AsyncStream.makeStream(bufferingPolicy: .bufferingNewest(1))
        (status, statusContinuation) = AsyncStream.makeStream(bufferingPolicy: .bufferingNewest(1))
    }

    public func start() {
        guard browser == nil else { return }
        let browser = NWBrowser(for: .bonjour(type: "_ssh._tcp", domain: "local."), using: .tcp)
        browser.stateUpdateHandler = { [weak self] state in
            Task { await self?.browserState(state) }
        }
        browser.browseResultsChangedHandler = { [weak self] results, _ in
            let services = results.compactMap(Self.service)
            Task { await self?.update(services) }
        }
        self.browser = browser
        browser.start(queue: queue)
    }

    public func stop() {
        browser?.cancel()
        browser = nil
        for connection in resolving.values { connection.cancel() }
        resolving = [:]
    }

    private struct Service: Sendable {
        var id: String
        var name: String
        var endpoint: NWEndpoint
    }

    private static func service(_ result: NWBrowser.Result) -> Service? {
        guard case .service(let name, let type, let domain, _) = result.endpoint else { return nil }
        // One machine on Wi-Fi and Ethernet appears twice; the name is what the user sees.
        return Service(id: "\(name).\(type).\(domain)", name: name, endpoint: result.endpoint)
    }

    private func browserState(_ state: NWBrowser.State) {
        switch state {
        case .ready:
            statusContinuation.yield(.browsing)
        case .waiting(let error), .failed(let error):
            if case .dns(let code) = error, code == Self.policyDenied {
                statusContinuation.yield(.denied)
            } else {
                statusContinuation.yield(.failed(error.localizedDescription))
            }
        default:
            break
        }
    }

    private func update(_ services: [Service]) {
        visible = Dictionary(services.map { ($0.id, $0.name) }, uniquingKeysWith: { first, _ in first })
        // A machine that leaves and comes back may have a new address, so it resolves again.
        resolved = resolved.filter { visible[$0.key] != nil }
        for service in services where resolved[service.id] == nil && resolving[service.id] == nil {
            resolve(service)
        }
        publish()
    }

    private func resolve(_ service: Service) {
        let parameters = NWParameters.tcp
        if let ip = parameters.defaultProtocolStack.internetProtocol as? NWProtocolIP.Options {
            ip.version = .v4
        }
        let connection = NWConnection(to: service.endpoint, using: parameters)
        resolving[service.id] = connection
        connection.stateUpdateHandler = { [weak self, weak connection] state in
            guard case .ready = state, let remote = connection?.currentPath?.remoteEndpoint else { return }
            connection?.cancel()
            Task { await self?.finishResolving(service, remote: remote) }
        }
        connection.start(queue: queue)
        Task { [weak self] in
            try? await Task.sleep(for: Self.resolveTimeout)
            await self?.abandon(service.id, connection)
        }
    }

    private func finishResolving(_ service: Service, remote: NWEndpoint) {
        resolving[service.id] = nil
        guard visible[service.id] != nil else { return }
        guard case .hostPort(let host, let port) = remote else { return }
        let address = switch host {
        case .ipv4(let ip): "\(ip)"
        case .ipv6(let ip): "\(ip)"
        case .name(let name, _): name
        @unknown default: "\(host)"
        }
        resolved[service.id] = DiscoveredHost(
            id: service.id, name: service.name, host: address, port: Int(port.rawValue),
            tailnet: TailnetHint.isTailnet(address)
        )
        publish()
    }

    /// Only the attempt that started this timer: a later retry for the same service keeps its time.
    private func abandon(_ id: String, _ connection: NWConnection) {
        guard resolving[id] === connection else { return }
        resolving[id] = nil
        connection.cancel()
    }

    private func publish() {
        let hosts = resolved.values
            .filter { visible[$0.id] != nil }
            .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
        resultsContinuation.yield(hosts)
    }
}
