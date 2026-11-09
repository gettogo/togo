import SwiftUI
import TogoKit

/// A diff, one file at a time: in one column, or side by side when the window is wide enough.
/// Steps between hunks and, for a commit, between files; stages, unstages or discards the file
/// shown; and sends a comment on a line to the session.
struct DiffScreen: View {
    @State private var model: DiffModel
    private let onOpen: ((ReviewRoute) -> Void)?

    @AppStorage("review.diff.mode") private var storedMode: DiffMode = .split
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.dynamicTypeSize) private var typeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var width: CGFloat = 0
    @State private var draft: DiffModel.CommentDraft?
    /// The hunk bar as drawn: it comes and goes in a `resize` transaction, so the code below
    /// moves with it. The write banner is an outcome of the user's action: it lands at once,
    /// with the error haptic.
    @State private var readyShown = LiveMirror<Bool>()

    /// `onOpen` shows another review screen, such as an image the diff cannot draw.
    init(model: DiffModel, onOpen: ((ReviewRoute) -> Void)? = nil) {
        _model = State(initialValue: model)
        self.onOpen = onOpen
    }

    private var splitAvailable: Bool { DiffModel.splitAvailable(horizontalSizeClass: sizeClass, width: width) }
    private var showsSplit: Bool { splitAvailable && model.mode == .split }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .phaseSwap(model.state)
            .background(Color.togoInk)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
            .safeAreaInset(edge: .top, spacing: 0) { banner }
            .transaction(value: model.message) { $0.animation = nil }
            .navigationTitle(Text(verbatim: model.displayName))
            .navigationSubtitle(Text(verbatim: model.displayFolder))
            .navigationBarTitleDisplayMode(.inline)
            .modifier(FileMenu(model: model))
            .toolbar { toolbar }
            .coordinated("review.diffDiscard", .dialog, isPresented: Binding { model.pendingDiscard != nil } set: { if !$0 { model.cancelDiscard() } }) { view, shown in
                view.confirmationDialog(
                    model.pendingDiscard?.title ?? "",
                    isPresented: shown,
                    titleVisibility: .visible,
                    presenting: model.pendingDiscard
                ) { confirmation in
                    Button(confirmation.button, role: .destructive) {
                        Task { await model.confirm(confirmation) }
                    }
                } message: { confirmation in
                    Text(confirmation.message)
                }
            }
            .presentSheet("review.diffDraft", item: $draft) { draft in
                CommentSheet(draft: draft, actionTitle: model.commentActionTitle, canSend: model.canSendComments) {
                    model.comment(draft, text: $0)
                }
            }
            .task { await model.loadIfNeeded() }
            .onAppear { model.mode = storedMode }
            .onChange(of: model.mode) { storedMode = model.mode }
            .onChange(of: showsSplit, initial: true) { model.showsSplit = showsSplit }
            .mirrors(model.state == .ready, into: $readyShown)
            .haptic(.done, trigger: model.successes)
            .haptic(.discard, trigger: model.discards)
            .haptic(.error, trigger: model.message, when: { $0 != nil })
    }

    @ViewBuilder private var content: some View {
        switch model.state {
        case .loading:
            StateView(kind: .loading, title: DiffCopy.loading, message: model.displayPath)
        case .failed(let message):
            StateView(kind: .error, title: DiffCopy.failed, message: message, action: .init(DiffCopy.tryAgain) {
                Task { await model.load() }
            })
        case .empty, .discarded:
            StateView(kind: .empty, title: model.emptyTitle, message: model.emptyMessage)
        case .binary:
            BinaryNotice(name: model.displayName, open: model.openRoute.flatMap { route in
                onOpen.map { open in { open(route) } }
            })
        case .ready:
            CodeLinesView(
                rows: model.rows(split: showsSplit), wrap: model.wrap, showsOldNumbers: !showsSplit,
                scrollTo: model.scrollTarget, onLongPress: { pick in draft = model.commentDraft(row: pick.row, old: pick.old) },
                onTopRow: { model.scrolled(toTopRow: $0) }
            )
        }
    }

    // MARK: Banner

    @ViewBuilder private var banner: some View {
        if let message = model.message {
            WriteBanner(message: message) {
                Task { await model.retry() }
            } dismiss: {
                model.dismissMessage()
            }
        }
    }

    // MARK: Toolbar

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                if splitAvailable {
                    Picker(DiffCopy.layout, selection: $model.mode) {
                        Label(DiffCopy.unified, systemImage: "rectangle").tag(DiffMode.unified)
                        Label(DiffCopy.sideBySide, systemImage: "rectangle.split.2x1").tag(DiffMode.split)
                    }
                    .pickerStyle(.inline)
                }
                Toggle(DiffCopy.wrap, isOn: wrapBinding)
                    .disabled(typeSize.isAccessibilitySize)
                if model.actions.contains(.discard) {
                    Section {
                        Button(role: .destructive) {
                            Task { await model.perform(.discard) }
                        } label: {
                            Label(model.discardActionTitle, systemImage: "trash")
                        }
                        .disabled(model.busy)
                    }
                }
            } label: {
                Label(DiffCopy.options, systemImage: "slider.horizontal.3")
            }
        }
        if readyShown.read(model.state == .ready) {
            ToolbarItemGroup(placement: .bottomBar) {
                Button(DiffCopy.previousHunk, systemImage: "chevron.up") { model.previousHunk() }
                    .disabled(!model.canGoToPreviousHunk)
                Button(DiffCopy.nextHunk, systemImage: "chevron.down") { model.nextHunk() }
                    .disabled(!model.canGoToNextHunk)
            }
        }
        if let action = model.actions.first(where: { $0 != .discard }) {
            ToolbarSpacer(.flexible, placement: .bottomBar)
            ToolbarItem(placement: .bottomBar) {
                Button(action == .stage ? DiffCopy.stage : DiffCopy.unstage, systemImage: action == .stage ? "plus.circle" : "minus.circle") {
                    Task { await model.perform(action) }
                }
                .disabled(model.busy)
            }
        }
    }

    /// Code always wraps at accessibility sizes, and the toggle says so.
    private var wrapBinding: Binding<Bool> {
        Binding { model.wrap || typeSize.isAccessibilitySize } set: { model.wrap = $0 }
    }
}

