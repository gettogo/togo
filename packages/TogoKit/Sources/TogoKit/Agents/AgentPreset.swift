import Foundation

/// A saved way to start an agent: a registry agent or any program, with its flags. Presets
/// sync through iCloud with the user's other devices; they never hold keys or secrets.
public struct AgentPreset: Codable, Hashable, Sendable, Identifiable {
    /// The agent id for a program outside the helper's registry.
    public static let custom = "custom"
    /// Where presets live in a `SyncedRecordStore`, one record per preset id.
    public static let storePrefix = "preset."
    /// The program the helper's registry runs for each agent id, as in its agents registry.
    public static let registryCommands = Dictionary(
        uniqueKeysWithValues: AgentCatalog.entries.filter { !$0.command.isEmpty }.map { ($0.id, $0.command) }
    )

    public var id: UUID
    public var name: String
    /// A registry id such as `claude`, or `custom` with `program` set.
    public var agent: String
    /// With `custom`: a bare name looked up on the machine's login PATH, or an absolute path.
    public var program: String?
    /// Each entry reaches the program as one argument, exactly as stored.
    public var args: [String]

    public init(id: UUID = UUID(), name: String, agent: String, program: String? = nil, args: [String] = []) {
        self.id = id
        self.name = name
        self.agent = agent
        self.program = program
        self.args = args
    }

    public var isCustom: Bool { agent == Self.custom }

    /// The program to look for on a machine: the custom program, or the command the helper's
    /// registry runs for the agent.
    public var command: String { isCustom ? (program ?? "") : Self.registryCommands[agent] ?? agent }

    /// The `session.create` parameters that start this preset in `cwd`.
    public func launch(cwd: String?, name: String?) -> CreateSessionParams {
        CreateSessionParams(
            agent: agent, cwd: cwd, name: name,
            program: isCustom ? program : nil,
            args: args.isEmpty ? nil : args
        )
    }

    private enum CodingKeys: String, CodingKey { case id, name, agent, program, args }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(UUID.self, forKey: .id)
        name = try container.decode(String.self, forKey: .name)
        agent = try container.decode(String.self, forKey: .agent)
        program = try container.decodeIfPresent(String.self, forKey: .program)
        args = try container.decodeIfPresent([String].self, forKey: .args) ?? []
    }
}
