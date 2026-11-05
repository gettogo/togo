import SwiftUI
import TogoKit

/// Local branches and the log of one of them. Choosing a branch filters the log to it; choosing a
/// commit opens it.
struct HistoryView: View {
    @State private var model: HistoryModel
    private let onOpen: (ReviewRoute) -> Void

    @Environment(\.dynamicTypeSize) private var typeSize

    init(model: HistoryModel, onOpen: @escaping (ReviewRoute) -> Void) {
        _model = State(initialValue: model)
        self.onOpen = onOpen
    }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            .navigationTitle(HistoryCopy.title)
            .navigationBarTitleDisplayMode(.inline)
            .task { await model.loadIfNeeded() }
    }

    @ViewBuilder private var content: some View {
        switch model.state {
        case .loading:
            StateView(kind: .loading, title: HistoryCopy.loading, message: model.subject.title)
        case .failed(let message):
            StateView(kind: .error, title: HistoryCopy.failed, message: message, action: .init(HistoryCopy.tryAgain) {
                Task { await model.load() }
            })
        case .ready:
            list
        }
    }

    private var list: some View {
        List {
            if !model.branches.isEmpty {
                HeadedSection(HistoryCopy.branches) {
                    ForEach(model.branches, id: \.name) { branch in
                        BranchRow(
                            branch: branch,
                            chosen: model.selectedRef == branch.name
                        ) {
                            Task { await model.select(branch: branch.name) }
                        }
                    }
                    .groupedRows()
                }
            }
            HeadedSection(model.selectedRef.map(HistoryCopy.commitsOn) ?? HistoryCopy.commits) {
                if model.commits.isEmpty {
                    Text(HistoryCopy.emptyMessage)
                        .foregroundStyle(Color.togoMute)
                        .groupedRows()
                }
                ForEach(model.commits, id: \.hash) { commit in
                    Button { onOpen(model.route(for: commit)) } label: {
                        CommitRow(commit: commit, when: model.when(commit))
                    }
                    .buttonStyle(.plain)
                    .groupedRows()
                }
                footer
            }
        }
        .groupedList()
        .refreshable { await model.load() }
    }

    @ViewBuilder private var footer: some View {
        if model.canLoadMore {
            Button {
                Task { await model.loadMore() }
            } label: {
                HStack {
                    Text(model.loadingMore ? HistoryCopy.loadingMore : HistoryCopy.showMore)
                    if model.loadingMore { ProgressView() }
                }
                .frame(maxWidth: .infinity, minHeight: 44)
            }
            .buttonStyle(.secondary)
            .disabled(model.loadingMore)
            .groupedRows()
        } else if model.reachedLimit {
            Text(HistoryCopy.latest200)
                .font(.footnote)
                .foregroundStyle(Color.togoMute)
                .groupedRows()
        }
        if model.moreFailed {
            Text(HistoryCopy.moreFailed)
                .font(.footnote)
                .foregroundStyle(Color.togoMute)
                .groupedRows()
        }
    }
}

private struct BranchRow: View {
    let branch: GitBranches.Branch
    let chosen: Bool
    let choose: () -> Void

    var body: some View {
        Button(action: choose) {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: branch.displayName)
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                    Text(HistoryCopy.upstream(branch.upstream, current: branch.current))
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                }
                Spacer(minLength: 0)
                if chosen {
                    Image(systemName: "checkmark")
                        .font(.body.weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                        .accessibilityHidden(true)
                }
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(chosen ? .isSelected : [])
    }
}

private struct CommitRow: View {
    let commit: GitCommit
    let when: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: commit.displaySubject)
                .font(.body)
                .foregroundStyle(Color.togoSnow)
                .multilineTextAlignment(.leading)
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) { hash; Text(verbatim: commit.displayAuthor); Text(when) }
                VStack(alignment: .leading, spacing: 2) { hash; Text(verbatim: commit.displayAuthor); Text(when) }
            }
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
        }
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: "\(commit.displaySubject), \(commit.displayAuthor), \(when)"))
        .accessibilityAddTraits(.isButton)
    }

    private var hash: some View {
        Text(verbatim: commit.short)
            .font(.system(.footnote, design: .monospaced))
    }
}

/// One commit: who made it, the files it changed and a way into the tree as it was.
struct CommitDetailView: View {
    @State private var model: CommitDetailModel
    private let onOpen: (ReviewRoute) -> Void

    init(model: CommitDetailModel, onOpen: @escaping (ReviewRoute) -> Void) {
        _model = State(initialValue: model)
        self.onOpen = onOpen
    }

