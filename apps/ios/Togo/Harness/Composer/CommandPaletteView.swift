import SwiftUI
import TogoKit

/// Every command the harness lists, searchable, with the session's settings first and starred
/// and recent commands on top. The row is the command (audit 3 §1.2): a choice opens its menu in
/// place, a list pushes its screen, words go to the composer, a run runs (a destructive one asks
/// in a system dialog), a terminal command switches to the terminal. No card in between.
struct CommandPaletteView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.horizontalSizeClass) private var sizeClass
    let model: CommandLineModel
    let screenTitle: (String) -> String
    @State private var query = ""
    @State private var path = NavigationPath()
    @State private var width: CGFloat = 0

    var body: some View {
        NavigationStack(path: $path) {
            list
                .navigationTitle(ComposerCopy.commands)
                .navigationBarTitleDisplayMode(.inline)
                .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: ComposerCopy.filterCommands)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button(PrimitiveCopy.done, systemImage: "checkmark", role: .confirm) { dismiss() }
                    }
                }
                .navigationDestination(for: String.self) { id in
                    HarnessScreenLoader(
                        store: model.store, screenID: id,
                        openScreen: { path.append($0) },
                        open: { if $0.kind == .screen { path.append($0.ref) } },
                        showTerminal: {
                            dismiss()
                            model.store.layer = .terminal
                        },
                        // A pick that lands closes the palette; the chat says what changed.
                        onSettled: { dismiss() }
                    )
                    .navigationBarTitleDisplayMode(.inline)
                }
                // A picker's "Not set up" sign-in pushes here: the agent's page and the
                // outcome stay inside the palette, never a sheet stacked within it.
                .navigationDestination(for: SignInPageModel.self) { page in
                    SignInPage(model: page)
                }
        }
        .environment(\.pushSignIn, PushSignIn { path.append($0) })
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .onAppear {
            // A choice command typed in full and sent opens here, already found.
            if let seed = model.paletteSeed {
                query = seed
                model.paletteSeed = nil
            }
        }
        .task { await model.loadPalette() }
        // The dialog a command asks with shows here while the palette is up.
        .actionConfirmation(Binding(get: { model.gate.asking }, set: { model.gate.asking = $0 }))
    }

    /// A screen a row opens: pushed here, or, for a large list where this sheet is wide, handed
    /// over to its own sheet so it can be a split view (`AdaptivePresentation.opening`).
    private func open(_ screen: String) {
        let content = AdaptivePresentation.content(forScreen: screen, in: model.store.descriptor)
        switch AdaptivePresentation.opening(content, sizeClass: sizeClass, containerWidth: width) {
        case .push: path.append(screen)
        case .handOver: model.handOver(to: screen)
        }
    }

    private var bare: String { query.hasPrefix("/") ? String(query.dropFirst()) : query }

    private var list: some View {
        let rows = model.sessionRows
        let folded = StateChip.folded(rows)
        let all = CommandLineLogic.filter(model.palette, query: bare).filter { !folded.contains(CommandKind.line($0)) }
        let session = rows.filter { row in
            bare.isEmpty || row.title.localizedCaseInsensitiveContains(bare)
                || (row.command.map { CommandKind.line($0).localizedCaseInsensitiveContains(bare) } ?? false)
        }
        let favourites = model.memory.favourites(model.scope)
        let recents = model.memory.recentCommands(model.scope)
        let word = { (item: HarnessCompletion) in CommandMemory.commandWord(CommandKind.line(item)) }
        let starred = all.filter { favourites.contains(word($0)) }
        let recent = all.filter { recents.contains(word($0)) && !favourites.contains(word($0)) }
        return List {
            if !session.isEmpty {
                Section {
                    ForEach(session) { row in
                        SessionSettingRow(model: model, row: row) { open($0) }
                            .listRowInsets(EdgeInsets())
                            .groupedRows()
                    }
                } header: {
                    Text(ComposerCopy.session).textCase(nil)
                }
            }
            if bare.isEmpty, !starred.isEmpty { section(ComposerCopy.favourites, starred) }
            if bare.isEmpty, !recent.isEmpty { section(ComposerCopy.recent, recent) }
            if !all.isEmpty { section(ComposerCopy.allCommands, all) }
            if all.isEmpty, session.isEmpty, !bare.isEmpty, !model.palette.isEmpty || model.paletteFailed {
                // Nothing matches: what was typed can still go as it is.
                Section {
                    Button(ComposerCopy.sendAsTyped("/" + bare), systemImage: "arrow.up") {
                        let line = "/" + bare
                        dismiss()
                        Task { await model.send(line: line) }
                    }
                    .foregroundStyle(Color.togoSnow)
                    .groupedRows()
                }
            }
        }
        .groupedList()
        .overlay { emptyState(nothing: all.isEmpty && session.isEmpty) }
    }

    /// Loading, failed and empty, in the list's place. They swap at once: the list is a sheet's
    /// content read on open, and a load is not news.
    @ViewBuilder private func emptyState(nothing: Bool) -> some View {
        if nothing, bare.isEmpty || model.palette.isEmpty {
            if model.palette.isEmpty, model.paletteFailed {
                StateView(kind: .error, title: ComposerCopy.commandsFailed, message: "", action: .init(ComposerCopy.tryAgain) {
                    Task { await model.loadPalette() }
                })
            } else if model.palette.isEmpty, model.sessionRows.isEmpty {
                ProgressView()
            } else if !model.palette.isEmpty, bare.isEmpty {
                StateView(kind: .empty, title: ComposerCopy.noCommands, message: "")
            }
        }
    }

    private func section(_ title: String, _ items: [HarnessCompletion]) -> some View {
        Section {
            ForEach(items, id: \.self) { item in
                PaletteCommandRow(model: model, item: item, screenTitle: screenTitle) { open($0) }
                    .listRowInsets(EdgeInsets())
                    .groupedRows()
                    .swipeActions(edge: .leading) {
                        Button(model.isFavourite(item) ? ComposerCopy.unfavourite : ComposerCopy.favourite,
                               systemImage: model.isFavourite(item) ? "star.slash" : "star") { model.toggleFavourite(item) }
                            .tint(Color.togoMute)
                    }
                    .accessibilityAction(named: model.isFavourite(item) ? ComposerCopy.unfavourite : ComposerCopy.favourite) {
                        model.toggleFavourite(item)
                    }
            }
        } header: {
            HStack(spacing: 8) {
                Text(title)
                Text(verbatim: "\(items.count)").font(.mono(.caption)).foregroundStyle(Color.togoMute)
            }
            .textCase(nil)
        }
    }
}

