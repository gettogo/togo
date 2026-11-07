import Foundation

public struct HostEndpoint: Codable, Hashable, Sendable {
    public var host: String
    public var port: Int = 22
    public var user: String

    public init(host: String, port: Int = 22, user: String) {
        self.host = host
        self.port = port
        self.user = user
    }

    private enum CodingKeys: String, CodingKey {
        case host, port, user
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        host = try container.decode(String.self, forKey: .host)
        port = try container.decodeIfPresent(Int.self, forKey: .port) ?? 22
        user = try container.decode(String.self, forKey: .user)
    }
}

/// Points at a credential in the secret store; the secret itself never lives in a profile.
public enum AuthMethodRef: Codable, Hashable, Sendable {
    case key(id: String)
    case password
}

public enum TransportPreference: String, Codable, Sendable, CaseIterable {
    case auto, ssh, mosh, et
}

public struct PortRange: Codable, Hashable, Sendable {
    public var lower: Int
    public var upper: Int

    public init(lower: Int, upper: Int) {
        self.lower = lower
        self.upper = upper
    }
}

public struct HostProfile: Codable, Hashable, Sendable, Identifiable {
    public var id: UUID
    public var name: String
    public var endpoint: HostEndpoint
    public var jumps: [HostEndpoint] = []
    public var auth: AuthMethodRef
    public var transport: TransportPreference = .auto
    public var moshServerPath: String? = nil
    public var moshPorts: PortRange? = nil
    public var etAvailable = false
    /// Every address the machine is known to answer on. Always holds the saved address as a
    /// manual route; the helper's report fills in the rest after each connect.
    public var routes: [HostRoute]
    /// The route the last successful connection used, tried first next time.
    public var lastRoute: String?

    public init(
        id: UUID,
        name: String,
        endpoint: HostEndpoint,
        jumps: [HostEndpoint] = [],
        auth: AuthMethodRef,
        transport: TransportPreference = .auto,
        moshServerPath: String? = nil,
        moshPorts: PortRange? = nil,
        etAvailable: Bool = false,
        routes: [HostRoute]? = nil,
        lastRoute: String? = nil
    ) {
        self.id = id
        self.name = name
        self.endpoint = endpoint
        self.jumps = jumps
        self.auth = auth
        self.transport = transport
        self.moshServerPath = moshServerPath
        self.moshPorts = moshPorts
        self.etAvailable = etAvailable
        self.routes = routes ?? [.manual(endpoint.host)]
        self.lastRoute = lastRoute
    }

    private enum CodingKeys: String, CodingKey {
        case id, name, endpoint, jumps, auth, transport, moshServerPath, moshPorts, etAvailable, routes, lastRoute
    }

    // Profiles saved by an older build lack newer keys; those take the defaults.
    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(UUID.self, forKey: .id)
        name = try container.decode(String.self, forKey: .name)
        endpoint = try container.decode(HostEndpoint.self, forKey: .endpoint)
        jumps = try container.decodeIfPresent([HostEndpoint].self, forKey: .jumps) ?? []
        auth = try container.decode(AuthMethodRef.self, forKey: .auth)
        transport = try container.decodeIfPresent(TransportPreference.self, forKey: .transport) ?? .auto
        moshServerPath = try container.decodeIfPresent(String.self, forKey: .moshServerPath)
        moshPorts = try container.decodeIfPresent(PortRange.self, forKey: .moshPorts)
        etAvailable = try container.decodeIfPresent(Bool.self, forKey: .etAvailable) ?? false
        // A profile from before routes held one address: it becomes the first manual route.
        routes = try container.decodeIfPresent([HostRoute].self, forKey: .routes) ?? [.manual(endpoint.host)]
        lastRoute = try container.decodeIfPresent(String.self, forKey: .lastRoute)
    }
}
