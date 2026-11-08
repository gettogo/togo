import Foundation
import TogoKit

/// The order agents are offered in: the user's favorites, then every other agent, each part
/// by name. Favorites belong to this device; they never sync. Lists take the favorites once,
/// when they open, so starring an agent leaves the rows where they are until the next open.
enum AgentOrder {
    /// How many recent agents are kept.
    static let recentLimit = 6

    /// The two parts of an agent list. `favorites` is empty when nothing is starred.
    struct Sections<T> {
        var favorites: [T]
        var others: [T]
        var all: [T] { favorites + others }
    }

    static func sections(_ agents: [AgentInfo], favorites: [String]) -> Sections<AgentInfo> {
        sections(agents, id: \.id, name: \.name, favorites: favorites)
    }

    static func sections<T>(
        _ items: [T], id: (T) -> String, name: (T) -> String, favorites: [String]
    ) -> Sections<T> {
        let starred = Set(favorites)
        let sorted = byName(items, name: name)
        return Sections(
            favorites: sorted.filter { starred.contains(id($0)) },
            others: sorted.filter { !starred.contains(id($0)) }
        )
    }

    static func sorted(_ agents: [AgentInfo], favorites: [String]) -> [AgentInfo] {
        sections(agents, favorites: favorites).all
    }

    /// `items` by name as Finder sorts them; equal names keep their order.
    static func byName<T>(_ items: [T], name: (T) -> String) -> [T] {
        items.enumerated().sorted { a, b in
            let order = name(a.element).localizedStandardCompare(name(b.element))
            return order == .orderedSame ? a.offset < b.offset : order == .orderedAscending
        }.map(\.element)
    }

    /// `recent` with `id` moved to the front, at most `recentLimit` long.
    static func used(_ id: String, in recent: [String]) -> [String] {
        Array(([id] + recent.filter { $0 != id }).prefix(recentLimit))
    }

    /// `favorites` with `id` added at the end, or taken out when it is there.
    static func toggled(_ id: String, in favorites: [String]) -> [String] {
        favorites.contains(id) ? favorites.filter { $0 != id } : favorites + [id]
    }

    /// Ids read back from defaults: known shapes only, each once.
    static func ids(from value: Any?) -> [String] {
        guard let list = value as? [String] else { return [] }
        var seen = Set<String>()
        return list.filter { !$0.isEmpty && $0.count <= 64 && seen.insert($0).inserted }
    }
}