/// One command in the palette. A choice is a menu attached to the row; anything else runs by
/// kind when tapped.
private struct PaletteCommandRow: View {
    let model: CommandLineModel
    let item: HarnessCompletion
    let screenTitle: (String) -> String
    let push: (String) -> Void

    var body: some View {
        Group {
            if case let .choose(choices, _) = CommandKind.dispatch(item) {
                Menu {
                    Picker(setting, selection: Binding(
                        get: { CommandKind.current(choices)?.id ?? "" },
                        set: { id in
                            guard let choice = choices.first(where: { $0.id == id }) else { return }
                            Task { await model.pick(choice, of: item, setting: setting) }
                        }
                    )) {
                        ForEach(choices) { Text($0.label).tag($0.id) }
                    }
                    .pickerStyle(.inline)
                } label: {
                    row(value: CommandKind.current(choices)?.label)
                }
            } else {
                Button {
                    Task {
                        if case .push(let screen) = await model.perform(item) { push(screen) }
                    }
                } label: {
                    row(value: nil)
                }
            }
        }
        .buttonStyle(.plain)
        .contextMenu {
            let starred = model.isFavourite(item)
            Button(starred ? ComposerCopy.unfavourite : ComposerCopy.favourite, systemImage: starred ? "star.slash" : "star") {
                model.toggleFavourite(item)
            }
        }
    }

    /// "Effort" for /effort, from its screen, else the command's own name.
    private var setting: String {
        item.screen.map(screenTitle) ?? String(CommandKind.line(item).drop { $0 == "/" })
    }

    private func row(value: String?) -> some View {
        CommandRow(item: item, file: false, highlighted: false, favourite: model.isFavourite(item), value: value)
    }
}

/// A Session row: Model, Effort or Mode with its current value, a menu in place, or the list
/// it opens when it is long.
private struct SessionSettingRow: View {
    let model: CommandLineModel
    let row: StateChip.Row
    let push: (String) -> Void

    var body: some View {
        switch row.control {
        case .mode:
            Menu {
                Picker(row.title, selection: Binding(get: { model.mode }, set: { id in
                    Task { await model.setMode(id) }
                })) {
                    ForEach(model.modes) { Text($0.label).tag($0.id) }
                }
                .pickerStyle(.inline)
            } label: {
                SettingRow(title: row.title, value: model.modeLabel, glyph: "chevron.up.chevron.down")
            }
            .buttonStyle(.plain)
        case let .choices(choices, _):
            Menu {
                Picker(row.title, selection: Binding(
                    get: { model.currentChoice(row)?.id ?? "" },
                    set: { id in
                        guard let choice = choices.first(where: { $0.id == id }) else { return }
                        Task { await model.pick(choice, row: row) }
                    }
                )) {
                    ForEach(choices) { Text($0.label).tag($0.id) }
                }
                .pickerStyle(.inline)
            } label: {
                SettingRow(title: row.title, value: StateChip.value(row, status: model.status, modeLabel: nil),
                           glyph: "chevron.up.chevron.down")
            }
            .buttonStyle(.plain)
        case .screen(let id):
            Button { push(id) } label: {
                SettingRow(title: row.title, value: StateChip.value(row, status: model.status, modeLabel: nil),
                           glyph: "chevron.forward")
            }
            .buttonStyle(.plain)
        }
    }
}

/// "Effort        high  ⌃⌄": the setting, its value in mono, and what a tap does.
private struct SettingRow: View {
    let title: String
    let value: String?
    let glyph: String

    var body: some View {
        HStack(alignment: .center, spacing: 10) {
            Text(title)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .fixedSize()
            Spacer(minLength: 12)
            if let value {
                // One line, trimmed in the middle, so a long model id ("anthropic/claude-sonnet-5")
                // keeps both ends and sits level with its title instead of wrapping.
                Text(verbatim: value)
                    .font(.mono(.subheadline))
                    .foregroundStyle(Color.togoMute)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .layoutPriority(1)
            }
            Image(systemName: glyph)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoMute)
                .accessibilityHidden(true)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 9)
        .frame(minHeight: 44)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(title)
        .accessibilityValue(value ?? "")
        .accessibilityAddTraits(.isButton)
    }
}
