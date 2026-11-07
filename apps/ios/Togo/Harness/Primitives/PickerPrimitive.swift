import SwiftUI
import TogoKit

/// A searchable, grouped list with one selection, such as a model or a session to resume.
///
/// Compact width: role chips, filter chips and the groups as sections that fold. Regular
/// width: the groups move into a sidebar beside the list. Tapping an item selects it, hands it
/// to the role chip waiting for one, or, when items carry actions, opens its card. Every item
/// action is also in the row's context menu and VoiceOver actions.
struct PickerPrimitive: View {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let picker: HarnessScreen.Picker
    let context: PrimitiveContext
    @State private var model: PickerModel
    @State private var actions = PrimitiveActions()
    @State private var card: PickerModel.Item?

    init(picker: HarnessScreen.Picker, context: PrimitiveContext) {
        self.picker = picker
        self.context = context
        _model = State(initialValue: PickerModel(picker))
    }

    private var itemActions: [HarnessActionRef] { picker.itemActions ?? [] }
    private var groupNames: [(name: String, count: Int)] { PickerModel.groupNames(picker) }
    private var usesSidebar: Bool { sizeClass == .regular && groupNames.count > 1 }

    var body: some View {
        content
            .modifier(PickerSearch(enabled: picker.searchable, query: $model.query, count: picker.items.count))
            .toolbar {
                if !(picker.filters ?? []).isEmpty {
                    ToolbarItem(placement: .primaryAction) { filterMenu }
                }
            }
            .primitiveActions(actions, context: context)
            .sheet(item: $card) { item in
                PickerItemCard(item: item, picker: picker, parent: actions, context: context)
            }
    }

    /// Picking mode and group folds change the List in a `resize` transaction at the tap, never
    /// through an implicit animation on the List.
    private func setPicking(_ id: String?) {
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { model.picking = id }
    }

    @ViewBuilder private var content: some View {
        if usesSidebar {
            HStack(spacing: 0) {
                PickerSidebar(names: groupNames, total: picker.items.count, selection: $model.sidebarGroup)
                    .frame(width: 260)
                Rectangle().fill(Color.togoLine).frame(width: 1).accessibilityHidden(true)
                list
            }
            .background(Color.togoInk)
        } else {
            list
        }
    }

