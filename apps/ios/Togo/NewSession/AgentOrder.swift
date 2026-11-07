import Foundation
import TogoKit

/// The order agents are offered in: the user's favorites in the order they were starred, then
/// the agents started most recently, then the rest by name. Favorites and recent starts belong
/// to this device; they never sync.
enum AgentOrder {
    /// How many recent agents are kept.
    static let recentLimit = 6

    static func sorted(_ agents: [AgentInfo], favorites: [String], recent: [String]) -> [AgentInfo] {
        sorted(agents, id: \.id, name: \.name, favorites: favorites, recent: recent)
    }

    static func sorted<T>(
        _ items: [T], id: (T) -> String, name: (T) -> String, favorites: [String], recent: [String]
    ) -> [T] {
        func rank(_ item: T) -> (group: Int, index: Int) {
            if let i = favorites.firstIndex(of: id(item)) { return (0, i) }
            if let i = recent.firstIndex(of: id(item)) { return (1, i) }
            return (2, 0)
        }
        return items.enumerated().sorted { a, b in
            let ra = rank(a.element), rb = rank(b.element)
            if ra.group != rb.group { return ra.group < rb.group }
            if ra.group < 2 { return ra.index < rb.index }
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
