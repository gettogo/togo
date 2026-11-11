import SwiftUI

/// Which workspace bar items give way first when a bar runs out of room, so the terminal's
/// controller always has its place (2026-11-10-keyboard/contract.md, D14). The system does the
/// folding: each item carries a `visibilityPriority`, and the lowest go into its overflow menu
/// first, the last-placed first among equals. This is the one table of tiers, so the bar and its
/// tests agree.
enum DockPriority {
    enum Tier: Int, Comparable {
        case low, high, urgent, essential

        static func < (lhs: Tier, rhs: Tier) -> Bool { lhs.rawValue < rhs.rawValue }
    }

    /// The bar's items, in the order the bar places them.
    enum Item: CaseIterable {
        case runAgain, changes, layer, mission, preview, actions, settings, showKeyboard, keys
    }

    /// `missionNeedsYou`: mission control carries a badge, so it is news and stays.
    static func tier(_ item: Item, missionNeedsYou: Bool = false) -> Tier {
        switch item {
        case .keys: .essential
        case .changes, .actions, .showKeyboard, .runAgain: .high
        case .mission: missionNeedsYou ? .urgent : .low
        case .layer, .preview, .settings: .low
        }
    }

    /// The `count` items that fold into the overflow menu first, among `items` as placed: the
    /// lowest tier first, and among equals the one placed last.
    static func overflowing(_ items: [Item], count: Int, missionNeedsYou: Bool = false) -> [Item] {
        let ranked = items.enumerated().sorted {
            let (a, b) = (tier($0.element, missionNeedsYou: missionNeedsYou), tier($1.element, missionNeedsYou: missionNeedsYou))
            return a != b ? a < b : $0.offset > $1.offset
        }
        return ranked.prefix(max(count, 0)).map(\.element)
    }
}

@available(iOS 27.0, *)
extension DockPriority.Tier {
    var priority: ToolbarItemVisibilityPriority {
        switch self {
        case .low: .low
        case .high: .high
        case .urgent: ToolbarItemVisibilityPriority(higherThan: .high)
        case .essential: ToolbarItemVisibilityPriority(higherThan: DockPriority.Tier.urgent.priority)
        }
    }
}
