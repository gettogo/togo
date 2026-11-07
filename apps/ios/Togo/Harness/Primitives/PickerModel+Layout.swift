import Foundation
import TogoKit

/// What a Picker shows where, apart from the views: which filters the groups already cover,
/// the sidebar's order, and what each row says. Pure, so it is tested without a view.
extension PickerModel {
    /// Choice filters that say the same as the groups: every item's facet for the filter
    /// pairs with exactly one group and back (omp's provider filter and its provider groups,
    /// Kilo's provider id and its provider names). The sidebar or the sections already show
    /// them, so they are never offered twice.
    static func groupFilterIDs(_ picker: HarnessScreen.Picker) -> Set<String> {
        guard groupNames(picker).count > 1 else { return [] }
        var ids: Set<String> = []
        for filter in picker.filters ?? [] where filter.kind != "toggle" {
            var groupFor: [String: String] = [:]
            var valueFor: [String: String] = [:]
            var pairs = true
            for item in picker.items {
                guard let group = item.group, let value = item.facets?[filter.id] else { continue }
                if groupFor[value, default: group] != group || valueFor[group, default: value] != value {
                    pairs = false
                    break
                }
                groupFor[value] = group
                valueFor[group] = value
            }
            if pairs, !groupFor.isEmpty { ids.insert(filter.id) }
        }
        return ids
    }

    /// The filters the filter menu offers: not the markers of who holds a role (the roles
    /// section shows those), and not the ones the sidebar shows when there is a sidebar.
    static func menuFilters(_ picker: HarnessScreen.Picker, sidebar: Bool) -> [Filter] {
        let roles = Set((picker.itemActions ?? []).map(\.id))
        let grouped = sidebar ? groupFilterIDs(picker) : []
        return (picker.filters ?? []).filter { !roles.contains($0.id) && !grouped.contains($0.id) }
    }

    /// The sidebar's groups: the selection's group first (where the user most likely goes),
    /// then the others in the adapter's order, each with its count.
    static func sidebarGroups(_ picker: HarnessScreen.Picker) -> [(name: String, count: Int)] {
        let names = groupNames(picker)
        guard let current = picker.items.first(where: { $0.id == picker.selected })?.group,
              let index = names.firstIndex(where: { $0.name == current }) else { return names }
        var ordered = names
        ordered.insert(ordered.remove(at: index), at: 0)
        return ordered
    }

    /// The machine name under a row's title in mono, such as omp's "anthropic/claude-opus-4-1":
    /// shown for a catalog in groups, where the id is the selector the agent takes. Session ids
    /// in an ungrouped Resume list are noise and stay on the detail.
    static func selector(of item: Item, in picker: HarnessScreen.Picker) -> String? {
        guard groupNames(picker).count > 1, item.id != item.title else { return nil }
        return item.id
    }

    /// The short facts a row shows as chips: in a catalog the subtitle ("64K context"), the
    /// badge, and every toggle facet that is on (Reasoning, Images), each once. An ungrouped
    /// list keeps its subtitle as a line of text (a Resume row's "2 h ago · main").
    static func chips(of item: Item, in picker: HarnessScreen.Picker) -> [String] {
        var chips: [String] = []
        func add(_ text: String?) {
            guard let text = text?.trimmingCharacters(in: .whitespaces), !text.isEmpty,
                  !chips.contains(where: { $0.caseInsensitiveCompare(text) == .orderedSame }) else { return }
            chips.append(text)
        }
        let catalog = groupNames(picker).count > 1
        if catalog { add(item.subtitle) }
        add(item.badge)
        let roles = Set((picker.itemActions ?? []).map(\.id))
        for filter in picker.filters ?? [] where filter.kind == "toggle" && !roles.contains(filter.id) {
            if isTrue(item.facets?[filter.id]) { add(filter.label) }
        }
        return chips
    }

    /// The subtitle as a line of text, where it is not already a chip.
    static func subtitleLine(of item: Item, in picker: HarnessScreen.Picker) -> String? {
        guard groupNames(picker).count <= 1, let subtitle = item.subtitle, !subtitle.isEmpty else { return nil }
        return subtitle
    }
}
