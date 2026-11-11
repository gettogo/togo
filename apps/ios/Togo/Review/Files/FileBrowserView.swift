import SwiftUI
import TogoKit
import UIKit

/// The working tree, or a commit's, as a tree: folders open in place, a file opens in the viewer.
/// Changed files carry a letter and the folders holding them a dot. The search field looks
/// through every file of the repository. Hidden files are behind a toggle, and a banner says
/// when the tree is as a commit had it.
struct FileBrowserView: View {
    @State private var model: FilesModel
    private let onOpen: (ReviewRoute) -> Void

    init(model: FilesModel, onOpen: @escaping (ReviewRoute) -> Void) {
        _model = State(initialValue: model)
        self.onOpen = onOpen
    }

    var body: some View {
        content
            .phaseSwap(phase)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            .safeAreaInset(edge: .top, spacing: 0) { banner }
            .navigationTitle(Text(verbatim: model.title))
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: Bindable(model.search).query, prompt: FileTreeCopy.searchPrompt)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Toggle(FilesCopy.hidden, isOn: $model.showsHidden)
                            .menuToggle()
                    } label: {
                        Label(FilesCopy.options, systemImage: "slider.horizontal.3")
                    }
                }
            }
            .task { await model.loadIfNeeded() }
    }

    private var phase: String {
        if model.search.isActive { return "search" }
        return model.items.isEmpty ? "\(CaseName.of(model.state)) empty" : CaseName.of(model.state)
    }

    @ViewBuilder private var content: some View {
        if model.search.isActive {
            FileSearchResults(search: model.search, onOpen: onOpen)
        } else {
            switch model.state {
            case .loading:
                StateView(kind: .loading, title: FilesCopy.loading, message: model.breadcrumb.isEmpty ? model.subject.title : model.breadcrumb)
            case .failed(let message):
                StateView(kind: .error, title: FilesCopy.failed, message: message, action: .init(FilesCopy.tryAgain) {
                    Task { await model.load() }
                })
            case .ready:
                if model.items.isEmpty {
                    StateView(
                        kind: .empty, title: FilesCopy.emptyFolder,
                        message: model.hiddenCount > 0 ? FilesCopy.onlyHidden : FilesCopy.emptyMessage
                    )
                } else {
                    tree
                }
            }
        }
    }

    private var tree: some View {
        List {
            Section {
                ForEach(model.items) { item in
                    TreeNode(model: model, item: item, onOpen: onOpen)
                }
            } header: {
                if !model.breadcrumb.isEmpty { PathText(model.breadcrumb) }
            }
        }
        .groupedList()
        .refreshable { await model.load() }
    }

    @ViewBuilder private var banner: some View {
        if let text = model.refBanner {
            RefBanner(text: text) { onOpen(model.workingTreeRoute) }
        }
    }
}

/// A folder that opens in place, or a file that opens in the viewer.
private struct TreeNode: View {
    let model: FilesModel
    let item: FilesModel.Item
    let onOpen: (ReviewRoute) -> Void

    var body: some View {
        switch item.kind {
        case .folder:
            DisclosureGroup(isExpanded: expanded) {
                children
            } label: {
                ItemRow(item: item, mark: nil, holdsChanges: model.marks.holdsChanges(item.path), expanded: model.isExpanded(item.path))
            }
            .groupedRows()
        case .file:
            if let route = item.route {
                Button { onOpen(route) } label: {
                    ItemRow(item: item, mark: model.marks.mark(item.path), holdsChanges: false, expanded: nil)
                }
                .buttonStyle(.plain)
                .reviewRow(opening: route)
                .contextMenu {
                    if let diff = model.changesRoute(for: item) {
                        Button(FileTreeCopy.showChanges, systemImage: "plus.forwardslash.minus") { onOpen(diff) }
                    }
                }
            }
        case .link:
            ItemRow(item: item, mark: nil, holdsChanges: false, expanded: nil)
                .groupedRows()
        }
    }

    private var expanded: Binding<Bool> {
        Binding {
            model.isExpanded(item.path)
        } set: { open in
            // motion: cause → effect — the folder's rows grow out of it as it opens
            withAnimation(Motion.animation(.rowGrow, reduce: UIAccessibility.isReduceMotionEnabled)) {
                if open { _ = model.expanded.insert(item.path) } else { _ = model.expanded.remove(item.path) }
            }
            if open { Task { await model.setExpanded(item.path, true) } }
        }
    }

    @ViewBuilder private var children: some View {
        switch model.folders[item.path] {
        case .ready?:
            let rows = model.children(of: item.path) ?? []
            if rows.isEmpty {
                Text(FilesCopy.emptyFolder)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .groupedRows()
            }
            ForEach(rows) { child in
                TreeNode(model: model, item: child, onOpen: onOpen)
            }
        case .failed(let message)?:
            VStack(alignment: .leading, spacing: 8) {
                Text(message).font(.footnote).foregroundStyle(Color.togoMute)
                Button(FilesCopy.tryAgain) { Task { await model.read(item.path) } }
                    .buttonStyle(.secondary(.fit))
            }
            .groupedRows()
        case .loading?, nil:
            HStack(spacing: 8) {
                ProgressView()
                Text(FileTreeCopy.opening).font(.footnote).foregroundStyle(Color.togoMute)
            }
            .frame(minHeight: 44)
            .groupedRows()
        }
    }
}

/// Files whose name or path holds the query, best first, each opening in the viewer.
private struct FileSearchResults: View {
    let search: FileSearch
    let onOpen: (ReviewRoute) -> Void

