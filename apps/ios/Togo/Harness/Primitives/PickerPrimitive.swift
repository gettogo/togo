import SwiftUI
import TogoKit

/// A searchable, grouped list with one selection, such as a model or a session to resume.
///
/// Laid out by `AdaptivePresentation.browseLayout`: in a stack (compact widths, or pushed into
/// another stack) the groups are sections, filters are a menu, search is in the navigation
/// bar and an item's detail is pushed. As the root of a wide sheet it is a split view: the
/// groups in a sidebar, the list, and the chosen item's detail beside it. The roles summary
/// heads the list in both. A selection goes through `ActionGate` and closes the host when it
/// lands; a failure stays here with its reason.
struct PickerPrimitive: View {
    @Environment(\.browseLayout) var layout
    @Environment(\.accessibilityReduceMotion) var reduceMotion
    let picker: HarnessScreen.Picker
    /// The screen's title, for the chat's line: "Model Sonnet".
    var title = ""
    let context: PrimitiveContext
    /// The screen's bars, which a split view puts on its list column. nil in a stack, where
    /// the screen view puts them on this whole view.
    let chrome: ScreenChrome?
    @State var model: PickerModel
    @State var actions = PrimitiveActions()
    /// The item whose detail is shown: pushed in a stack, the detail column in a split view.
    @State var focused: PickerModel.Item?
    /// Why the last selection failed, shown at the top of the list at once.
    @State var failure: String?
    /// The item whose selection failed, for Try again.
    @State var failedItem: PickerModel.Item?

    init(picker: HarnessScreen.Picker, title: String = "", context: PrimitiveContext, chrome: ScreenChrome?) {
        self.picker = picker
        self.title = title
        self.context = context
        self.chrome = chrome
        _model = State(initialValue: PickerModel(picker))
    }

    var itemActions: [HarnessActionRef] { picker.itemActions ?? [] }
    var splits: Bool { layout == .split && chrome != nil && PickerModel.groupNames(picker).count > 1 }

    var body: some View {
        Group {
            if splits {
                split
            } else if let chrome {
                // Chosen as a split view, but the groups went: a stack of its own.
                NavigationStack { stack.modifier(chrome) }
            } else {
                stack
            }
        }
        .primitiveActions(actions, context: context)
    }

    private var stack: some View {
        list(split: false)
            .modifier(PickerSearch(enabled: picker.searchable, query: $model.query, count: picker.items.count))
            .toolbar {
                if !PickerModel.menuFilters(picker, sidebar: false).isEmpty {
                    ToolbarItem(placement: .primaryAction) { filterMenu(sidebar: false) }
                }
            }
            // An item's detail pushes on this stack: never a sheet over the sheet.
            .navigationDestination(item: $focused) { item in detail(item) }
    }

    func detail(_ item: PickerModel.Item) -> some View {
        PickerItemDetail(
            item: item, picker: picker, actions: actions, context: context,
            onSelect: { select(item) }, onAssign: { assign($0, item) }
        )
    }

    /// Waiting for an item to give a role changes the list's footer in a `resize` transaction
    /// at the tap, never through an implicit animation on the List.
    func setPicking(_ id: String?) {
        // motion: preventing a jump — the roles footer's words change and the rows below move with it.
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { model.picking = id }
    }

    // MARK: List

    func list(split: Bool) -> some View {
        let groups = model.groups(picker)
        let setup = model.setupItems(picker)
        return List(selection: split ? listSelection : nil) {
            if let failure {
                // Errors land at once, at the list that failed (motion principles v2 §2).
                VStack(alignment: .leading, spacing: 8) {
                    Label(failure, systemImage: "exclamationmark.circle.fill")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(PrimitiveTint.fail)
                        .fixedSize(horizontal: false, vertical: true)
                    if let failedItem {
                        Button(PrimitiveCopy.tryAgain) { select(failedItem) }
                            .buttonStyle(.secondary)
                            .disabled(actions.isBusy)
                    }
                }
                .listRowBackground(Color.clear)
            }
            if !itemActions.isEmpty, !model.isSearching {
                PickerRolesSection(refs: itemActions, items: picker.items, picking: model.picking) { id in
                    setPicking(model.picking == id ? nil : id)
                }
            }
            ForEach(groups) { group in
                Section {
                    if split || model.isExpanded(group, alone: groups.count == 1) {
                        ForEach(group.items) { item in row(item, split: split) }
                    }
                } header: {
                    if groups.count > 1 || group.name != nil {
                        groupHeader(group, alone: groups.count == 1, foldable: !split)
                    }
                }
            }
            if let action = picker.setupAction, !setup.isEmpty {
                PickerSetupSection(
                    items: setup, total: picker.setup?.count ?? setup.count, action: action,
                    asksSecret: picker.setupInput == "secret", expanded: model.isSetupExpanded,
                    searching: model.isSearching, actions: actions,
                    toggle: { model.toggle(id: PickerModel.setupID) },
                    run: { item, value in setUp(item, with: action, value: value) }
                )
            }
        }
        .groupedList()
        .overlay {
            // Search results never animate (motion v2 §2): "No matches" is simply there.
            if groups.isEmpty, setup.isEmpty { empty }
        }
    }

