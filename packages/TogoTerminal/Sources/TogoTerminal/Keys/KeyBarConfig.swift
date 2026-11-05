import Foundation

public struct KeyBarItem: Hashable, Codable, Sendable, Identifiable {
    public var id: UUID
    /// A word, also the accessibility label when `symbol` is set.
    public var title: String
    /// SF Symbol name; the bar shows it in place of the title.
    public var symbol: String?
    public var action: KeyAction

    public init(id: UUID = UUID(), title: String, symbol: String? = nil, action: KeyAction) {
        self.id = id
        self.title = title
        self.symbol = symbol
        self.action = action
    }

    private enum CodingKeys: String, CodingKey { case id, title, symbol, action }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decodeIfPresent(UUID.self, forKey: .id) ?? UUID()
        title = try c.decode(String.self, forKey: .title)
        symbol = try c.decodeIfPresent(String.self, forKey: .symbol)
        action = try c.decode(KeyAction.self, forKey: .action)
    }
}

public struct KeyBarConfig: Hashable, Codable, Sendable {
    public var items: [KeyBarItem]

    public init(items: [KeyBarItem] = []) {
        self.items = items
    }

    private enum CodingKeys: String, CodingKey { case items }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        items = try c.decodeIfPresent([KeyBarItem].self, forKey: .items) ?? []
    }

    public static let standard = KeyBarConfig(items: [
        .keys("Esc", "Esc"),
        KeyBarItem(title: "Ctrl", action: .ctrlLatch),
        KeyBarItem(title: "Alt", action: .altLatch),
        .keys("Tab", "Tab"),
        .keys("Back Tab", "S-Tab"),
    ] + arrows + [compose, paste, hide])

    public static let claude = KeyBarConfig(items: [
        .keys("Esc", "Esc"),
        .keys("Rewind", "Esc Esc"),
        .keys("Mode", "S-Tab"),
        .keys("Interrupt", "C-c"),
        .keys("Ctrl O", "C-o"),
    ] + arrows + [compose, paste, hide])

    /// The bar for OMP sessions: the standard bar with Interrupt repeated at the end.
    public static let omp = KeyBarConfig(items: standard.items + [.keys("Interrupt", "C-c")])

    static let arrowItems: [(String, String, String)] = [
        ("Left", "arrow.left", "Left"), ("Down", "arrow.down", "Down"),
        ("Up", "arrow.up", "Up"), ("Right", "arrow.right", "Right"),
    ]

    private static var arrows: [KeyBarItem] {
        arrowItems.map { .keys($0.0, $0.2, symbol: $0.1) }
    }

    private static let compose = KeyBarItem(title: "Compose", symbol: "square.and.pencil", action: .compose)
    private static let paste = KeyBarItem(title: "Paste", symbol: "doc.on.clipboard", action: .paste)
    private static let hide = KeyBarItem(title: "Hide keyboard", symbol: "keyboard.chevron.compact.down", action: .hideKeyboard)
}

extension KeyBarItem {
    /// Presets are written in the sequence grammar so they read like what the user would type.
    static func keys(_ title: String, _ sequence: String, symbol: String? = nil) -> KeyBarItem {
        // A preset that fails to parse is a programming error caught by the preset tests.
        let steps = (try? KeySequence.parse(sequence)) ?? []
        return KeyBarItem(title: title, symbol: symbol, action: .steps(steps))
    }
}