/// The title becomes a menu of files when the diff has more than one.
private struct FileMenu: ViewModifier {
    let model: DiffModel

    func body(content: Content) -> some View {
        if model.files.count > 1 {
            content.toolbarTitleMenu {
                Picker(DiffCopy.files, selection: Binding { model.fileIndex } set: { model.selectFile($0) }) {
                    ForEach(Array(model.fileNames.enumerated()), id: \.offset) { index, name in
                        Text(verbatim: name).tag(index)
                    }
                }
            }
        } else {
            content
        }
    }
}

/// In place of a file the diff cannot draw as text.
private struct BinaryNotice: View {
    let name: String
    let open: (() -> Void)?

    var body: some View {
        VStack(spacing: 14) {
            VStack(spacing: 6) {
                Image(systemName: "doc")
                    .font(.system(.largeTitle).weight(.light))
                    .foregroundStyle(Color.togoMute)
                    .accessibilityHidden(true)
                Text(DiffCopy.binary)
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                Text(verbatim: name)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(Color.togoMute)
            }
            .multilineTextAlignment(.center)
            .accessibilityElement(children: .combine)
            if let open {
                Button(DiffCopy.openFile, action: open)
                    .buttonStyle(.primary(.fit))
                    .padding(.top, 8)
            }
        }
        .padding(.horizontal, Layout.margin)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// What the last write said, under the navigation bar.
private struct WriteBanner: View {
    let message: DiffModel.Message
    let retry: () -> Void
    let dismiss: () -> Void

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 12) { text; Spacer(minLength: 0); buttons }
            VStack(alignment: .leading, spacing: 4) { text; HStack { buttons } }
        }
        .padding(.leading, 16)
        .padding(.trailing, 8)
        .padding(.vertical, 8)
        .background(Color.togoRaised, in: .rect(cornerRadius: 14))
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    private var text: some View {
        Text(message.text)
            .font(.subheadline)
            .foregroundStyle(Color.togoSnow)
            .fixedSize(horizontal: false, vertical: true)
    }

    @ViewBuilder private var buttons: some View {
        if message.retry {
            Button(DiffCopy.tryAgain, action: retry)
                .buttonStyle(.secondary(.fit))
                .fontWeight(.semibold)
        }
        Button(DiffCopy.dismiss, systemImage: "xmark", action: dismiss)
            .labelStyle(.iconOnly)
            .buttonStyle(.secondary(.fit))
    }
}

#if DEBUG
extension DiffModel {
    /// A session Togo started, on a machine whose helper can write.
    static let previewSubject = ReviewSubject(
        hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
        managed: true, capabilities: [Capability.git, Capability.gitWrite]
    )

    static func preview(_ source: DiffSource, services: FakeReviewServices = FakeReviewServices()) -> DiffModel {
        DiffModel(subject: previewSubject, services: services, source: source, sendToSession: { _ in })
    }
}

#Preview("Worktree") {
    NavigationStack {
        DiffScreen(model: .preview(.worktree(path: "src/handlers/items.ts")))
    }
}

#Preview("Commit") {
    NavigationStack {
        DiffScreen(model: .preview(.commit(hash: ReviewFixtures.log[0].hash, path: nil)))
    }
}
#endif
