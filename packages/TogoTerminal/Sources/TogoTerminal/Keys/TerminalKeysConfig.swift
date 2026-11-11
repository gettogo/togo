import Foundation

/// A key the column and the palette can show, by name rather than by sequence, so the agent
/// table can fill Mode and Stop and the settings page can reorder the palette without
/// knowing what each key sends.
public enum KeyRef: String, Codable, Hashable, Sendable, CaseIterable {
    case esc, ctrl, alt, shift, mode, stop
    case tab, shiftTab, enter, hideKeyboard
    case home, end, pageUp, pageDown, slash, at
    case paste, attach, dictate
    case up, down, left, right

    /// The palette's order: four to a row.
    public static let defaultPalette: [KeyRef] = [
        .tab, .shiftTab, .enter, .hideKeyboard,
        .alt, .shift, .home, .end,
        .pageUp, .pageDown, .slash, .at,
        .paste, .attach, .dictate,
    ]

    /// The column's keys above the controller, in order.
    public static let column: [KeyRef] = [.esc, .ctrl, .mode, .stop]
    /// The column in compact height: Mode and Stop go to the palette's first row.
    public static let shortColumn: [KeyRef] = [.esc, .ctrl]
    public static let arrows: [KeyRef] = [.up, .down, .left, .right]

    /// The key sent, for keys that send one. Mode and Stop come from `AgentKeys`; latches and
    /// tools have no stroke.
    public var stroke: KeyStroke? {
        switch self {
        case .esc: KeyStroke(.escape)
        case .tab: KeyStroke(.tab)
        case .shiftTab: KeyStroke(.tab, .shift)
        case .enter: KeyStroke(.enter)
        case .home: KeyStroke(.home)
        case .end: KeyStroke(.end)
        case .pageUp: KeyStroke(.pageUp)
        case .pageDown: KeyStroke(.pageDown)
        case .slash: KeyStroke(.char("/"))
        case .at: KeyStroke(.char("@"))
        case .up: KeyStroke(.up)
        case .down: KeyStroke(.down)
        case .left: KeyStroke(.left)
        case .right: KeyStroke(.right)
        case .ctrl, .alt, .shift, .mode, .stop, .hideKeyboard, .paste, .attach, .dictate: nil
        }
    }

    public var latch: KeyModifiers? {
        switch self {
        case .ctrl: .ctrl
        case .alt: .alt
        case .shift: .shift
        default: nil
        }
    }

    /// The word on the face, or nil for a key drawn as a symbol.
    public var title: String {
        switch self {
        case .esc: "Esc"
        case .ctrl: "Ctrl"
        case .alt: "Alt"
        case .shift: "Shift"
        case .mode: "Mode"
        case .stop: "Stop"
        case .tab: "Tab"
        case .shiftTab: "Shift Tab"
        case .enter: "Enter"
        case .hideKeyboard: "Hide keyboard"
        case .home: "Home"
        case .end: "End"
        case .pageUp: "PgUp"
        case .pageDown: "PgDn"
        case .slash: "/"
        case .at: "@"
        case .paste: "Paste"
        case .attach: "Attach"
        case .dictate: "Dictate"
        case .up: "Up"
        case .down: "Down"
        case .left: "Left"
        case .right: "Right"
        }
    }

    public var symbol: String? {
        switch self {
        case .shift: "shift"
        case .shiftTab: "arrow.left.to.line"
        case .enter: "return"
        case .hideKeyboard: "keyboard.chevron.compact.down"
        case .paste: "doc.on.clipboard"
        case .attach: "photo"
        case .dictate: "mic"
        case .up: "arrow.up"
        case .down: "arrow.down"
        case .left: "arrow.left"
        case .right: "arrow.right"
        default: nil
        }
    }

    /// Held, it repeats like the system keyboard.
    public var repeats: Bool {
        switch self {
        case .tab, .shiftTab, .up, .down, .left, .right, .pageUp, .pageDown: true
        default: false
        }
    }
}

/// What the agent's Mode and Stop keys send. A Swift table for now; the helper's registry
/// will carry it once more agents are checked against their real bindings.
public struct AgentKeys: Equatable, Sendable {
    /// Cycles the agent's mode (plan, build, ask…). Nil for agents without one: the key is
    /// then left out.
    public var mode: KeyStroke?
    /// Interrupts the agent's turn.
    public var stop: KeyStroke

    public init(mode: KeyStroke?, stop: KeyStroke) {
        self.mode = mode
        self.stop = stop
    }

    /// A shell: Tab completes, ^C interrupts.
    public static let shell = AgentKeys(mode: KeyStroke(.tab), stop: KeyStroke(.char("c"), .ctrl))
    /// Agents not in the table: Shift-Tab cycles modes in most TUIs, ^C interrupts.
    public static let fallback = AgentKeys(mode: KeyStroke(.tab, .shift), stop: KeyStroke(.char("c"), .ctrl))

    public static let table: [String: AgentKeys] = [
        "shell": .shell,
        "opencode": AgentKeys(mode: KeyStroke(.tab), stop: KeyStroke(.char("c"), .ctrl)),
        "claude": AgentKeys(mode: KeyStroke(.tab, .shift), stop: KeyStroke(.escape)),
        "codex": AgentKeys(mode: nil, stop: KeyStroke(.char("c"), .ctrl)),
        "omp": AgentKeys(mode: KeyStroke(.tab, .shift), stop: KeyStroke(.char("c"), .ctrl)),
        "pi": AgentKeys(mode: KeyStroke(.tab, .shift), stop: KeyStroke(.escape)),
        "crush": AgentKeys(mode: nil, stop: KeyStroke(.escape)),
        "aider": AgentKeys(mode: nil, stop: KeyStroke(.char("c"), .ctrl)),
    ]

