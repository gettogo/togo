import SwiftUI
import TogoKit

/// Where the review screens are and what is open in them. On a phone every route is pushed. In
/// a split view, folders and commits go down the sidebar and a diff or file replaces the detail.
/// A link tapped in the chat opens one diff or file on its own (`focus`): no lists behind it, so
/// Close goes back to the chat, and "All changes" turns it into the whole review.
struct ReviewNavigation: Equatable {
    var tab: ReviewTab
    var path: [ReviewRoute] = []
    var detail: ReviewRoute?
    /// The one screen the review was opened for. While set, `path` is pushed on top of it.
    var focus: ReviewRoute?

    mutating func open(_ route: ReviewRoute, layout: ReviewRoot.Layout) {
        if focus != nil {
            path.append(route)
            return
        }
        // "Back to working tree" from a folder as a commit had it: the Files tab, from its root.
        if case .folder(let folder, nil, .session) = route, folder.isEmpty {
            tab = .files
            path = []
            return
        }
        switch (layout, route) {
        case (.stack, _), (.split, .folder), (.split, .commit):
            path.append(route)
        case (.split, .diff), (.split, .file):
            detail = route
        }
    }

    /// After the window changes between compact and regular, as when a Duo folds or unfolds: a
    /// diff or file open beside the list is pushed onto the stack, and one pushed last moves
    /// beside the list. Running it again changes nothing.
    mutating func relayout(to layout: ReviewRoot.Layout) {
        // A focused screen is one stack on every width.
        guard focus == nil else { return }
        switch layout {
        case .stack:
            guard let detail else { return }
            path.append(detail)
            self.detail = nil
        case .split:
            guard detail == nil, let last = path.last, last.opensBeside else { return }
            path.removeLast()
            detail = last
        }
    }

    /// Another tab starts from its list, with nothing from the last tab open beside it.
    mutating func select(_ tab: ReviewTab) {
        guard tab != self.tab || focus != nil else { return }
        self.tab = tab
        path = []
        detail = nil
        focus = nil
    }

    /// From a focused diff or file to the whole review on Changes. Where the list has a detail
    /// column beside it, what was focused stays open there.
    mutating func showAll(layout: ReviewRoot.Layout) {
        let was = focus
        tab = .changes
        path = []
        detail = nil
        focus = nil
        if let was {
            switch layout {
            case .split: detail = was
            case .stack: path = [was]
            }
        }
    }
}

