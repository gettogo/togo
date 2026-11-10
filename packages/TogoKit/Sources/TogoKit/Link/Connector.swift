import Foundation

public enum ConnectorError: Error, Equatable, LocalizedError {
    /// The chosen transport cannot run for this host, or no transport could.
    case unavailable(String)

    public var errorDescription: String? {
        switch self {
        case .unavailable(let reason): reason
        }
    }
}

/// Picks the transport for an attach. Auto tries Mosh, then SSH and records
/// in words why each skipped kind was not used. An explicit choice never falls back, except
/// when the address SSH reached is one the chosen kind cannot use: that is about the
/// network, not the choice, so SSH carries the session with a note saying why.
public actor Connector {
    /// ET is opt-in per machine: its client crypto has not been audited yet, so Auto never picks it.
    private static let autoOrder: [TransportKind] = [.mosh, .ssh]
    private let factories: [TransportFactory]

    public init(factories: [TransportFactory]) {
        self.factories = factories
    }

    /// Returns a transport that has already connected.
    public func attach(_ request: AttachRequest) async throws -> Attachment {
        guard let explicit = TransportKind(preference: request.profile.transport) else {
            return try await attachAuto(request)
        }
        guard let factory = factories.first(where: { $0.kind == explicit }) else {
            throw ConnectorError.unavailable(String(localized: "\(explicit.displayName) is not available in this build", bundle: .module))
        }
        if let reason = factory.unavailableReason(request.profile) {
            throw ConnectorError.unavailable(reason)
        }
        do {
            let transport = try await connected(factory, request)
            return Attachment(transport: transport, kind: explicit, notes: [], connection: request.connection)
        } catch let error as TransportAddressError {
            guard explicit != .ssh, let ssh = factories.first(where: { $0.kind == .ssh }) else { throw error }
            let note = FallbackNote(kind: explicit, reason: Self.reason(for: error, kind: explicit, host: request.profile.endpoint.host))
            let transport = try await connected(ssh, request)
            return Attachment(transport: transport, kind: .ssh, notes: [note], connection: request.connection)
        }
    }

    private func attachAuto(_ request: AttachRequest) async throws -> Attachment {
        var notes: [FallbackNote] = []
        var lastError: (any Error)?
        for kind in Self.autoOrder {
            guard let factory = factories.first(where: { $0.kind == kind }) else { continue }
            if let reason = factory.unavailableReason(request.profile) {
                notes.append(FallbackNote(kind: kind, reason: reason))
                continue
            }
            do {
                let transport = try await connected(factory, request)
                return Attachment(transport: transport, kind: kind, notes: notes, connection: request.connection)
            } catch {
                lastError = error
                notes.append(FallbackNote(kind: kind, reason: Self.reason(for: error, kind: kind, host: request.profile.endpoint.host)))
            }
        }
        // The last kind's own error tells the link whether a retry can help; a dropped
        // channel during a network change must not read as "no transport".
        if let lastError { throw lastError }
        throw ConnectorError.unavailable(notes.last?.reason ?? "No transport is available")
    }

    private func connected(_ factory: TransportFactory, _ request: AttachRequest) async throws -> any ByteTransport {
        let transport = try await factory.make(request)
        do {
            try await transport.connect()
        } catch {
            await transport.close()
            throw error
        }
        return transport
    }

    static func reason(for error: any Error, kind: TransportKind, host: String) -> String {
        switch error {
        case MoshBootstrapError.serverMissing:
            String(localized: "Mosh isn't installed on \(host)", bundle: .module)
        case MoshConnectError.noUDPReply:
            String(localized: "Mosh got no reply from \(host); the network may block UDP", bundle: .module)
        case TransportAddressError.linkLocalOnly:
            String(localized: "\(kind.displayName) can't use \(host)'s link-local IPv6 address", bundle: .module)
        default:
            error.localizedDescription
        }
    }
}

extension TransportKind {
    init?(preference: TransportPreference) {
        switch preference {
        case .auto: return nil
        case .ssh: self = .ssh
        case .mosh: self = .mosh
        case .et: self = .et
        }
    }
}
