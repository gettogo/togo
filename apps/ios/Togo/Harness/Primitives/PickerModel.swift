import Foundation
import TogoKit

/// A Picker's view state: the search, the filter values, which groups are folded, the group
/// the sidebar shows and an item action waiting for its item. Pure, so the filtering and
/// grouping are tested without a view.
///
/// Filters match an item's `facets` by filter id. A choice filter keeps items whose facet
/// equals the chosen option; "all" (or no choice) keeps everything. A toggle filter that is on
/// keeps items whose facet is true. An item whose facet named after an item action is true
/// holds that action now, such as the model a role uses; the role chips show it.
struct PickerModel: Equatable {
    typealias Item = HarnessScreen.Picker.Item
    typealias Filter = HarnessScreen.Picker.Filter

    /// One section of the list: the items of one group, in the order the adapter sent them.
    struct Group: Equatable, Identifiable {
        /// The group's name, or nil for items without one.
        var name: String?
        var items: [Item]
        /// Items in the group before search and filters.
        var total: Int

        var id: String { name ?? "" }
    }

    static let allOption = "all"

    var query = ""
    /// Filter id to option id for choice filters, or "true" for a toggle that is on.
    var filterValues: [String: String] = [:]
    /// Groups folded shut in the compact list.
    var collapsed: Set<String> = []
    /// The group the sidebar shows in a regular window; nil for every group.
    var sidebarGroup: String?
    /// The item action waiting for the user to tap an item, by action id.
    var picking: String?

    init() {}

    init(_ picker: HarnessScreen.Picker) {
        for filter in picker.filters ?? [] {
            if let value = filter.default, !value.isEmpty { filterValues[filter.id] = value }
        }
    }

    var isSearching: Bool { !query.trimmingCharacters(in: .whitespaces).isEmpty }

    var hasNarrowing: Bool {
        isSearching || filterValues.contains { _, value in value != Self.allOption && value != "false" && !value.isEmpty }
    }

    mutating func reset() {
        query = ""
        filterValues = [:]
        sidebarGroup = nil
    }

    // MARK: Matching

    /// Every word of the query appears in the item's title, subtitle, badge, group or id.
    static func matches(_ item: Item, query: String) -> Bool {
        let words = query.lowercased().split(whereSeparator: \.isWhitespace)
        guard !words.isEmpty else { return true }
        let haystack = [item.title, item.subtitle, item.badge, item.group, item.id]
            .compactMap { $0 }.joined(separator: " ").lowercased()
        return words.allSatisfy { haystack.contains($0) }
    }

    static func matches(_ item: Item, filter: Filter, value: String?) -> Bool {
        guard let value, !value.isEmpty, value != allOption else { return true }
        let facet = item.facets?[filter.id]
        if filter.kind == "toggle" {
            return value != "true" || isTrue(facet)
        }
        return facet == value
    }

    static func isTrue(_ value: String?) -> Bool {
        guard let value = value?.lowercased() else { return false }
        return value == "true" || value == "1" || value == "yes"
    }

    /// Whether the item passes the search, every filter and the sidebar's group.
    func shows(_ item: Item, in picker: HarnessScreen.Picker, except skipped: String? = nil, ignoringGroup: Bool = false) -> Bool {
        guard Self.matches(item, query: query) else { return false }
        for filter in picker.filters ?? [] where filter.id != skipped {
            guard Self.matches(item, filter: filter, value: filterValues[filter.id]) else { return false }
        }
        if !ignoringGroup, let sidebarGroup, item.group != sidebarGroup { return false }
        return true
    }

    func visibleItems(_ picker: HarnessScreen.Picker) -> [Item] {
        picker.items.filter { shows($0, in: picker) }
    }

    /// The visible items in groups, groups in the order they first appear.
    func groups(_ picker: HarnessScreen.Picker) -> [Group] {
        var order: [String?] = []
        var totals: [String: Int] = [:]
        for item in picker.items {
            let key = item.group ?? ""
            if totals[key] == nil { order.append(item.group) }
            totals[key, default: 0] += 1
        }
        let visible = visibleItems(picker)
        return order.compactMap { name in
            let rows = visible.filter { $0.group == name }
            guard !rows.isEmpty else { return nil }
            return Group(name: name, items: rows, total: totals[name ?? ""] ?? rows.count)
        }
    }