/// The review sheet for one session: Changes, History, Files and Ports. On a phone it is one
/// navigation stack under a tab picker; in a regular-width window the tab's list sits in a
/// sidebar beside the diff or file it opened.
struct ReviewRoot: View {
    enum Layout: Equatable {
        case stack, split
    }

    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.dismiss) private var dismiss
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    let subject: ReviewSubject
    private let services: any ReviewServices
    private let initialTab: ReviewTab
    private let machine: String
    private let sendToSession: ((String) -> Bool)?
    private let onSelectTab: (ReviewTab) -> Void
    private let onWrite: () -> Void
    /// Why nothing can be changed any more, once the session or machine is gone.
    private let ended: String?
    private let onDraft: (Bool) -> Void

    @State private var session: ReviewSession
    /// The review's own width (the sheet or the cover), measured outside its layout.
    @State private var measuredWidth: CGFloat?
    /// A find field is open in a file, so Escape is its to close first.
    @State private var findOpen = false

    /// `navigation` restores screens already open, for previews and snapshots. `session` is the
    /// store's open review, which outlives this view when a fold moves the review.
    init(
        subject: ReviewSubject, services: any ReviewServices, initialTab: ReviewTab, machine: String = "",
        sendToSession: ((String) -> Bool)?, navigation: ReviewNavigation? = nil, session: ReviewSession? = nil,
        onSelectTab: @escaping (ReviewTab) -> Void = { _ in }, onWrite: @escaping () -> Void = {},
        ended: String? = nil, onDraft: @escaping (Bool) -> Void = { _ in }
    ) {
        self.onSelectTab = onSelectTab
        self.onWrite = onWrite
        self.ended = ended
        self.onDraft = onDraft
        self.subject = subject
        self.services = services
        self.initialTab = initialTab
        self.machine = machine
        self.sendToSession = sendToSession
        _session = State(initialValue: session ?? ReviewSession(
            subject: subject, services: services, tab: initialTab, machine: machine, navigation: navigation
        ))
    }

    private var navigation: ReviewNavigation {
        get { session.navigation }
        nonmutating set { session.navigation = newValue }
    }

    private var changes: ChangesModel { session.changes }

    /// Ports shows only when the machine's helper lists ports.
    static func tabs(for subject: ReviewSubject) -> [ReviewTab] {
        ReviewTab.allCases.filter { $0 != .ports || subject.canPorts }
    }

    /// The split view needs regular width by the policy (size class and ≥ 600 pt) and room
    /// for its sidebar beside the diff (≥ 640 pt); otherwise the phone's stack.
    static func layout(for sizeClass: UserInterfaceSizeClass?, width: CGFloat?) -> Layout {
        guard AdaptivePresentation.width(sizeClass: sizeClass, containerWidth: width) == .regular else { return .stack }
        if let width, width > 0, width < AdaptivePresentation.splitMinimumWidth { return .stack }
        return .split
    }

    /// A comment from the diff goes into the session's input as a paste, cleaned for a terminal.
    /// Enter is never pressed: the user reads it there and sends it.
    /// The handle is looked up again on each send, since a reconnect replaces it. A send
    /// answers false when the terminal has gone since (the tab closed, or was let go past the
    /// pool's limit), so the comment is not lost without a word.
    static func sendToSession(_ handle: @escaping @MainActor () -> TerminalHandle?) -> ((String) -> Bool)? {
        guard handle() != nil else { return nil }
        return { text in
            let clean = SessionText.clean(text)
            guard !clean.isEmpty else { return true }
            guard let terminal = handle() else { return false }
            terminal.paste(clean)
            return true
        }
    }

    /// Follows the live width even while the presentation is kept: a fold inside an open
    /// review turns its split into the stack.
    private var layout: Layout { Self.layout(for: sizeClass, width: measuredWidth) }

    var body: some View {
        Group {
            if let focus = navigation.focus {
                focused(focus)
            } else {
                switch layout {
                case .stack: stack
                case .split: split
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        // measured-outside: the frame fills the presentation whichever layout is inside.
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { measuredWidth = $0 }
        .background(Color.togoInk)
        .background { escape }
        .togoControls()
        .onChange(of: initialTab) { _, tab in
            if Self.tabs(for: subject).contains(tab) { navigation.select(tab) }
        }
        // The store follows the picker, so a shortcut for the tab left behind opens it again.
        .onChange(of: navigation.tab) { _, tab in onSelectTab(tab) }
        // Folding or unfolding moves an open diff between the stack and the detail.
        .onChange(of: layout, initial: true) { _, layout in navigation.relayout(to: layout) }
        .onChange(of: changes.successes) { onWrite() }
        .onChange(of: changes.status?.files.count) { onWrite() }
        .onChange(of: ended, initial: true) { _, why in changes.ended = why }
        .onChange(of: changes.hasDraft, initial: true) { _, draft in onDraft(draft) }
        .environment(\.reviewEnded, ended)
        .environment(\.reviewFindOpen, $findOpen)
    }

    // MARK: Layouts

    /// One diff or file, opened from a link in the chat. Close goes back to the chat; "All
    /// changes" opens the rest of the review with this screen still open.
    private func focused(_ route: ReviewRoute) -> some View {
        NavigationStack(path: Bindable(session).navigation.path) {
            destination(route)
                .id(route)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button(ReviewEntryCopy.close, systemImage: "xmark", role: .close) { dismiss() }
                    }
                    if subject.canRead {
                        ToolbarItem(placement: .topBarLeading) {
                            Button(ReviewEntryCopy.allChanges, systemImage: "plus.forwardslash.minus") {
                                // motion: cause → effect — the lone screen opens out into the review around it
                                withAnimation(Motion.animation(.respond, reduce: reduceMotion)) {
                                    navigation.showAll(layout: layout)
                                }
                            }
                            .accessibilityHint(ReviewEntryCopy.allChangesHint)
                        }
                    }
                }
                .navigationDestination(for: ReviewRoute.self) { destination($0) }
        }
    }

    private var stack: some View {
        NavigationStack(path: Bindable(session).navigation.path) {
            tabRoot
        }
    }

    private var split: some View {
        NavigationSplitView {
            NavigationStack(path: Bindable(session).navigation.path) {
                tabRoot
            }
            .environment(\.openReviewRoute, navigation.detail)
            .navigationSplitViewColumnWidth(min: 320, ideal: 380, max: 480)
        } detail: {
            NavigationStack {
                if let route = navigation.detail {
                    destination(route)
                        .id(route)
                } else {
                    StateView(kind: .empty, title: ReviewEntryCopy.nothingOpen, message: ReviewEntryCopy.nothingOpenMessage)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .background(Color.togoInk)
                }
            }
        }
        .navigationSplitViewStyle(.balanced)
    }

    /// The chosen tab's screen under the picker, with the routes it can push.
    private var tabRoot: some View {
        tabContent
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .safeAreaInset(edge: .top, spacing: 0) { picker }
            .navigationTitle(Text(verbatim: title))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(ReviewEntryCopy.close, systemImage: "xmark", role: .close) { dismiss() }
                }
            }
            .navigationDestination(for: ReviewRoute.self) { destination($0) }
    }

    private var title: String { subject.displayTitle }

    @ViewBuilder private var tabContent: some View {
        switch navigation.tab {
        case .changes: ChangesView(model: changes, onOpen: open)
        case .history: HistoryView(model: session.history, onOpen: open)
        case .files: FileBrowserView(model: session.files, onOpen: open)
        case .ports: PortsView(model: session.ports)
        }
    }

    /// Segments while the names fit; at accessibility sizes a menu that names the tab shown.
    @ViewBuilder private var picker: some View {
        let selection = Binding { navigation.tab } set: { navigation.select($0) }
        let tabs = Self.tabs(for: subject)
        Group {
            if dynamicTypeSize.isAccessibilitySize {
                Picker(ReviewEntryCopy.sections, selection: selection) {
                    ForEach(tabs, id: \.self) { Text(ReviewCopy.tabName($0)).tag($0) }
                }
                .pickerStyle(.menu)
                .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
            } else {
                Picker(ReviewEntryCopy.sections, selection: selection) {
                    ForEach(tabs, id: \.self) { Text(ReviewCopy.tabName($0)).tag($0) }
                }
                .pickerStyle(.segmented)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .background(Color.togoInk)
    }

    // MARK: Routes

    private func open(_ route: ReviewRoute) {
        navigation.open(route, layout: layout)
    }

    @ViewBuilder private func destination(_ route: ReviewRoute) -> some View {
        switch route {
        case .diff(let source):
            DiffDestination(
                model: session.screen(for: route) {
                    DiffModel(subject: subject, services: services, source: source, sendToSession: sendToSession, machine: machine)
                },
                onOpen: open,
                onWrite: {
                    onWrite()
                    Task { await changes.refresh() }
                }
            )
        case .file(let path, let ref, let base):
            FileViewer(model: session.screen(for: route) {
                FileViewerModel(subject: subject, services: services, path: path, ref: ref, base: base)
            })
        case .folder(let path, let ref, let base):
            FileBrowserView(
                model: session.screen(for: route) {
                    FilesModel(subject: subject, services: services, path: path, ref: ref, base: base)
                },
                onOpen: open
            )
        case .commit(let commit):
            CommitDetailView(
                model: session.screen(for: route) { CommitDetailModel(subject: subject, services: services, commit: commit) },
                onOpen: open
            )
        }
    }

    /// Escape closes the review from any screen in it, once a find field in a file is closed.
    private var escape: some View {
        Button(ReviewEntryCopy.close) { dismiss() }
            .keyboardShortcut(.cancelAction)
            .disabled(findOpen)
            .opacity(0)
            .accessibilityHidden(true)
    }
}

private extension ReviewRoute {
    /// Diffs and files open beside the list in a split view; folders and commits go down it.
    var opensBeside: Bool {
        switch self {
        case .diff, .file: true
        case .folder, .commit: false
        }
    }
}

/// A diff that tells the root when a write went through, so a list beside it reads again.
private struct DiffDestination: View {
    @Environment(\.reviewEnded) private var ended
    @State var model: DiffModel
    let onOpen: (ReviewRoute) -> Void
    let onWrite: () -> Void

    var body: some View {
        DiffScreen(model: model, onOpen: onOpen)
            .onChange(of: model.completedWrites) { onWrite() }
            .onChange(of: ended, initial: true) { _, why in model.ended = why }
    }
}

extension EnvironmentValues {
    /// Why the review can no longer change anything, once its session or machine is gone.
    @Entry var reviewEnded: String?
    /// Set by a file's find field while it is open, so Escape closes the field before the review.
    @Entry var reviewFindOpen: Binding<Bool>?
}

// MARK: Presentation

extension View {
    /// Shows the store's review sheet over this view while `active`: a full-screen cover in a
    /// compact window, a large sheet in a regular one, chosen when it opens and kept until it
    /// closes. The terminal keeps running underneath.
    func reviewPresentation(store: AppStore, terminals: TerminalPool, active: Bool) -> some View {
        modifier(ReviewPresenter(store: store, terminals: terminals, active: active))
    }
}

private struct ReviewPresenter: ViewModifier {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.windowWidth) private var windowWidth
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: AppStore
    let terminals: TerminalPool
    let active: Bool

    func body(content: Content) -> some View {
        content
            .presentCover("review.cover", item: request(regular: false)) { request in
                root(request, regular: false)
                    .modifier(ReviewEntrance())
            }
            .presentSheet("review.sheet", item: request(regular: true)) { request in
                // Page-sized, so the sheet keeps a regular width and shows the split view: a
                // form sheet would be compact inside and fall back to the phone's stack.
                root(request, regular: true)
                    .presentationDetents([.large])
                    .presentationSizing(.page)
                    .modifier(ReviewEntrance())
            }
            // Under Reduce Motion the cover does not slide; the review fades in where it stands.
            .transaction(value: store.reviewing?.id) { if reduceMotion { $0.disablesAnimations = true } }
            .onChange(of: store.reviewing?.id) { _, open in if open == nil { store.reviewClosed() } }
    }

    private func request(regular: Bool) -> Binding<ReviewRequest?> {
        Binding {
            guard active, let request = store.reviewing else { return nil }
            let kept = store.openReview?.subject == request.subject ? store.openReview?.regular : nil
            return ReviewPresentation.regular(kept: kept, sizeClass: sizeClass, width: windowWidth) == regular ? request : nil
        } set: { request in
            if request == nil, store.reviewing != nil { store.sheet = nil }
        }
    }

    private func root(_ request: ReviewRequest, regular: Bool) -> some View {
        ReviewHost(store: store, terminals: terminals, request: request, regular: regular)
    }
}

