import SwiftUI
import TogoKit

struct FolderStep: View {
    let model: NewSessionModel
    let onStart: () async -> Void
    let onChoose: (String) async -> Void

    var body: some View {
        if model.helperMissing {
            PlainStart(model: model, onStart: onStart)
        } else {
            StepScaffold(title: NewSessionCopy.folderTitle) {
                places
            }
            // Browsing is its own page so the navigation bar's back button returns to the places.
            .navigationDestination(isPresented: browsingShown) {
                StepScaffold(title: NewSessionCopy.browse) {
                    BrowseList(model: model, onChoose: onChoose)
                }
            }
        }
    }

    private var browsingShown: Binding<Bool> {
        Binding(
            get: { model.browsing != .idle },
            set: { shown in if !shown { model.leaveBrowsing() } }
        )
    }

    private var places: some View {
        VStack(alignment: .leading, spacing: 24) {
            VStack(alignment: .leading, spacing: 4) {
                SectionHeading(NewSessionCopy.recentHeading)
                recentRows
            }
            VStack(alignment: .leading, spacing: 4) {
                SectionHeading(NewSessionCopy.otherHeading)
                ChoiceRow(symbol: "house", title: Text(verbatim: NewSessionCopy.home), chevron: true) {
                    Task { await onChoose("~") }
                }
                ChoiceRow(symbol: "folder", title: Text(verbatim: NewSessionCopy.browse), chevron: true) {
                    Task { await model.browse("~") }
                }
            }
        }
    }

    @ViewBuilder private var recentRows: some View {
        switch model.recent {
        case .idle, .loading:
            PawWalk(NewSessionCopy.recentLoading).padding(.vertical, 12)
        case .failed(let message):
            VStack(alignment: .leading, spacing: 4) {
                InlineError(message: message)
                RetryButton { Task { await model.loadRecent() } }
            }
            .padding(.vertical, 12)
        case .loaded(let dirs) where dirs.isEmpty:
            Text(verbatim: NewSessionCopy.recentEmpty)
                .font(.body)
                .foregroundStyle(Color.togoMute)
                .padding(.vertical, 12)
        case .loaded(let dirs):
            ForEach(dirs, id: \.self) { dir in
                FolderRow(path: dir) { Task { await onChoose(dir) } }
            }
        }
    }
}

/// A folder as monospace text: the folder name in bold, the path above it in mute.
struct FolderRow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let path: String
    var chevron = true
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                label
                    .font(.system(.subheadline, design: .monospaced))
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 1)
                    .truncationMode(.middle)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if chevron {
                    Image(systemName: "chevron.right")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Color.togoMute)
                        .accessibilityHidden(true)
                }
            }
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .overlay(alignment: .bottom) { Color.togoLine.frame(height: 1) }
        .accessibilityLabel(Text(verbatim: PathText.abbreviate(path)))
    }

    private var label: Text {
        let shown = PathText.abbreviate(path)
        guard let slash = shown.lastIndex(of: "/"), shown != "/" else { return Text(verbatim: shown).bold() }
        let parent = String(shown[...slash])
        let last = String(shown[shown.index(after: slash)...])
        let muted = Text(verbatim: parent).foregroundStyle(Color.togoMute)
        let strong = Text(verbatim: last).bold().foregroundStyle(Color.togoSnow)
        return Text("\(muted)\(strong)")
    }
}

private struct BrowseList: View {
    let model: NewSessionModel
    let onChoose: (String) async -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            switch model.browsing {
            case .idle, .loading:
                PawWalk(NewSessionCopy.browseLoading).padding(.vertical, 12)
            case .failed(let message):
                InlineError(message: message).padding(.vertical, 8)
                RetryButton { Task { await model.retryBrowse() } }
            case .loaded(let listing):
                directory(listing)
            }
        }
    }

    @ViewBuilder private func directory(_ listing: ListDirResult) -> some View {
        let folders = listing.entries.filter { $0.dir && (model.showHidden || !$0.name.hasPrefix(".")) }
        VStack(alignment: .leading, spacing: 12) {
            Text(verbatim: PathText.abbreviate(listing.path))
                .font(.system(.footnote, design: .monospaced))
                .foregroundStyle(Color.togoMute)
                .lineLimit(2)
                .truncationMode(.middle)
            Button(NewSessionCopy.useFolder) { Task { await onChoose(listing.path) } }
                .buttonStyle(.primary)
            Toggle(NewSessionCopy.showHidden, isOn: Bindable(model).showHidden)
                .frame(minHeight: 44)
        }
        .padding(.vertical, 8)
        if listing.path != "/" {
            ChoiceRow(symbol: "arrow.up", title: Text(verbatim: NewSessionCopy.up)) {
                Task { await model.browse(Self.parent(of: listing.path)) }
            }
        }
        if folders.isEmpty {
            Text(verbatim: NewSessionCopy.browseEmpty).foregroundStyle(Color.togoMute).padding(.vertical, 12)
        }
        ForEach(folders, id: \.name) { entry in
            FolderRow(path: Self.child(listing.path, entry.name)) {
                Task { await model.browse(Self.child(listing.path, entry.name)) }
            }
        }
    }

    private static func child(_ path: String, _ name: String) -> String {
        path.hasSuffix("/") ? path + name : path + "/" + name
    }

    private static func parent(of path: String) -> String {
        let parent = (path as NSString).deletingLastPathComponent
        return parent.isEmpty ? "/" : parent
    }
}

/// The folder and agent steps for a machine without the helper: one line and Start.
private struct PlainStart: View {
    let model: NewSessionModel
    let onStart: () async -> Void

    var body: some View {
        StepScaffold(title: NewSessionCopy.plainTitle) {
            VStack(alignment: .leading, spacing: 20) {
                Text(verbatim: NewSessionCopy.plainBody)
                    .font(.body)
                    .foregroundStyle(Color.togoMute)
                if let error = model.createError { InlineError(message: error) }
                StartButton(title: NewSessionCopy.plainStart, creating: model.creating, onStart: onStart)
            }
        }
    }
}

/// The Start action with its working state; the label becomes "Starting" while the machine answers.
struct StartButton: View {
    let title: String
    let creating: Bool
    let onStart: () async -> Void

    var body: some View {
        Button {
            Task { await onStart() }
        } label: {
            Text(creating ? NewSessionCopy.starting : title)
        }
        .buttonStyle(.primary)
        .disabled(creating)
    }
}