    /// Every group name with its item count, for the sidebar.
    static func groupNames(_ picker: HarnessScreen.Picker) -> [(name: String, count: Int)] {
        var order: [String] = []
        var counts: [String: Int] = [:]
        for item in picker.items {
            guard let group = item.group else { continue }
            if counts[group] == nil { order.append(group) }
            counts[group, default: 0] += 1
        }
        return order.map { ($0, counts[$0] ?? 0) }
    }

    /// How many items the option would show, with the search and the other filters applied.
    func count(_ option: String?, of filter: Filter, in picker: HarnessScreen.Picker) -> Int {
        picker.items.filter { item in
            shows(item, in: picker, except: filter.id) && Self.matches(item, filter: filter, value: option)
        }.count
    }

    /// A choice filter's chips: All first, then its options other than an explicit "all".
    static func options(_ filter: Filter) -> [HarnessChoice] {
        [HarnessChoice(id: allOption, label: PrimitiveCopy.all)]
            + (filter.options ?? []).filter { $0.id != allOption && !$0.id.isEmpty }
    }

    func value(of filter: Filter) -> String {
        filterValues[filter.id] ?? (filter.kind == "toggle" ? "false" : Self.allOption)
    }

    /// Whether the group's rows show: always while searching, otherwise unless folded.
    func isExpanded(_ group: Group) -> Bool {
        isSearching || !collapsed.contains(group.id)
    }

    mutating func toggle(_ group: Group) {
        if collapsed.contains(group.id) { collapsed.remove(group.id) } else { collapsed.insert(group.id) }
    }

    // MARK: Item actions

    /// The items that hold an item action now, by the facet named after it.
    static func holders(of action: String, in items: [Item]) -> [Item] {
        items.filter { isTrue($0.facets?[action]) }
    }

    /// The labels of the item actions this item holds.
    static func held(by item: Item, actions: [HarnessActionRef]) -> [String] {
        actions.filter { isTrue(item.facets?[$0.id]) }.map(\.label)
    }

    /// Facets worth showing on an item's card: not the filter values the chips already show,
    /// and not the item action markers.
    static func facts(of item: Item, picker: HarnessScreen.Picker) -> [(key: String, value: String)] {
        let hidden = Set((picker.itemActions ?? []).map(\.id))
        return (item.facets ?? [:]).filter { !hidden.contains($0.key) }
            .sorted { $0.key < $1.key }
            .map { (key: $0.key, value: $0.value) }
    }

    /// What tapping an item does: hand it to the item action waiting for one,
    /// or select it.
    enum Tap: Equatable {
        case assign(action: String)
        case select
        case none
    }

    /// A tap selects, even when items carry actions: those are in swipe and the context menu
    /// (audit 3 §4.3). While a role chip waits for an item, the tap hands it the item.
    func tap(_ item: Item, in picker: HarnessScreen.Picker) -> Tap {
        if item.disabled == true { return .none }
        if let picking { return .assign(action: picking) }
        return item.id == picker.selected ? .none : .select
    }

    /// Whether a selection's result closes its sheet: it landed, was typed, restarted the agent
    /// or waits for the turn's end. A failure keeps the list, with its reason.
    static func closes(_ result: HarnessActionResult?) -> Bool {
        guard let result else { return false }
        return [.applied, .typed, .relaunched, .queued].contains(result.result)
    }

    /// The change a selection makes, for `ActionGate`: the screen's confirm, the item's name,
    /// and the selected item to go back to.
    static func change(selecting item: Item, in picker: HarnessScreen.Picker, setting: String) -> GatedChange {
        let previous = picker.items.first { $0.id == picker.selected }
        return GatedChange(
            action: picker.onSelect, target: item.id, args: args(for: item), confirm: picker.onSelectConfirm,
            setting: setting, value: item.title,
            previous: previous.map { ($0.title, args(for: $0)) }
        )
    }

    /// The args a selection or item action carries: `{"id": item id}`.
    static func args(for item: Item) -> [String: JSONValue] {
        ["id": .string(item.id)]
    }
}
