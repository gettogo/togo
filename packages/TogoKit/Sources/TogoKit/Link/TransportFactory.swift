import Foundation

public enum TransportKind: String, Sendable, CaseIterable {
    case mosh, et, ssh

    public var displayName: String {
        switch self {
        case .mosh: "Mosh"
        case .et: "ET"
        case .ssh: "SSH"
        }
    }
}

public struct AttachRequest: Sendable {
    public var profile: HostProfile
    public var connection: SSHConnection
    public var argv: [String]
    public var cols: Int
    public var rows: Int

    public init(profile: HostProfile, connection: SSHConnection, argv: [String], cols: Int, rows: Int) {
        self.profile = profile
        self.connection = connection
        self.argv = argv
        self.cols = cols
        self.rows = rows
    }
}

public struct TransportFactory: Sendable {
    public var kind: TransportKind
    /// nil means the kind may be tried for this profile.
    public var unavailableReason: @Sendable (HostProfile) -> String?
    public var make: @Sendable (AttachRequest) async throws -> any ByteTransport

    public init(
        kind: TransportKind,
        unavailableReason: @escaping @Sendable (HostProfile) -> String?,
        make: @escaping @Sendable (AttachRequest) async throws -> any ByteTransport
    ) {
        self.kind = kind
        self.unavailableReason = unavailableReason
        self.make = make
    }

    public static let ssh = TransportFactory(kind: .ssh, unavailableReason: { _ in nil }) { request in
        SSHTransport(connection: request.connection, argv: request.argv, cols: request.cols, rows: request.rows)
    }

    /// Mosh's UDP flow goes straight to the server's address, which a jump host hides.
    public static func moshUnavailableReason(_ profile: HostProfile) -> String? {
        profile.jumps.isEmpty ? nil : "Mosh needs a direct route; this machine uses a jump host"
    }

    public static func etUnavailableReason(_ profile: HostProfile) -> String? {
        profile.etAvailable ? nil : "etserver not found on this machine"
    }
}

public struct FallbackNote: Sendable, Equatable {
    public var kind: TransportKind
    public var reason: String

    public init(kind: TransportKind, reason: String) {
        self.kind = kind
        self.reason = reason
    }
}

public struct Attachment: Sendable {
    public var transport: any ByteTransport
    public var kind: TransportKind
    public var notes: [FallbackNote]
    public var connection: SSHConnection

    public init(transport: any ByteTransport, kind: TransportKind, notes: [FallbackNote], connection: SSHConnection) {
        self.transport = transport
        self.kind = kind
        self.notes = notes
        self.connection = connection
    }
}
