import Foundation

/// The running program's command intent. Existing screen/action routes remain
/// authoritative; this describes commands that need the original runtime.
/// Strings stay open so a newer helper cannot break the command list.
public struct NativeCommandMetadata: Codable, Hashable, Sendable {
    public struct Subcommand: Codable, Hashable, Sendable {
        public var name: String
        public var detail: String?
        public var hint: String?

        public init(name: String, detail: String? = nil, hint: String? = nil) {
            self.name = name
            self.detail = detail
            self.hint = hint
        }

        private enum CodingKeys: CodingKey { case name, detail, hint }

        public init(from decoder: any Decoder) throws {
            let c = try? decoder.container(keyedBy: CodingKeys.self)
            name = (try? c?.decode(String.self, forKey: .name)) ?? ""
            detail = try? c?.decode(String.self, forKey: .detail)
            hint = try? c?.decode(String.self, forKey: .hint)
        }
    }

    public var opens: String
    public var opensWithArgs: String?
    public var args: String?
    public var state: String?
    public var subcommands: [Subcommand]?
    public var aliases: [String]?
    public var group: String?
    public var destructive: Bool?
    public var icon: String?
    public var source: String?
    public var screenId: String?

    public init(opens: String, opensWithArgs: String? = nil, args: String? = nil, state: String? = nil,
                subcommands: [Subcommand]? = nil, aliases: [String]? = nil, group: String? = nil,
                destructive: Bool? = nil, icon: String? = nil, source: String? = nil, screenId: String? = nil) {
        self.opens = opens
        self.opensWithArgs = opensWithArgs
        self.args = args
        self.state = state
        self.subcommands = subcommands
        self.aliases = aliases
        self.group = group
        self.destructive = destructive
        self.icon = icon
        self.source = source
        self.screenId = screenId
    }

    private enum CodingKeys: CodingKey {
        case opens, opensWithArgs, args, state, subcommands, aliases, group, destructive, icon, source, screenId
    }

    /// Bad optional metadata must not discard an otherwise usable command.
    public init(from decoder: any Decoder) throws {
        let c = try? decoder.container(keyedBy: CodingKeys.self)
        opens = (try? c?.decode(String.self, forKey: .opens)) ?? "unknown"
        opensWithArgs = try? c?.decode(String.self, forKey: .opensWithArgs)
        args = try? c?.decode(String.self, forKey: .args)
        state = try? c?.decode(String.self, forKey: .state)
        subcommands = (try? c?.decode([Subcommand].self, forKey: .subcommands))?.filter { !$0.name.isEmpty }
        aliases = try? c?.decode([String].self, forKey: .aliases)
        group = try? c?.decode(String.self, forKey: .group)
        destructive = try? c?.decode(Bool.self, forKey: .destructive)
        icon = try? c?.decode(String.self, forKey: .icon)
        source = try? c?.decode(String.self, forKey: .source)
        screenId = try? c?.decode(String.self, forKey: .screenId)
    }

    public func intent(hasArguments: Bool) -> String {
        hasArguments ? opensWithArgs ?? opens : opens
    }
}