    private var list: some View {
        let groups = model.groups(picker)
        return List {
            if !itemActions.isEmpty || hasChips {
                Section {
                    VStack(alignment: .leading, spacing: 10) {
                        if !itemActions.isEmpty { roleChips }
                        if let picking = model.picking, let ref = itemActions.first(where: { $0.id == picking }) {
                            pickBanner(ref)
                                .transition(Motion.revealConceal(reduce: reduceMotion))
                        }
                        if hasChips { filterChips }
                    }
                    .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 4, trailing: 0))
                    .listRowBackground(Color.clear)
                }
            }
            ForEach(groups) { group in
                Section {
                    if model.isExpanded(group) || usesSidebar {
                        ForEach(group.items) { item in row(item) }
                    }
                } header: {
                    if groups.count > 1 || group.name != nil {
                        groupHeader(group)
                    }
                }
            }
        }
        .groupedList()
        .overlay {
            Group {
                if groups.isEmpty { empty.transition(.opacity) }
            }
            .motion(.fade, value: groups.isEmpty)
        }
    }

    // MARK: Rows

    private func row(_ item: PickerModel.Item) -> some View {
        PickerRow(
            item: item, selected: item.id == picker.selected,
            held: PickerModel.held(by: item, actions: itemActions),
            running: actions.isRunning(picker.onSelect, target: item.id)
                || itemActions.contains { actions.isRunning($0.id, target: item.id) },
            picking: model.picking != nil
        ) { tap(item) }
        .groupedRows()
        .contextMenu { menu(for: item) }
        .swipeActions(edge: .leading) {
            if item.id != picker.selected, item.disabled != true {
                Button(PrimitiveCopy.select, systemImage: "checkmark") { select(item) }
                    .tint(Color.togoSnow)
            }
        }
        .accessibilityActions {
            if item.id != picker.selected { Button(PrimitiveCopy.select) { select(item) } }
            ForEach(itemActions) { ref in
                Button("\(PrimitiveCopy.useAs) \(ref.label)") { assign(ref, item) }
            }
            if !itemActions.isEmpty { Button(PrimitiveCopy.details) { card = item } }
        }
    }

    @ViewBuilder private func menu(for item: PickerModel.Item) -> some View {
        if item.id != picker.selected, item.disabled != true {
            Button(PrimitiveCopy.select, systemImage: "checkmark") { select(item) }
        }
        if !itemActions.isEmpty {
            Section(PrimitiveCopy.useAs) {
                ForEach(itemActions) { ref in
                    Button(ref.label, systemImage: PickerModel.isTrue(item.facets?[ref.id]) ? "checkmark" : "circle") {
                        assign(ref, item)
                    }
                }
            }
            Button(PrimitiveCopy.details, systemImage: "info.circle") { card = item }
        }
        Button(PrimitiveCopy.copyID, systemImage: "doc.on.doc") { copy(item.id) }
        Button(PrimitiveCopy.openTerminal, systemImage: "terminal") { context.showTerminal() }
    }

    private func tap(_ item: PickerModel.Item) {
        switch model.tap(item, in: picker) {
        case .assign(let id):
            if let ref = itemActions.first(where: { $0.id == id }) { assign(ref, item) }
        case .details:
            card = item
        case .select:
            select(item)
        case .none:
            break
        }
    }

    private func select(_ item: PickerModel.Item) {
        Task { await actions.perform(picker.onSelect, target: item.id, args: PickerModel.args(for: item), context: context) }
    }

    private func assign(_ ref: HarnessActionRef, _ item: PickerModel.Item) {
        setPicking(nil)
        Task { await actions.trigger(ref, target: item.id, args: PickerModel.args(for: item), context: context) }
    }

    private func copy(_ text: String) {
        UIPasteboard.general.string = text
        actions.show(PrimitiveFeedback(kind: .done, title: PrimitiveCopy.copied, detail: text))
    }

    // MARK: Chips and headers

    private var roleChips: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) {
                ForEach(itemActions) { ref in
                    PickerRoleChip(
                        label: ref.label,
                        holder: PickerModel.holders(of: ref.id, in: picker.items).first?.title,
                        on: model.picking == ref.id
                    ) {
                        setPicking(model.picking == ref.id ? nil : ref.id)
                    }
                }
            }
            .padding(.horizontal, 16)
        }
        .scrollIndicators(.hidden)
    }

    private func pickBanner(_ ref: HarnessActionRef) -> some View {
        HStack(spacing: 10) {
            Text(PrimitiveCopy.choosing(ref.label))
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(PrimitiveCopy.cancel) { setPicking(nil) }
                .buttonStyle(PrimitiveCapsuleStyle(role: .normal))
        }
        .padding(.leading, 14)
        .padding(.trailing, 6)
        .padding(.vertical, 4)
        .background(Color.togoSnow.opacity(0.08), in: .rect(cornerRadius: 14, style: .continuous))
        .padding(.horizontal, 16)
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(.updatesFrequently)
    }

    private var chipFilters: [HarnessScreen.Picker.Filter] {
        let filters = picker.filters ?? []
        let choice = filters.first { $0.kind != "toggle" && ($0.options?.count ?? 0) <= 8 }
        return (choice.map { [$0] } ?? []) + filters.filter { $0.kind == "toggle" }
    }

    private var hasChips: Bool { !chipFilters.isEmpty }

    private var filterChips: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 6) {
                ForEach(chipFilters) { filter in
                    if filter.kind == "toggle" {
                        let on = model.value(of: filter) == "true"
                        Button { model.filterValues[filter.id] = on ? "false" : "true" } label: {
                            Text(filter.label)
                        }
                        .buttonStyle(PrimitiveToggleChipStyle(on: on))
                        .accessibilityAddTraits(on ? .isSelected : [])
                    } else {
                        ForEach(PickerModel.options(filter)) { option in
                            let on = model.value(of: filter) == option.id
                            Button { model.filterValues[filter.id] = option.id } label: {
                                HStack(alignment: .firstTextBaseline, spacing: 5) {
                                    Text(option.label)
                                    Text(verbatim: "\(model.count(option.id, of: filter, in: picker))")
                                        .font(.mono(.caption2))
                                        .opacity(0.6)
                                }
                            }
                            .buttonStyle(PrimitiveToggleChipStyle(on: on))
                            .accessibilityAddTraits(on ? .isSelected : [])
                        }
                    }
                }
            }
            .padding(.horizontal, 16)
        }
        .scrollIndicators(.hidden)
        .motion(.respond, value: model.filterValues)
    }

    private var filterMenu: some View {
        Menu {
            ForEach(picker.filters ?? []) { filter in
                if filter.kind == "toggle" {
                    Toggle(filter.label, isOn: Binding(
                        get: { model.value(of: filter) == "true" },
                        set: { model.filterValues[filter.id] = $0 ? "true" : "false" }
                    ))
                } else {
                    Picker(filter.label, selection: Binding(
                        get: { model.value(of: filter) },
                        set: { model.filterValues[filter.id] = $0 }
                    )) {
                        ForEach(PickerModel.options(filter)) { Text($0.label).tag($0.id) }
                    }
                    .pickerStyle(.inline)
                }
            }
        } label: {
            Label(PrimitiveCopy.filters, systemImage: "line.3.horizontal.decrease")
        }
        .badge(model.hasNarrowing && !model.isSearching ? 1 : 0)
    }

    private func groupHeader(_ group: PickerModel.Group) -> some View {
        let count = model.hasNarrowing ? group.items.count : group.total
        return Button {
            guard !usesSidebar, !model.isSearching else { return }
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { model.toggle(group) }
        } label: {
            HStack(spacing: 8) {
                Text(verbatim: group.name ?? PrimitiveCopy.other)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                Text(verbatim: "\(count)")
                    .font(.mono(.caption))
                    .foregroundStyle(Color.togoMute)
                Spacer(minLength: 8)
                if !usesSidebar, !model.isSearching {
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(Color.togoMute)
                        .rotationEffect(.degrees(model.isExpanded(group) && !reduceMotion ? 90 : 0))
                }
            }
            .frame(minHeight: 32)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .textCase(nil)
        .accessibilityAddTraits(.isHeader)
        .accessibilityValue(usesSidebar || model.isSearching ? "" : model.isExpanded(group)
            ? String(localized: "primitive.expanded", defaultValue: "Expanded")
            : String(localized: "primitive.collapsed", defaultValue: "Collapsed"))
    }

    private var empty: some View {
        StateView(
            kind: .empty, title: model.hasNarrowing ? PrimitiveCopy.noMatches : PrimitiveCopy.nothingHere,
            message: model.hasNarrowing ? PrimitiveCopy.tryShorter : "",
            action: model.hasNarrowing ? StateView.Action(PrimitiveCopy.showAll) { model.reset() } : nil
        )
    }
}

/// `.searchable` when the picker asks for it.
private struct PickerSearch: ViewModifier {
    let enabled: Bool
    @Binding var query: String
    let count: Int

    func body(content: Content) -> some View {
        if enabled {
            content.searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: PrimitiveCopy.searchCount(count))
        } else {
            content
        }
    }
}
