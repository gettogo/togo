import Foundation

/// How to sign in to each agent from the phone: the login command Togo starts in a muted
/// session on the machine, and what to type once it is up for agents whose login lives in
/// their own interface. The helper reads the steps off the screen (a page, a device code, a
/// key prompt, a provider menu) and the app answers them natively.
public struct SignInCommand: Hashable, Sendable, Identifiable {
    public var id: String { agentID + (variant.map { "." + $0 } ?? "") }
    /// The agent's registry id, such as `claude`.
    public var agentID: String
    /// A second way in for the same agent, such as Crush's Copilot sign-in. Nil for the main one.
    public var variant: String?
    /// What the row says.
    public var title: String
    /// The program on the machine's login PATH, run as a custom session.
    public var program: String
    public var args: [String]
    /// Text sent once the program waits at its input, such as `/login`. Nil sends nothing.
    public var typeAfterStart: String?
    /// One plain sentence on what happens next.
    public var note: String

    public init(
        agentID: String, variant: String? = nil, title: String, program: String, args: [String],
        typeAfterStart: String? = nil, note: String
    ) {
        self.agentID = agentID
        self.variant = variant
        self.title = title
        self.program = program
        self.args = args
        self.typeAfterStart = typeAfterStart
        self.note = note
    }
}

public enum SignInCatalog {
    /// The page opens in the system's sign-in sheet, which shares Safari's sign-ins; its
    /// redirect to the machine's localhost goes through a forward on the same port.
    private static let page = String(localized: "Opens the sign-in page here; the agent on your machine gets the result.", bundle: .module)
    private static let code = String(localized: "Shows a code to copy, then opens the page to enter it.", bundle: .module)
    private static let menu = String(localized: "Pick a provider, then sign in on the page or paste a key.", bundle: .module)

    /// Every agent Togo knows how to sign in, in the registry's order.
    public static let all: [SignInCommand] = [
        .init(agentID: "claude", title: "Claude Code", program: "claude", args: ["auth", "login"],
              note: String(localized: "Opens the sign-in page here; your Mac gets the result. If the page shows a code instead, paste it.", bundle: .module)),
        .init(agentID: "omp", title: "OMP", program: "omp", args: [], typeAfterStart: "/login", note: menu),
        .init(agentID: "codex", title: "Codex", program: "codex", args: ["login"], note: page),
        .init(agentID: "opencode", title: "OpenCode", program: "opencode", args: ["auth", "login"], note: menu),
        .init(agentID: "copilot", title: "Copilot CLI", program: "copilot", args: ["login"], note: page),
        .init(agentID: "cursor", title: "Cursor CLI", program: "cursor-agent", args: ["login"], note: page),
        .init(agentID: "aider", title: "Aider (OpenRouter)", program: "aider",
              args: ["--no-git", "--model", "openrouter/auto"],
              note: String(localized: "Signs in to OpenRouter. Other providers take an API key in your shell profile.", bundle: .module)),
        .init(agentID: "amp", title: "Amp", program: "amp", args: ["login"], note: code),
        .init(agentID: "auggie", title: "Auggie", program: "auggie", args: ["login", "--headless"], note: page),
        .init(agentID: "crush", title: "Crush (Charm Hyper)", program: "crush", args: ["login"], note: code),
        .init(agentID: "crush", variant: "copilot", title: "Crush (GitHub Copilot)", program: "crush",
              args: ["login", "copilot"], note: code),
        .init(agentID: "crush", variant: "openai", title: "Crush (ChatGPT)", program: "crush",
              args: ["login", "openai"], note: page),
        .init(agentID: "droid", title: "Droid", program: "droid", args: [],
              note: String(localized: "Choose Login, then copy the code and open the page.", bundle: .module)),
        .init(agentID: "goose", title: "Goose", program: "goose", args: ["configure"],
              note: String(localized: "Answer its setup questions; API keys go in a masked field.", bundle: .module)),
        .init(agentID: "kilo", title: "Kilo Code", program: "kilo", args: ["auth", "login"], note: menu),
        .init(agentID: "kimi", title: "Kimi Code", program: "kimi", args: ["login"], note: code),
        .init(agentID: "pi", title: "Pi", program: "pi", args: [], typeAfterStart: "/login", note: menu),
        .init(agentID: "vibe", title: "Mistral Vibe", program: "vibe", args: ["--setup"],
              note: String(localized: "Its setup runs in the terminal; paste your API key there or sign in on your Mac.", bundle: .module)),
    ]

    /// The sign-ins for the agents a machine has installed.
    public static func commands(installed: Set<String>) -> [SignInCommand] {
        all.filter { installed.contains($0.agentID) }
    }

    /// The session's create parameters: a custom, muted session in the home folder, named
    /// for the sign-in so it reads as one in the session list.
    public static func params(for command: SignInCommand) -> CreateSessionParams {
        CreateSessionParams(
            agent: "custom", name: "Sign in · " + command.title, program: command.program,
            args: command.args.isEmpty ? nil : command.args, muted: true
        )
    }
}