    var body: some View {
        switch search.state {
        case .idle:
            StateView(kind: .loading, title: FileTreeCopy.searching, message: search.query)
        case .searching where search.hits.isEmpty:
            StateView(kind: .loading, title: FileTreeCopy.searching, message: search.query)
        case .failed(let message):
            StateView(kind: .error, title: FileTreeCopy.searchFailed, message: message)
        default:
            if search.hits.isEmpty {
                StateView(kind: .empty, title: FileTreeCopy.noResults, message: FileTreeCopy.noResultsMessage(search.query))
            } else {
                list
            }
        }
    }

    private var list: some View {
        List {
            Section {
                ForEach(search.hits) { hit in
                    Button { onOpen(hit.route) } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: hit.name)
                                .font(.body)
                                .foregroundStyle(Color.togoSnow)
                            if !hit.folder.isEmpty { PathText(hit.folder) }
                        }
                        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                        .contentShape(.rect)
                        .accessibilityElement(children: .combine)
                    }
                    .buttonStyle(.plain)
                    .reviewRow(opening: hit.route)
                }
            } footer: {
                SectionFooter {
                    if search.partial {
                        Text(FileTreeCopy.partial)
                    } else if search.more {
                        Text(FileTreeCopy.moreResults(FileSearch.limit))
                    }
                }
            }
        }
        .groupedList()
    }
}

/// Says the files shown are a commit's, with the way back.
struct RefBanner: View {
    let text: String
    let back: () -> Void

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 12) { label; Spacer(minLength: 0); button }
            VStack(alignment: .leading, spacing: 4) { label; button }
        }
        .padding(.horizontal, Layout.margin)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.togoRaised)
        .overlay(alignment: .bottom) { Color.togoLine.frame(height: 1) }
    }

    private var label: some View {
        Text(verbatim: text)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(Color.togoSnow)
    }

    private var button: some View {
        Button(FilesCopy.backToWorkingTree, action: back)
            .buttonStyle(.secondary(.fit))
            .font(.footnote)
    }
}

private struct ItemRow: View {
    let item: FilesModel.Item
    /// A changed file's mark.
    let mark: ChangeMarks.Mark?
    /// A folder with a change somewhere inside it.
    let holdsChanges: Bool
    /// A folder's state, for its symbol; nil for a file.
    let expanded: Bool?

    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .body) private var iconWidth: CGFloat = 24

    var body: some View {
        Group {
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(alignment: .firstTextBaseline, spacing: 12) {
                        icon
                        name
                        changeMark
                    }
                    detail.padding(.leading, iconWidth + 12)
                }
            } else {
                HStack(spacing: 12) {
                    icon
                    name
                    Spacer(minLength: 8)
                    detail
                    changeMark
                }
            }
        }
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: spoken))
        .accessibilityHint(item.kind == .link ? FilesCopy.linkHint : "")
        .accessibilityAddTraits(item.kind == .file && item.route != nil ? .isButton : [])
    }

    private var icon: some View {
        Image(systemName: symbol)
            .font(.body)
            .foregroundStyle(Color.togoMute)
            .frame(width: iconWidth)
            .accessibilityHidden(true)
    }

    private var name: some View {
        Text(verbatim: item.name)
            .font(.body)
            .foregroundStyle(Color.togoSnow)
            .multilineTextAlignment(.leading)
            .lineLimit(typeSize.isAccessibilitySize ? nil : 2)
    }

    /// "M", "A", "U" or "!" beside a changed file, as editors mark them; a dot on a folder
    /// holding changes.
    @ViewBuilder private var changeMark: some View {
        if let mark {
            Text(verbatim: mark.letter)
                .font(.system(.footnote, design: .monospaced).weight(.bold))
                .foregroundStyle(Self.color(mark))
                .accessibilityHidden(true)
        } else if holdsChanges {
            Circle()
                .fill(Color.togoSnow)
                .frame(width: 6, height: 6)
                .accessibilityHidden(true)
        }
    }

    static func color(_ mark: ChangeMarks.Mark) -> Color {
        switch mark {
        // Foreground tokens: the diff's word colours are backgrounds and read at about 1.3:1.
        case .added, .untracked: .togoChangeAdded
        case .conflicted: .togoChangeConflict
        case .modified: .togoSnow
        }
    }

    @ViewBuilder private var detail: some View {
        if item.kind == .link {
            Text(FilesCopy.link).font(.footnote).foregroundStyle(Color.togoMute)
        } else if let size = item.size, item.kind == .file {
            Text(verbatim: FilesCopy.size(size)).font(.footnote).foregroundStyle(Color.togoMute)
        }
    }

    private var symbol: String {
        switch item.kind {
        case .folder: expanded == true ? "folder.fill" : "folder"
        case .file: "doc"
        case .link: "link"
        }
    }

    private var spoken: String {
        switch item.kind {
        case .folder: holdsChanges ? "\(item.name), \(FilesCopy.folder), \(FileTreeCopy.holdsChanges)" : "\(item.name), \(FilesCopy.folder)"
        case .link: "\(item.name), \(FilesCopy.link)"
        // A bare name such as "notes.txt" reads as a word; the kind says it is a file.
        case .file:
            [item.name, mark?.words, item.size.map(FilesCopy.size) ?? FilesCopy.file].compactMap(\.self).joined(separator: ", ")
        }
    }
}

#if DEBUG
extension FilesModel {
    static func preview(path: String = "", ref: String? = nil, services: FakeReviewServices = FakeReviewServices()) -> FilesModel {
        FilesModel(subject: DiffModel.previewSubject, services: services, path: path, ref: ref)
    }
}

#Preview("Root") {
    NavigationStack { FileBrowserView(model: .preview(), onOpen: { _ in }) }
}

#Preview("At a commit") {
    NavigationStack {
        FileBrowserView(model: .preview(ref: ReviewFixtures.log[2].hash), onOpen: { _ in })
    }
}
#endif