    var body: some View {
        List {
            Section {
                VStack(alignment: .leading, spacing: 6) {
                    Text(verbatim: model.commit.displaySubject)
                        .font(.title3.weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                    Text(verbatim: model.commit.hash)
                        .font(.system(.footnote, design: .monospaced))
                        .foregroundStyle(Color.togoMute)
                    Text(verbatim: "\(model.commit.displayAuthor), \(model.commit.date.formatted(date: .abbreviated, time: .shortened))")
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                }
                .padding(.vertical, 4)
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityElement(children: .combine)
                .groupedRows()
                Button { onOpen(model.browseRoute) } label: {
                    Label(HistoryCopy.browse, systemImage: "folder")
                        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .foregroundStyle(Color.togoSnow)
                .groupedRows()
            }
            filesSection
        }
        .groupedList()
        .navigationTitle(Text(verbatim: model.commit.short))
        .navigationBarTitleDisplayMode(.inline)
        .task { await model.loadIfNeeded() }
    }

    @ViewBuilder private var filesSection: some View {
        switch model.state {
        case .loading:
            HeadedSection(HistoryCopy.changedFiles) {
                HStack { ProgressView(); Text(HistoryCopy.loadingFiles) }
                    .foregroundStyle(Color.togoMute)
                    .groupedRows()
            }
        case .failed(let message):
            HeadedSection(HistoryCopy.changedFiles) {
                VStack(alignment: .leading, spacing: 8) {
                    Text(message).foregroundStyle(Color.togoMute)
                    Button(HistoryCopy.tryAgain) { Task { await model.load() } }
                        .buttonStyle(.secondary(.fit))
                }
                .groupedRows()
            }
        case .empty:
            HeadedSection(HistoryCopy.changedFiles) {
                Text(HistoryCopy.noChangesMessage).foregroundStyle(Color.togoMute).groupedRows()
            }
        case .ready:
            HeadedSection(HistoryCopy.changedFiles) {
                ForEach(model.files) { file in
                    Button { onOpen(model.route(for: file)) } label: { ChangedFileRow(file: file) }
                        .buttonStyle(.plain)
                        .reviewRow(opening: model.route(for: file))
                }
                if model.isCutOff {
                    Text(HistoryCopy.cutOff).font(.footnote).foregroundStyle(Color.togoMute).groupedRows()
                }
            }
        }
    }
}

private struct ChangedFileRow: View {
    let file: DiffFile

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: name)
                .font(.body)
                .foregroundStyle(Color.togoSnow)
            HStack(spacing: 8) {
                if !folder.isEmpty { PathText(folder) }
                Text(HistoryCopy.change(file.change))
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                if file.additions + file.deletions > 0 {
                    Text(verbatim: "+\(file.additions) -\(file.deletions)")
                        .font(.system(.footnote, design: .monospaced))
                        .foregroundStyle(Color.togoMute)
                        .accessibilityLabel(Text(verbatim: "\(HistoryCopy.added(file.additions)), \(HistoryCopy.removed(file.deletions))"))
                }
            }
        }
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isButton)
    }

    private var name: String { DisplayText.clean(file.id.split(separator: "/").last.map(String.init) ?? file.id) }
    private var folder: String {
        guard let slash = file.id.lastIndex(of: "/") else { return "" }
        return DisplayText.clean(String(file.id[..<slash]))
    }
}

/// A list section under the same heading the other review lists use: the system's grey section
/// title falls short of 4.5:1 on the app's ground.
private struct HeadedSection<Content: View>: View {
    let title: String
    let content: Content

    init(_ title: String, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }

    var body: some View {
        Section {
            content
        } header: {
            SectionHeading(title)
                .textCase(nil)
        }
    }
}

#if DEBUG
extension HistoryModel {
    static func preview(services: FakeReviewServices = FakeReviewServices()) -> HistoryModel {
        HistoryModel(subject: DiffModel.previewSubject, services: services, now: { ReviewFixtures.now.addingTimeInterval(7_200) })
    }
}

#Preview("History") {
    NavigationStack { HistoryView(model: .preview(), onOpen: { _ in }) }
}

#Preview("Commit") {
    NavigationStack {
        CommitDetailView(
            model: CommitDetailModel(
                subject: DiffModel.previewSubject, services: FakeReviewServices(), commit: ReviewFixtures.log[0]
            ),
            onOpen: { _ in }
        )
    }
}
#endif