    /// The keys for `agent`, the helper's agent id. Nil and unknown ids take the fallback.
    public static func keys(for agent: String?) -> AgentKeys {
        guard let agent else { return .fallback }
        return table[agent.lowercased()] ?? .fallback
    }
}

/// What the user configured for the terminal's keys: the column, the controller, the keys a
/// waiting prompt adds, and the shortcuts above the keyboard. Layout is never stored: the
/// column stands or lies, the controller moves to a vertical bar, and the row rides a floating
/// keyboard from what the system reports.
public struct TerminalKeysConfig: Hashable, Codable, Sendable {
    public enum ShowColumn: String, Codable, Sendable {
        /// While a software keyboard types into the terminal.
        case whileTyping
        /// Also kept on screen by the session's "Keep keys on screen".
        case alsoOnDemand
    }

    public static let version = 2

    public var showColumn: ShowColumn = .whileTyping
    /// Flick the controller for arrows. Off: a tap still opens the palette, which then leads
    /// with the arrows.
    public var controller = true
    /// The keys a waiting prompt or menu wants, above the keyboard.
    public var promptKeys = true
    /// The user's shortcuts and snippets for every agent, above the keyboard and in the palette.
    public var shortcuts: [KeyBarItem] = []
    /// The agent's own, keyed by the helper's agent id, shown before the others.
    public var shortcutsByAgent: [String: [KeyBarItem]] = [:]
    /// The palette's keys, in order.
    public var palette: [KeyRef] = KeyRef.defaultPalette

    public init() {}

    private enum CodingKeys: String, CodingKey {
        case version, showColumn, controller, promptKeys, shortcuts, shortcutsByAgent, palette
    }

    /// A config written by any build decodes: a missing field takes its default, a key the
    /// build does not know is dropped rather than losing the config.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        showColumn = (try? c.decodeIfPresent(ShowColumn.self, forKey: .showColumn)) ?? .whileTyping
        controller = try c.decodeIfPresent(Bool.self, forKey: .controller) ?? true
        promptKeys = try c.decodeIfPresent(Bool.self, forKey: .promptKeys) ?? true
        shortcuts = (try c.decodeIfPresent([Droppable<KeyBarItem>].self, forKey: .shortcuts) ?? []).compactMap(\.value)
        let byAgent = try c.decodeIfPresent([String: [Droppable<KeyBarItem>]].self, forKey: .shortcutsByAgent) ?? [:]
        shortcutsByAgent = byAgent.mapValues { $0.compactMap(\.value) }.filter { !$0.value.isEmpty }
        let palette = (try c.decodeIfPresent([Droppable<KeyRef>].self, forKey: .palette) ?? []).compactMap(\.value)
        self.palette = palette.isEmpty ? KeyRef.defaultPalette : palette
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(Self.version, forKey: .version)
        try c.encode(showColumn, forKey: .showColumn)
        try c.encode(controller, forKey: .controller)
        try c.encode(promptKeys, forKey: .promptKeys)
        try c.encode(shortcuts, forKey: .shortcuts)
        try c.encode(shortcutsByAgent, forKey: .shortcutsByAgent)
        try c.encode(palette, forKey: .palette)
    }

    /// The shortcuts for a session: the agent's own first, then the ones for every agent.
    public func shortcuts(for agent: String?) -> [KeyBarItem] {
        let own = agent.flatMap { shortcutsByAgent[$0.lowercased()] } ?? []
        return own + shortcuts
    }

    /// Reads the bytes `AppSettings.keyBar` holds, written by this build or an older one. Bytes
    /// of the old key bar are migrated; bytes that decode as neither read as the default.
    public static func decode(_ data: Data) -> TerminalKeysConfig {
        let decoder = JSONDecoder()
        if let versioned = try? decoder.decode(Versioned.self, from: data), versioned.version != nil,
           let config = try? decoder.decode(TerminalKeysConfig.self, from: data) {
            return config
        }
        if let legacy = try? decoder.decode(KeyBarConfig.self, from: data) {
            return migrated(from: legacy)
        }
        return TerminalKeysConfig()
    }

    /// The old bar's keys as this config: an unedited preset becomes the defaults; a bar the
    /// user changed keeps every key the column and palette do not already have, in order, as
    /// shortcuts for every agent. Nobody loses a shortcut.
    public static func migrated(from legacy: KeyBarConfig) -> TerminalKeysConfig {
        var config = TerminalKeysConfig()
        guard ![KeyBarConfig.standard, .claude, .omp].contains(legacy) else { return config }
        let builtIn = Set((KeyBarConfig.standard.items + KeyBarConfig.claude.items + KeyBarConfig.omp.items).map(\.action))
        config.shortcuts = legacy.items.filter { item in
            guard case .steps = item.action else { return false }
            return !builtIn.contains(item.action)
        }
        return config
    }

    private struct Versioned: Decodable {
        var version: Int?
    }
}

/// An element that decodes to nil instead of failing its array.
struct Droppable<Value: Decodable>: Decodable {
    var value: Value?

    init(from decoder: any Decoder) throws {
        value = try? Value(from: decoder)
    }
}
