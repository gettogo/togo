import Foundation

// The key bar this described is gone (`TerminalKeysConfig` replaced it); the type stays so
// settings saved by older builds, and synced from them, still decode and migrate.

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
        // A bar synced from a newer build can hold a key this build does not know; that key
        // is dropped rather than losing the whole bar.
        items = (try c.decodeIfPresent([Droppable<KeyBarItem>].self, forKey: .items) ?? []).compactMap(\.value)
            .map(Self.migrated)
    }

    /// The Compose key opened a composer the terminal no longer has; it comes back as Attach,
    /// in the same place, so a customised bar keeps a key for pictures.
    static func migrated(_ item: KeyBarItem) -> KeyBarItem {
        guard item.action == .compose else { return item }
        return KeyBarItem(id: item.id, title: attach.title, symbol: attach.symbol, action: .attach)
    }

    public static let standard = KeyBarConfig(items: [
        .keys("Esc", "Esc"),
        .keys("^C", "C-c"),
        KeyBarItem(title: "Ctrl", action: .ctrlLatch),
        KeyBarItem(title: "Alt", action: .altLatch),
        KeyBarItem(title: "Shift", symbol: "shift", action: .shiftLatch),
        .keys("Tab", "Tab"),
        .keys("Back Tab", "S-Tab"),
    ] + arrows + [attach, dictate, paste, hide])

    public static let claude = KeyBarConfig(items: [
        .keys("Esc", "Esc"),
        .keys("Rewind", "Esc Esc"),
        .keys("Mode", "S-Tab"),
        .keys("Interrupt", "C-c"),
        .keys("Ctrl O", "C-o"),
    ] + arrows + [attach, dictate, paste, hide])

    /// The bar for OMP sessions: the standard bar with Interrupt repeated at the end.
    public static let omp = KeyBarConfig(items: standard.items + [.keys("Interrupt", "C-c")])

    static let arrowItems: [(String, String, String)] = [
        ("Left", "arrow.left", "Left"), ("Down", "arrow.down", "Down"),
        ("Up", "arrow.up", "Up"), ("Right", "arrow.right", "Right"),
    ]

    private static var arrows: [KeyBarItem] {
        arrowItems.map { .keys($0.0, $0.2, symbol: $0.1) }
    }

    static let attach = KeyBarItem(title: "Attach image", symbol: "photo", action: .attach)
    private static let dictate = KeyBarItem(title: "Dictate", symbol: "mic", action: .dictate)
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
