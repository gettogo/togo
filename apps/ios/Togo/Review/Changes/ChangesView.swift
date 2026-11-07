import SwiftUI
import TogoKit

/// A session's uncommitted changes: what is staged, what is not, new files and conflicts, with
/// stage, unstage and discard on each row and a commit sheet below.
struct ChangesView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Bindable var model: ChangesModel
    let onOpen: (ReviewRoute) -> Void

    init(model: ChangesModel, onOpen: @escaping (ReviewRoute) -> Void) {
        self.model = model
        self.onOpen = onOpen
    }

    var body: some View {
        content
            .background(Color.togoInk)
            .task { await model.refresh() }
            .confirmationDialog(
                model.pendingConfirmation?.title ?? "", isPresented: confirming, titleVisibility: .visible,
                presenting: model.pendingConfirmation
            ) { confirmation in
                Button(confirmation.button, role: .destructive) {
                    Task { await model.confirm(confirmation) }
                }
                Button(ChangesCopy.cancel, role: .cancel) {}
            } message: { confirmation in
                Text(verbatim: confirmation.message)
            }
            .sheet(isPresented: $model.isCommitSheetOpen) {
                CommitSheet(model: model)
                    .togoSheet(.form)
            }
            .haptic(.done, trigger: model.successes)
            .haptic(.discard, trigger: model.discards)
    }

    private var confirming: Binding<Bool> {
        Binding(
            get: { model.pendingConfirmation != nil },
            set: { if !$0 { model.pendingConfirmation = nil } }
        )
    }

    private var content: some View {
        phaseContent.phaseSwap(model.phase)
    }

    @ViewBuilder private var phaseContent: some View {
        switch model.phase {
        case .loading:
            StateView(kind: .loading, title: ChangesCopy.loading, message: model.subject.title)
        case .failed(let message):
            StateView(
                kind: .error, title: ChangesCopy.loadFailed, message: message,
                action: .init(ReviewCopy.tryAgain) { Task { await model.refresh() } }
            )
        case .notRepo:
            StateView(kind: .empty, title: ChangesCopy.notRepo, message: ChangesCopy.notRepoMessage)
        case .loaded:
            // Rows arriving or leaving after a refresh or a write glide on `resize`; a row whose
            // state changes in place does not move (motion spec §2.5).
            IdentityKeyed(ChangesShown(model), key: ChangesShown.shape) { shown in
                list(shown)
            }
        }
    }

    private func list(_ shown: ChangesShown) -> some View {
        List {
            Section {
                ChangesHeader(model: model)
                    .groupedRows()
                notes(shown)
            }
            if shown.sections.isEmpty {
                Section {
                    StateView(kind: .empty, title: ChangesCopy.noChanges, message: ChangesCopy.noChangesMessage)
                        .padding(.vertical, 24)
                        .listRowBackground(Color.clear)
                }
            }
            ForEach(shown.sections) { section in
                Section {
                    ForEach(section.rows) { row in
                        ChangeRowView(row: row, canWrite: model.canWrite, open: onOpen, act: act)
                            .reviewRow(opening: row.route)
                    }
                } header: {
                    sectionHeader(section)
                }
            }
        }
        .groupedList()
        .refreshable { await model.refresh() }
        .safeAreaInset(edge: .bottom) {
            if shown.canWrite {
                Button(ChangesCopy.commit) { model.openCommitSheet() }
                    .buttonStyle(.primary)
                    .padding(.horizontal, Layout.margin)
                    .padding(.vertical, 12)
                    .background(Color.togoInk)
                    .transition(Motion.present(from: .bottom, reduce: reduceMotion))
            }
        }
    }

    /// The read-only reason, a cut-short list and the last refusal, each in a sentence.
    @ViewBuilder private func notes(_ shown: ChangesShown) -> some View {
        if let note = shown.readOnlyNote {
            NoteRow(symbol: "lock", message: note)
                .groupedRows()
        }
        if let note = shown.truncationNote {
            NoteRow(symbol: "list.bullet", message: note)
                .groupedRows()
        }
        if let notice = shown.notice {
            NoticeRow(
                notice: notice,
                retry: { Task { await model.retry() } },
                dismiss: { model.dismissNotice() }
            )
            .groupedRows()
        }
    }

    private func sectionHeader(_ section: ChangeSection) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            SectionHeading(section.title, detail: section.rows.count == 1 ? nil : String(section.rows.count))
            if model.canActOnAll, section.kind != .conflicted {
                Menu {
                    if section.kind == .staged {
                        Button(ChangesCopy.unstageAll, systemImage: "minus.circle") {
                            Task { await model.stageAll(.staged) }
                        }
                    } else {
                        Button(ChangesCopy.stageAll, systemImage: "plus.circle") {
                            Task { await model.stageAll(section.kind) }
                        }
                    }
                } label: {
                    Image(systemName: "checklist")
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(.rect)
                }
                .accessibilityLabel(ChangesCopy.sectionActions(section.title))
            }
        }
        .textCase(nil)
    }

    private func act(_ action: ChangeRowView.Action, _ row: ChangeRow) {
        Task {
            switch action {
            case .stage: await model.stage(row.paths)
            case .unstage: await model.unstage(row.paths)
            case .discard: model.requestDiscard(row.paths)
            }
        }
    }
}

