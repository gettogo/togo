import SwiftUI
import TogoKit

/// One folder of the working tree, or of a commit: folders first, then files. Hidden files are
/// behind a toggle, and a banner says when the folder is as a commit had it.
struct FileBrowserView: View {
    @State private var model: FilesModel
    private let onOpen: (ReviewRoute) -> Void

    init(model: FilesModel, onOpen: @escaping (ReviewRoute) -> Void) {
        _model = State(initialValue: model)
        self.onOpen = onOpen
    }

    var body: some View {
        content
            .phaseSwap(model.items.isEmpty ? "\(CaseName.of(model.state)) empty" : CaseName.of(model.state))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            .safeAreaInset(edge: .top, spacing: 0) { banner }
            .navigationTitle(Text(verbatim: model.title))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Toggle(FilesCopy.hidden, isOn: $model.showsHidden)
                    } label: {
                        Label(FilesCopy.options, systemImage: "slider.horizontal.3")
                    }
                }
            }
            .task { await model.loadIfNeeded() }
    }

    @ViewBuilder private var content: some View {
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
                list
            }
        }
    }

    private var list: some View {
        List {
            Section {
                ForEach(model.items) { item in
                    if let route = item.route {
                        Button { onOpen(route) } label: { ItemRow(item: item) }
                            .buttonStyle(.plain)
                            .reviewRow(opening: route)
                    } else {
                        ItemRow(item: item)
                            .groupedRows()
                    }
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

    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .body) private var iconWidth: CGFloat = 24

    var body: some View {
        Group {
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(alignment: .firstTextBaseline, spacing: 12) {
                        icon
                        name
                    }
                    detail.padding(.leading, iconWidth + 12)
                }
            } else {
                HStack(spacing: 12) {
                    icon
                    name
                    Spacer(minLength: 8)
                    detail
                    if item.kind == .folder { chevron }
                }
            }
        }
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: spoken))
        .accessibilityHint(item.kind == .link ? FilesCopy.linkHint : "")
        .accessibilityAddTraits(item.route == nil ? [] : .isButton)
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

    private var chevron: some View {
        Image(systemName: "chevron.right")
            .font(.footnote.weight(.semibold))
            .foregroundStyle(Color.togoMute)
            .accessibilityHidden(true)
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
        case .folder: "folder"
        case .file: "doc"
        case .link: "link"
        }
    }

    private var spoken: String {
        switch item.kind {
        case .folder: "\(item.name), \(FilesCopy.folder)"
        case .link: "\(item.name), \(FilesCopy.link)"
        // A bare name such as "notes.txt" reads as a word; the kind says it is a file.
        case .file: "\(item.name), \(item.size.map(FilesCopy.size) ?? FilesCopy.file)"
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
