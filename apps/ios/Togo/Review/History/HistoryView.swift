import SwiftUI
import TogoKit
import UIKit

/// The commit graph of one branch, the current one unless the branch menu picks another. A
/// commit opens on its diff; its menu lists its files or copies its hash.
struct HistoryView: View {
    @State private var model: HistoryModel
    private let onOpen: (ReviewRoute) -> Void

    init(model: HistoryModel, onOpen: @escaping (ReviewRoute) -> Void) {
        _model = State(initialValue: model)
        self.onOpen = onOpen
    }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .phaseSwap(model.state)
            .background(Color.togoInk)
            // The title stays the session's, as on Changes and Files; the picker says History.
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
                Section {
                    BranchPicker(model: model)
                        .groupedRows()
                }
            }
            Section {
                if model.commits.isEmpty {
                    Text(HistoryCopy.emptyMessage)
                        .foregroundStyle(Color.togoMute)
                        .groupedRows()
                }
                let width = model.graphWidth
                ForEach(Array(model.commits.enumerated()), id: \.element.hash) { index, commit in
                    Button { onOpen(model.route(for: commit)) } label: {
                        CommitRow(
                            commit: commit, graph: model.graph.indices.contains(index) ? model.graph[index] : nil,
                            graphWidth: width, when: model.when(commit)
                        )
                    }
                    .buttonStyle(.plain)
                    .reviewRow(opening: model.route(for: commit))
                    // The lanes run through the rows unbroken, so rows sit edge to edge.
                    .listRowInsets(EdgeInsets(top: 0, leading: 12, bottom: 0, trailing: 16))
                    .listRowSeparator(.hidden)
                    .contextMenu {
                        Button(HistoryCopy.showFiles, systemImage: "list.bullet") { onOpen(model.filesRoute(for: commit)) }
                        Button(HistoryCopy.browse, systemImage: "folder") {
                            onOpen(.folder(path: "", ref: commit.hash))
                        }
                        Button(HistoryCopy.copyHash, systemImage: "doc.on.doc") { UIPasteboard.general.string = commit.hash }
                    }
                    .accessibilityAction(named: HistoryCopy.showFiles) { onOpen(model.filesRoute(for: commit)) }
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
                        .contentTransition(.interpolate)
                    // motion: cause → effect — the tapped Show more now shows it loading
                    if model.loadingMore { ProgressView().transition(.opacity) }
                }
                .frame(maxWidth: .infinity, minHeight: 44)
                // motion: cause → effect — the button's words and spinner change in place on the tap
                .motion(.respond, value: model.loadingMore)
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

/// "Branch  main" as a menu row: the current branch first and marked, then the others.
private struct BranchPicker: View {
    let model: HistoryModel

    var body: some View {
        let selection = Binding<String> {
            model.shownBranch ?? ""
        } set: { branch in
            Task { await model.select(branch: branch) }
        }
        Picker(HistoryCopy.branch, selection: selection) {
            if model.currentBranch == nil {
                Text(HistoryCopy.detached).tag("")
            }
            ForEach(model.branches, id: \.name) { branch in
                Text(verbatim: branch.name == model.currentBranch ? HistoryCopy.current(branch.displayName) : branch.displayName)
                    .tag(branch.name)
            }
        }
        .pickerStyle(.menu)
        .foregroundStyle(Color.togoSnow)
        .frame(minHeight: 44)
    }
}

private struct CommitRow: View {
    let commit: GitCommit
    let graph: HistoryGraph.Row?
    let graphWidth: Int
    let when: String

    @Environment(\.dynamicTypeSize) private var typeSize
    /// Room per lane, and how far down the dot sits: level with the subject's first line.
    @ScaledMetric(relativeTo: .body) private var laneWidth: CGFloat = 14
    @ScaledMetric(relativeTo: .body) private var dotY: CGFloat = 23

    /// Past this many lanes the graph is cut at its edge, so the words keep their room.
    private static let maxLanes = 6

    private var showsGraph: Bool { graph != nil && !typeSize.isAccessibilitySize }
    private var graphColumn: CGFloat { laneWidth * CGFloat(min(graphWidth, Self.maxLanes)) }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: commit.displaySubject)
                .font(.body)
                .foregroundStyle(Color.togoSnow)
                .multilineTextAlignment(.leading)
                // trunc-ok: a commit subject, two lines in the list; the diff it opens is titled with it in full
                .lineLimit(typeSize.isAccessibilitySize ? nil : 2)
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) { meta }
                VStack(alignment: .leading, spacing: 2) { meta }
            }
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
        }
        .padding(.vertical, 10)
        .padding(.leading, showsGraph ? graphColumn + 8 : 4)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        // The graph fills the row's whole height, so its lanes meet the rows above and below.
        .background(alignment: .leading) {
            if showsGraph, let graph {
                LaneGraph(row: graph, laneWidth: laneWidth, dotY: dotY)
                    .frame(width: graphColumn)
                    .clipped()
                    .accessibilityHidden(true)
            }
        }
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: spoken))
        .accessibilityAddTraits(.isButton)
    }

    @ViewBuilder private var meta: some View {
        Text(verbatim: commit.short)
            .font(.system(.footnote, design: .monospaced))
        Text(verbatim: commit.displayAuthor)
        Text(when)
        if let files = commit.files {
            Text(HistoryCopy.filesChanged(files))
        } else if graph?.merge == true {
            Text(HistoryCopy.merge)
        }
    }

    private var spoken: String {
        var words = [commit.displaySubject, commit.displayAuthor, when]
        if let files = commit.files { words.append(HistoryCopy.filesChanged(files)) }
        if graph?.merge == true { words.append(HistoryCopy.merge) }
        return words.joined(separator: ", ")
    }
}