/// The review for one request, following what the store says about it while it is open.
private struct ReviewHost: View {
    let store: AppStore
    let terminals: TerminalPool
    let request: ReviewRequest
    let regular: Bool

    var body: some View {
        let tab = TabID(hostID: request.subject.hostID, sessionID: request.subject.sessionID)
        let machine = store.machineName(for: request.subject.hostID)
        ReviewRoot(
            subject: request.subject, services: store.reviewServices, initialTab: request.tab,
            machine: machine,
            sendToSession: ReviewRoot.sendToSession { [terminals] in terminals.handle(for: tab) },
            session: store.keptReview(
                for: request.subject, services: store.reviewServices, tab: request.tab, machine: machine, regular: regular
            ),
            onSelectTab: { [store, request] tab in
                if store.reviewing?.subject == request.subject { store.sheet = .review(request.subject, tab) }
            },
            onWrite: { [store] in store.reviewWrites += 1 },
            ended: store.reviewEnded,
            onDraft: { [store] draft in store.reviewHasDraft = draft }
        )
        .tint(Color.togoSnow)
    }
}

/// Under Reduce Motion the review cross-fades in over 200 ms instead of sliding up.
private struct ReviewEntrance: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var shown = false

    func body(content: Content) -> some View {
        if reduceMotion {
            content
                .opacity(shown ? 1 : 0)
                // motion: origin — under Reduce Motion a fade stands in for the slide up
                .onAppear { withAnimation(Motion.animation(.rowGrow, reduce: reduceMotion)) { shown = true } }
        } else {
            content
        }
    }
}

#if DEBUG
#Preview("Review") {
    ReviewRoot(
        subject: DiffModel.previewSubject, services: FakeReviewServices(), initialTab: .changes, machine: "Studio",
        sendToSession: { _ in true }
    )
}
#endif
