import SwiftUI
import TogoKit

/// The picker as the root of a wide sheet: a `NavigationSplitView` with the groups in the
/// sidebar, the list with the roles summary and its bars in the middle, and the chosen item's
/// detail in the last column (the current selection's until another is chosen). Search sits on
/// the split view, so it applies to every column. A compact width collapses it to the list.
extension PickerPrimitive {
    var split: some View {
        SplitColumns(picker: self)
    }

    // MARK: Actions

    func tap(_ item: PickerModel.Item) {
        switch model.tap(item, in: picker) {
        case .assign(let id):
            if let ref = itemActions.first(where: { $0.id == id }) { assign(ref, item) }
        case .select:
            select(item)
        case .none:
            break
        }
    }

    /// Selects through `ActionGate`: a restart asks while the agent works, and runs with Undo
    /// while it is idle. Landing closes the host; failing keeps the list with the reason.
    func select(_ item: PickerModel.Item) {
        failure = nil
        failedItem = nil
        let change = PickerModel.change(selecting: item, in: picker, setting: title)
        Task {
            await actions.change(change, context: context) { result in
                if PickerModel.closes(result) {
                    context.settled?()
                } else if let result, result.result == .failed {
                    // The agent did not take it (omp's model switch is read back): say so here,
                    // with Try again, and never as if it had.
                    failure = result.message ?? PrimitiveCopy.failed
                    failedItem = item
                    Task { await context.reload?() }
                }
            }
        }
    }

    func assign(_ ref: HarnessActionRef, _ item: PickerModel.Item) {
        setPicking(nil)
        Task { await actions.trigger(ref, target: item.id, args: PickerModel.args(for: item), place: .toast, context: context) }
    }

    /// Sets up a "Not set up" entry: omp's own sign-in typed into the session, or an API key
    /// sent to the machine. A sign-in pushes "Sign in to Anthropic" inside this sheet, where
    /// the agent's page, the sign-in browser and the outcome all show; where there is no stack
    /// to push on, the picker stays up and the page comes as a sheet over it. Closing the
    /// picker here would take that sheet down with it.
    func setUp(_ item: PickerModel.Item, with ref: HarnessActionRef, value: String?) {
        var args = PickerModel.args(for: item)
        if let value { args["value"] = .string(value) }
        let actions = actions
        let context = context
        if value == nil, let push = context.signIn, let subject = context.subject {
            let page = SignInPageModel(
                provider: item.title, itemID: item.id, agent: context.agent,
                hostID: subject.hostID, sessionID: subject.sessionID,
                send: { await actions.perform(ref.id, target: item.id, args: args, place: .none, context: context) },
                read: context.readScreen, refresh: context.reload
            )
            // Back on the list, the provider just set up shows its models, group open.
            page.onSetUp = {
                failure = nil
                failedItem = nil
                model.reveal(setUp: item)
            }
            page.start()
            push(page)
            return
        }
        Task {
            let result = await actions.perform(ref.id, target: item.id, args: args, place: .toast, context: context)
            // A key landed: the picker closes to the chat. A sign-in keeps it up (see above).
            if value != nil, result?.result == .typed { context.settled?() }
        }
    }

    func copy(_ text: String) {
        UIPasteboard.general.string = text
        actions.show(PrimitiveFeedback(kind: .done, title: PrimitiveCopy.copied, detail: text))
    }
}

/// The three columns. Its own view, so the column state lives with the split view.
private struct SplitColumns: View {
    @Environment(\.screenDone) private var done
    let picker: PickerPrimitive
    @State private var columns = NavigationSplitViewVisibility.all
    @State private var compactColumn = NavigationSplitViewColumn.content

    private var source: HarnessScreen.Picker { picker.picker }

    var body: some View {
        NavigationSplitView(columnVisibility: $columns, preferredCompactColumn: $compactColumn) {
            PickerSidebar(
                names: PickerModel.sidebarGroups(source), total: source.items.count,
                selection: picker.$model.sidebarGroup
            )
            .navigationTitle(sidebarTitle)
        } content: {
            content
        } detail: {
            NavigationStack { detail }
                // Nothing in this column nests another split view.
                .environment(\.browseLayout, .stack)
        }
        .navigationSplitViewStyle(.balanced)
        .modifier(SplitSearch(enabled: source.searchable, query: picker.$model.query, count: source.items.count))
    }

    @ViewBuilder private var content: some View {
        let list = picker.list(split: true)
            .toolbar {
                if let done { done.item }
                if !PickerModel.menuFilters(source, sidebar: true).isEmpty {
                    ToolbarItem(placement: .primaryAction) { picker.filterMenu(sidebar: true) }
                }
            }
            .navigationBarTitleDisplayMode(.inline)
        if let chrome = picker.chrome { list.modifier(chrome) } else { list }
    }

    @ViewBuilder private var detail: some View {
        if let item = picker.focused ?? source.items.first(where: { $0.id == source.selected }) {
            picker.detail(item)
        } else {
            ContentUnavailableView(
                PrimitiveCopy.noItemChosen, systemImage: "sidebar.right",
                description: Text(PrimitiveCopy.chooseForDetails)
            )
            .background(Color.togoInk)
        }
    }

    /// The sidebar's title: the filter the groups stand for ("Provider"), else "Groups".
    private var sidebarTitle: String {
        let ids = PickerModel.groupFilterIDs(source)
        return source.filters?.first { ids.contains($0.id) }?.label ?? PrimitiveCopy.groups
    }
}

/// Search on the split view, in its navigation bar, for every column.
private struct SplitSearch: ViewModifier {
    let enabled: Bool
    @Binding var query: String
    let count: Int

    func body(content: Content) -> some View {
        if enabled {
            content.searchable(text: $query, prompt: PrimitiveCopy.searchCount(count))
        } else {
            content
        }
    }
}