/// What the changes list draws, mirrored so rows, notes and the commit bar arriving or leaving
/// move in one `resize` transaction while a row's own state updates in place.
struct ChangesShown: Equatable {
    var sections: [ChangeSection]
    var readOnlyNote: String?
    var truncationNote: String?
    var notice: ChangesModel.Notice?
    var canWrite: Bool

    @MainActor init(_ model: ChangesModel) {
        sections = model.sections
        readOnlyNote = model.readOnlyNote
        truncationNote = model.truncationNote
        notice = model.notice
        canWrite = model.canWrite
    }

    static func shape(_ shown: ChangesShown) -> [String] {
        var out = shown.sections.map { "\($0.id):" + $0.rows.map { "\($0.id)" }.joined(separator: ",") }
        out.append("notes \(shown.readOnlyNote != nil) \(shown.truncationNote != nil) \(shown.notice != nil)")
        out.append("write \(shown.canWrite)")
        return out
    }
}

/// Branch, how it stands against its upstream, and how much has changed.
private struct ChangesHeader: View {
    let model: ChangesModel

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            if let branch = model.branch {
                Label {
                    Text(verbatim: branch)
                        .font(.system(.body, design: .monospaced).weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                } icon: {
                    Image(systemName: "arrow.triangle.branch")
                        .foregroundStyle(Color.togoMute)
                }
            }
            if let line = model.branchLine {
                Text(line)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
            if !model.isEmpty {
                Text(model.summary)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
        }
        .padding(.vertical, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

private struct NoteRow: View {
    let symbol: String
    let message: String

    var body: some View {
        Label {
            Text(message)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
        } icon: {
            Image(systemName: symbol)
                .foregroundStyle(Color.togoMute)
        }
        .padding(.vertical, 2)
    }
}

/// The last refusal, with "Try again" when waiting is all it takes.
private struct NoticeRow: View {
    let notice: ChangesModel.Notice
    let retry: () -> Void
    let dismiss: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Label {
                    Text(notice.message)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoSnow)
                        .frame(maxWidth: .infinity, alignment: .leading)
                } icon: {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundStyle(Color.togoMute)
                }
                Button(action: dismiss) {
                    Image(systemName: "xmark")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Color.togoMute)
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(ChangesCopy.dismiss)
            }
            if notice.canRetry {
                Button(ReviewCopy.tryAgain, action: retry)
                    .buttonStyle(.primary(.fit))
                    .padding(.vertical, 4)
            }
        }
    }
}

/// A file: its name, its folder, and the change in words. Swipe or press for the actions.
struct ChangeRowView: View {
    enum Action { case stage, unstage, discard }

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let row: ChangeRow
    let canWrite: Bool
    let open: (ReviewRoute) -> Void
    let act: (Action, ChangeRow) -> Void

    private var actions: [Action] {
        guard canWrite else { return [] }
        switch row.section {
        case .staged: return [.unstage, .discard]
        case .changed, .untracked: return [.stage, .discard]
        case .conflicted: return [.stage]
        }
    }

    var body: some View {
        Button { open(row.route) } label: {
            HStack(spacing: 12) {
                if !dynamicTypeSize.isAccessibilitySize {
                    Image(systemName: symbol)
                        .font(.body)
                        .foregroundStyle(Color.togoMute)
                        .frame(width: 24)
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: row.displayName)
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                        // trunc-ok: a file name, middle-cut keeping its extension, three lines at accessibility sizes; tapping opens the diff titled with the full path
                        .lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 1)
                        .truncationMode(.middle)
                    if !row.folder.isEmpty {
                        PathText(row.displayFolder)
                    }
                    Text(row.words)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                }
                Spacer(minLength: 8)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoMuteDim)
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(row.accessibilityLabel)
        .accessibilityAddTraits(.isButton)
        .accessibilityActions {
            ForEach(actions, id: \.self) { action in
                Button(title(action)) { act(action, row) }
            }
        }
        .swipeActions(edge: .leading, allowsFullSwipe: true) {
            ForEach(actions.filter { $0 != .discard }, id: \.self) { action in
                Button(title(action), systemImage: action == .stage ? "plus" : "minus") { act(action, row) }
                    .tint(Color(uiColor: .systemGray))
            }
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if actions.contains(.discard) {
                Button(title(.discard), systemImage: "trash") { act(.discard, row) }
                    .tint(Color(uiColor: .systemRed))
            }
        }
        .contextMenu {
            ForEach(actions, id: \.self) { action in
                Button(
                    title(action), systemImage: icon(action), role: action == .discard ? .destructive : nil
                ) { act(action, row) }
            }
        }
    }

    private func title(_ action: Action) -> String {
        switch action {
        case .stage: ChangesCopy.stage
        case .unstage: ChangesCopy.unstage
        case .discard: row.section == .untracked ? ChangesCopy.delete : ChangesCopy.discard
        }
    }

    private func icon(_ action: Action) -> String {
        switch action {
        case .stage: "plus.circle"
        case .unstage: "minus.circle"
        case .discard: "trash"
        }
    }

    private var symbol: String {
        switch row.change {
        case .modified, .typeChanged: "pencil"
        case .added: "plus"
        case .deleted: "minus"
        case .renamed, .copied: "arrow.right"
        case .untracked: "doc.badge.plus"
        case .untrackedFolder: "folder.badge.plus"
        case .conflict: "exclamationmark.triangle"
        }
    }
}
