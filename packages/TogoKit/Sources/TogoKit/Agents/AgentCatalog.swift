import Foundation

/// How an agent is drawn wherever it shows as a glyph.
public enum AgentMark: Hashable, Sendable {
    /// A symbol asset in the app's catalog, made from the agent's official mark (see the
    /// Acknowledgments' "Agent marks" for each source and its terms).
    case logo(String)
    /// One or two letters in SF Mono, for an agent without an official mark the app may show.
    case monogram(String)
    /// An SF Symbol, for the shell and programs outside the registry.
    case symbol(String)
}

/// The agents the helper's registry knows, as the app names and draws them. The helper's list
/// (`helper/internal/agents`) is the source of truth; ids it adds later still show, under their
/// helper name and a monogram.
public enum AgentCatalog {
    public struct Entry: Hashable, Sendable {
        public var id: String
        public var name: String
        /// The program the helper runs; empty for the shell.
        public var command: String
        public var mark: AgentMark
    }

    /// In the helper's registry order.
    public static let entries: [Entry] = [
        Entry(id: "claude", name: "Claude Code", command: "claude", mark: .logo("agent.claude")),
        Entry(id: "omp", name: "OMP", command: "omp", mark: .logo("agent.omp")),
        Entry(id: "codex", name: "Codex", command: "codex", mark: .monogram("Cx")),
        Entry(id: "opencode", name: "OpenCode", command: "opencode", mark: .logo("agent.opencode")),
        Entry(id: "copilot", name: "Copilot CLI", command: "copilot", mark: .logo("agent.copilot")),
        Entry(id: "cursor", name: "Cursor CLI", command: "cursor-agent", mark: .logo("agent.cursor")),
        Entry(id: "aider", name: "Aider", command: "aider", mark: .monogram("Ad")),
        Entry(id: "amp", name: "Amp", command: "amp", mark: .logo("agent.amp")),
        Entry(id: "auggie", name: "Auggie", command: "auggie", mark: .monogram("Au")),
        Entry(id: "cline", name: "Cline", command: "cline", mark: .logo("agent.cline")),
        Entry(id: "crush", name: "Crush", command: "crush", mark: .monogram("Cr")),
        Entry(id: "droid", name: "Droid", command: "droid", mark: .monogram("Dr")),
        Entry(id: "goose", name: "Goose", command: "goose", mark: .logo("agent.goose")),
        Entry(id: "kilo", name: "Kilo Code", command: "kilo", mark: .logo("agent.kilo")),
        Entry(id: "kimi", name: "Kimi Code", command: "kimi", mark: .logo("agent.kimi")),
        Entry(id: "kiro", name: "Kiro CLI", command: "kiro-cli", mark: .monogram("Kr")),
        Entry(id: "openhands", name: "OpenHands", command: "openhands", mark: .logo("agent.openhands")),
        Entry(id: "pi", name: "Pi", command: "pi", mark: .logo("agent.pi")),
        Entry(id: "vibe", name: "Mistral Vibe", command: "vibe", mark: .logo("agent.mistral")),
        Entry(id: "shell", name: "Shell", command: "", mark: .symbol("terminal")),
    ]

    private static let byID = Dictionary(uniqueKeysWithValues: entries.map { ($0.id, $0) })

    public static func entry(_ id: String) -> Entry? { byID[id] }

    public static func name(_ id: String) -> String? { byID[id]?.name }

    /// The agent's mark: a monogram from its id when the app does not know it, the terminal
    /// for the shell and for programs outside the registry.
    public static func mark(_ id: String) -> AgentMark {
        if let entry = byID[id] { return entry.mark }
        if id.isEmpty || id == AgentPreset.custom { return .symbol("terminal") }
        return .monogram(String(id.prefix(2)).capitalized)
    }
}