/// One row of the commit graph: the lanes passing through, those meeting at this commit, and
/// its dot, hollow for a merge. Each lane keeps its color down the list.
private struct LaneGraph: View {
    let row: HistoryGraph.Row
    let laneWidth: CGFloat
    let dotY: CGFloat

    static let colors: [Color] = [
        .togoSyntaxType, .togoSyntaxString, .togoSyntaxKeyword, .togoSyntaxNumber, .togoSyntaxAttribute,
    ]

    var body: some View {
        Canvas { context, size in
            func x(_ lane: Int) -> CGFloat { laneWidth * (CGFloat(lane) + 0.5) }
            func line(_ edge: HistoryGraph.Edge, from y0: CGFloat, to y1: CGFloat) {
                var path = Path()
                path.move(to: CGPoint(x: x(edge.from), y: y0))
                if edge.from == edge.to {
                    path.addLine(to: CGPoint(x: x(edge.to), y: y1))
                } else {
                    let mid = (y0 + y1) / 2
                    path.addCurve(
                        to: CGPoint(x: x(edge.to), y: y1),
                        control1: CGPoint(x: x(edge.from), y: mid),
                        control2: CGPoint(x: x(edge.to), y: mid)
                    )
                }
                context.stroke(path, with: .color(Self.color(edge.lane)), style: StrokeStyle(lineWidth: 2, lineCap: .round))
            }
            for edge in row.top { line(edge, from: 0, to: dotY) }
            for edge in row.bottom { line(edge, from: dotY, to: size.height) }
            let radius: CGFloat = 5
            let dot = CGRect(x: x(row.lane) - radius, y: dotY - radius, width: radius * 2, height: radius * 2)
            let color = Self.color(row.lane)
            if row.merge {
                context.fill(Path(ellipseIn: dot), with: .color(.togoGrouped))
                context.stroke(Path(ellipseIn: dot.insetBy(dx: 1, dy: 1)), with: .color(color), lineWidth: 2)
            } else {
                context.fill(Path(ellipseIn: dot), with: .color(color))
            }
        }
    }

    static func color(_ lane: Int) -> Color { colors[lane % colors.count] }
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
