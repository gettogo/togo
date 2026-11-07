import SwiftUI
import TogoKit

/// Every command the harness lists, searchable, with starred and recent ones on top. A
/// command opens its card: what it does, a field for its argument, Send, and its native
/// screen when it has one.
struct CommandPaletteView: View {
    @Environment(\.dismiss) private var dismiss
    let model: CommandLineModel
    let screenTitle: (String) -> String
    @State private var query = ""
    @State private var path: [PaletteRoute] = []

    enum PaletteRoute: Hashable {
        case command(HarnessCompletion)
        case screen(String)
    }

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
                .navigationDestination(for: PaletteRoute.self) { route in
                    switch route {
                    case .command(let item):
                        CommandCard(model: model, item: item, screenTitle: screenTitle) { path.append(.screen($0)) } onSent: {
                            dismiss()
                        }
                    case .screen(let id):
                        HarnessScreenLoader(
                            store: model.store, screenID: id,
                            openScreen: { path.append(.screen($0)) },
                            open: { if $0.kind == .screen { path.append(.screen($0.ref)) } },
                            showTerminal: {
                                dismiss()
                                model.store.layer = .terminal
                            }
                        )
                        .navigationBarTitleDisplayMode(.inline)
                    }
                }
        }
        .task { await model.loadPalette() }
        .presentationDetents([.medium, .large])
        .presentationBackground(Color.togoInk)
    }

    private var list: some View {
        let all = CommandLineLogic.filter(model.palette, query: query.hasPrefix("/") ? String(query.dropFirst()) : query)
        let favourites = model.memory.favourites(model.scope)
        let recents = model.memory.recentCommands(model.scope)
        let word = { (item: HarnessCompletion) in CommandMemory.commandWord(item.insert.isEmpty ? item.title : item.insert) }
        let starred = all.filter { favourites.contains(word($0)) }
        let recent = all.filter { recents.contains(word($0)) && !favourites.contains(word($0)) }
        return List {
            if query.isEmpty, !starred.isEmpty { section(ComposerCopy.favourites, starred) }
            if query.isEmpty, !recent.isEmpty { section(ComposerCopy.recent, recent) }
            if !all.isEmpty { section(ComposerCopy.allCommands, all) }
        }
        .groupedList()
        .overlay {
            if all.isEmpty, !model.palette.isEmpty {
                StateView(kind: .empty, title: ComposerCopy.noCommands, message: ComposerCopy.returnSends)
            } else if model.palette.isEmpty {
                ProgressView()
            }
        }
    }

    private func section(_ title: String, _ items: [HarnessCompletion]) -> some View {
        Section {
            ForEach(items, id: \.self) { item in
                CommandRow(
                    item: item, file: false, highlighted: false, favourite: model.isFavourite(item),
                    opens: item.screen.map(screenTitle)
                ) { path.append(.command(item)) }
                .listRowInsets(EdgeInsets())
                .groupedRows()
                .swipeActions(edge: .leading) {
                    Button(model.isFavourite(item) ? ComposerCopy.unfavourite : ComposerCopy.favourite,
                           systemImage: model.isFavourite(item) ? "star.slash" : "star") { model.toggleFavourite(item) }
                        .tint(Color.togoMute)
                }
                .contextMenu {
                    Button(model.isFavourite(item) ? ComposerCopy.unfavourite : ComposerCopy.favourite,
                           systemImage: model.isFavourite(item) ? "star.slash" : "star") { model.toggleFavourite(item) }
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

/// One command: what it does, its argument, Send, and its native screen when it has one.
private struct CommandCard: View {
    let model: CommandLineModel
    let item: HarnessCompletion
    let screenTitle: (String) -> String
    let openScreen: (String) -> Void
    let onSent: () -> Void
    @State private var argument = ""

    private var takesInput: Bool { item.hint?.isEmpty == false }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if let detail = item.detail, !detail.isEmpty {
                    Text(detail)
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                        .primitiveCard(radius: 14, padding: 12)
                }
                if takesInput {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(ComposerCopy.input).font(.subheadline).foregroundStyle(Color.togoMute)
                        TextField(ComposerCopy.input, text: $argument, prompt: Text(placeholder))
                            .font(.mono(.body))
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .submitLabel(.send)
                            .onSubmit(send)
                            .primitiveWell()
                    }
                }
                if let screen = item.screen {
                    HStack(spacing: 12) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(ComposerCopy.opens(screenTitle(screen))).font(.subheadline.weight(.semibold))
                            Text(ComposerCopy.hasNativeDetail).font(.footnote).foregroundStyle(Color.togoMute)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        Button(ComposerCopy.open) { openScreen(screen) }
                            .buttonStyle(PrimitiveCapsuleStyle(role: .primary))
                    }
                    .foregroundStyle(Color.togoSnow)
                    .primitiveCard(radius: 14, padding: 12)
                }
                Text(ComposerCopy.typesIntoTerminal).font(.footnote).foregroundStyle(Color.togoMute)
            }
            .padding(Layout.margin)
        }
        .background(Color.togoInk)
        .navigationTitle(item.title)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                let starred = model.isFavourite(item)
                Button(starred ? ComposerCopy.unfavourite : ComposerCopy.favourite, systemImage: starred ? "star.fill" : "star") {
                    model.toggleFavourite(item)
                }
            }
        }
        .safeAreaInset(edge: .bottom) {
            Button(ComposerCopy.send, action: send)
                .buttonStyle(PrimitiveCapsuleStyle(role: .primary, large: true))
                .padding(.horizontal, Layout.margin)
                .padding(.bottom, 8)
        }
    }

    private var placeholder: String {
        let hint = (item.hint ?? "").trimmingCharacters(in: CharacterSet(charactersIn: "[]<>"))
        return "\(hint) (\(ComposerCopy.optional))"
    }

    /// "/compact focus on tests": the command, then the argument when one was typed.
    static func line(_ item: HarnessCompletion, argument: String) -> String {
        let command = (item.insert.isEmpty ? item.title : item.insert).trimmingCharacters(in: .whitespaces)
        let arg = argument.trimmingCharacters(in: .whitespacesAndNewlines)
        return arg.isEmpty ? command : "\(command) \(arg)"
    }

    private func send() {
        let line = Self.line(item, argument: argument)
        onSent()
        Task { await model.send(line: line) }
    }
}