    /// The split view's list selection is the item in the detail column; choosing a row while
    /// a role waits gives it the role instead.
    private var listSelection: Binding<String?> {
        Binding(
            get: { focused?.id ?? picker.selected },
            set: { id in
                guard let id, let item = picker.items.first(where: { $0.id == id }) else { return }
                if case .assign(let action) = model.tap(item, in: picker),
                   let ref = itemActions.first(where: { $0.id == action }) {
                    assign(ref, item)
                } else {
                    focused = item
                }
            }
        )
    }

    @ViewBuilder private func row(_ item: PickerModel.Item, split: Bool) -> some View {
        let label = PickerRowLabel(
            item: item, picker: picker, selected: item.id == picker.selected,
            held: PickerModel.held(by: item, actions: itemActions),
            running: actions.isRunning(picker.onSelect, target: item.id)
                || itemActions.contains { actions.isRunning($0.id, target: item.id) }
        )
        Group {
            if split {
                // The list's selection drives the detail column; the row is not a button.
                label.tag(item.id)
            } else {
                Button { tap(item) } label: { label }
                    .buttonStyle(.plain)
                    .disabled(item.disabled == true)
                    .accessibilityHint(model.picking != nil ? PrimitiveCopy.select : "")
            }
        }
        .groupedRows()
        .contextMenu { menu(for: item) }
        .swipeActions(edge: .trailing) {
            // The item's roles: the tap selects, so these sit one swipe away.
            ForEach(itemActions.prefix(3)) { ref in
                Button(ref.label) { assign(ref, item) }
                    .tint(Color.togoMute)
            }
        }
        .accessibilityActions {
            if item.id != picker.selected { Button(PrimitiveCopy.select) { select(item) } }
            ForEach(itemActions) { ref in
                Button("\(PrimitiveCopy.useAs) \(ref.label)") { assign(ref, item) }
            }
            Button(PrimitiveCopy.details) { focused = item }
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
        }
        Button(PrimitiveCopy.details, systemImage: "info.circle") { focused = item }
        Button(PrimitiveCopy.copyID, systemImage: "doc.on.doc") { copy(item.id) }
        Button(PrimitiveCopy.openTerminal, systemImage: "terminal") { context.showTerminal() }
    }

    private func groupHeader(_ group: PickerModel.Group, alone: Bool, foldable: Bool) -> some View {
        let count = model.hasNarrowing ? group.items.count : group.total
        let folds = foldable && !model.isSearching && !alone
        return Button {
            guard folds else { return }
            // motion: cause → effect — the group folds where it was tapped; rows below glide.
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { model.toggle(group) }
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(verbatim: group.name ?? PrimitiveCopy.other)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                Text(verbatim: "\(count)")
                    .font(.mono(.caption))
                    .foregroundStyle(Color.togoMute)
                Spacer(minLength: 8)
                if folds {
                    Image(systemName: model.isExpanded(group) ? "chevron.down" : "chevron.right")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(Color.togoMute)
                }
            }
            .frame(minHeight: 32)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .textCase(nil)
        .accessibilityAddTraits(.isHeader)
        .accessibilityValue(!folds ? "" : model.isExpanded(group)
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

    /// The filters as a system menu in the bar: on a compact width every filter, beside a
    /// sidebar only those it does not already show.
    func filterMenu(sidebar: Bool) -> some View {
        Menu {
            ForEach(PickerModel.menuFilters(picker, sidebar: sidebar)) { filter in
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
                        ForEach(PickerModel.options(filter)) { option in
                            Text(verbatim: "\(option.label)  \(model.count(option.id, of: filter, in: picker))").tag(option.id)
                        }
                    }
                    .pickerStyle(.inline)
                }
            }
        } label: {
            Label(PrimitiveCopy.filters, systemImage: "line.3.horizontal.decrease")
        }
        .badge(model.hasNarrowing && !model.isSearching ? 1 : 0)
    }
}

/// `.searchable` in the navigation bar when the picker asks for it: never a field floating
/// over the rows.
struct PickerSearch: ViewModifier {
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
