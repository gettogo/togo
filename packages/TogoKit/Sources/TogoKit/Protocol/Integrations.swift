import Foundation

// integrations.v1: each harness's setup state on a machine, and the helper's own installer
// for the harnesses that take hooks. The phone names harnesses by registry id only.

extension Capability {
    /// `integrations.status`, `integrations.install` and `integrations.uninstall`.
    public static let integrations = "integrations.v1"
}

/// What a harness needs from setup.
public enum IntegrationSetup: WireEnum {
    /// Togo writes hook entries into the harness's config.
    case hooks
    /// The harness talks to Togo another way; nothing to set up.
    case none
    /// The harness has hooks Togo has not confirmed yet; nothing is written.
    case pending
    case unknown(String)

    public static let known: [IntegrationSetup] = [.hooks, .none, .pending]

    public var rawValue: String {
        switch self {
        case .hooks: "hooks"
        case .none: "none"
        case .pending: "pending"
        case .unknown(let value): value
        }
    }
}

/// The one word setup shows per harness.
public enum IntegrationState: WireEnum {
    case ready, needsSetup, outdated, notNeeded, notSupported, notInstalled
    case unknown(String)

    public static let known: [IntegrationState] = [
        .ready, .needsSetup, .outdated, .notNeeded, .notSupported, .notInstalled,
    ]

    public var rawValue: String {
        switch self {
        case .ready: "ready"
        case .needsSetup: "needsSetup"
        case .outdated: "outdated"
        case .notNeeded: "notNeeded"
        case .notSupported: "notSupported"
        case .notInstalled: "notInstalled"
        case .unknown(let value): value
        }
    }
}

/// One harness's setup state, as `togo doctor` prints it.
public struct IntegrationReport: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var name: String
    /// The harness is on the machine.
    public var installed: Bool
    public var setup: IntegrationSetup
    public var state: IntegrationState
    /// installed, outdated or absent, for a harness that takes hooks.
    public var hooks: String?
    /// chained, togo, own or absent, for a harness whose status line Togo chains.
    public var statusLine: String?
    /// The file Togo edits on the machine, and its copy from before Togo's first change.
    public var path: String?
    public var backup: String?
    /// When the helper last received the harness's hook and its status line (RFC 3339).
    public var lastHookAt: String?
    public var statusLineAt: String?
    /// The command that sets the harness up on the machine.
    public var fix: String?

    public init(
        id: String, name: String, installed: Bool, setup: IntegrationSetup, state: IntegrationState,
        hooks: String? = nil, statusLine: String? = nil, path: String? = nil, backup: String? = nil,
        lastHookAt: String? = nil, statusLineAt: String? = nil, fix: String? = nil
    ) {
        self.id = id
        self.name = name
        self.installed = installed
        self.setup = setup
        self.state = state
        self.hooks = hooks
        self.statusLine = statusLine
        self.path = path
        self.backup = backup
        self.lastHookAt = lastHookAt
        self.statusLineAt = statusLineAt
        self.fix = fix
    }
}

/// `integrations.status` result.
public struct IntegrationsStatus: Codable, Hashable, Sendable {
    public var integrations: [IntegrationReport]

    public init(integrations: [IntegrationReport]) {
        self.integrations = integrations
    }
}

/// One harness's install or uninstall outcome: installed, unchanged, removed, skipped or
/// refused (with `error`).
public struct IntegrationOutcome: Codable, Hashable, Sendable {
    public var id: String
    public var state: String
    public var path: String?
    public var backup: String?
    public var error: String?

    public init(id: String, state: String, path: String? = nil, backup: String? = nil, error: String? = nil) {
        self.id = id
        self.state = state
        self.path = path
        self.backup = backup
        self.error = error
    }
}

/// `integrations.install` and `integrations.uninstall` result: the outcomes, then every row.
public struct IntegrationsChange: Codable, Hashable, Sendable {
    public var results: [IntegrationOutcome]
    public var integrations: [IntegrationReport]

    public init(results: [IntegrationOutcome], integrations: [IntegrationReport]) {
        self.results = results
        self.integrations = integrations
    }
}

/// `integrations.install` / `integrations.uninstall` params. No agents on install sets up
/// every harness installed on the machine.
struct IntegrationsChangeParams: Codable, Hashable, Sendable {
    var agents: [String]?
    var block: Bool?
}

extension HelperClient {
    public func integrationsStatus() async throws -> IntegrationsStatus {
        try await readyCall(.integrationsStatus, EmptyParams(), needs: Capability.integrations)
    }

    /// Runs the helper's installer for `agents`, or for every installed harness when nil.
    public func installIntegrations(_ agents: [String]?, block: Bool = false) async throws -> IntegrationsChange {
        try await readyCall(
            .integrationsInstall, IntegrationsChangeParams(agents: agents, block: block ? true : nil),
            needs: Capability.integrations
        )
    }

    public func uninstallIntegrations(_ agents: [String]) async throws -> IntegrationsChange {
        try await readyCall(
            .integrationsUninstall, IntegrationsChangeParams(agents: agents, block: nil), needs: Capability.integrations
        )
    }
}
