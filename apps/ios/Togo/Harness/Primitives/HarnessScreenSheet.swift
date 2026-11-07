import SwiftUI
import TogoKit

/// A native screen presented on its own (from the composer, the session menu or a palette
/// hand-over): reads it from the session and draws it as the root of its presentation. A
/// browse list with groups gets a split view where the sheet has the room; anything else gets a
/// stack, and any screen it opens is pushed onto that stack. "Terminal" closes the presentation
/// and switches the session to its terminal; open targets other than screens go to `onOpen`. A
/// pick that lands closes it to the chat, which says what changed.
struct HarnessScreenSheet: View {
    @Environment(\.dismiss) private var dismiss
    let store: HarnessSessionStore
    let screenID: String
    var onOpen: (HarnessOpen) -> Void = { _ in }

    var body: some View {
        HarnessScreenLoader(
            store: store, screenID: screenID,
            openScreen: { _ in },
            open: { target in
                dismiss()
                onOpen(target)
            },
            showTerminal: {
                dismiss()
                store.layer = .terminal
            },
            onSettled: { dismiss() },
            root: ScreenDone { dismiss() }
        )
    }
}

/// The Done of a screen that is the root of its presentation.
struct ScreenDone: Equatable {
    var id = "screen"
    let run: @MainActor () -> Void

    @MainActor func callAsFunction() { run() }

    static func == (lhs: Self, rhs: Self) -> Bool { lhs.id == rhs.id }

    @ToolbarContentBuilder @MainActor var item: some ToolbarContent {
        ToolbarItem(placement: .confirmationAction) {
            Button(PrimitiveCopy.done, systemImage: "checkmark", role: .confirm) { run() }
        }
    }
}

/// Pushes a sign-in page onto the root presentation's stack.
struct PushSignIn {
    let run: @MainActor (SignInPageModel) -> Void
}

extension EnvironmentValues {
    /// The root presentation's sign-in push, for screens pushed onto its stack.
    @Entry var pushSignIn: PushSignIn? = nil
    /// How a browse list lays itself out: a split view only where the root host chose one.
    @Entry var browseLayout = BrowseLayout.stack
    /// The root presentation's Done, for a split view that draws its own bars.
    @Entry var screenDone: ScreenDone? = nil
}

/// Reads one screen and draws it, with a loading state and a failure that offers the terminal.
struct HarnessScreenLoader: View {
    enum Phase: Equatable {
        case loading
        case loaded(HarnessScreen)
        /// `fallback`: the harness said no native route serves this screen.
        case failed(fallback: Bool)
    }

    let store: HarnessSessionStore
    let screenID: String
    let openScreen: @MainActor (String) -> Void
    let open: @MainActor (HarnessOpen) -> Void
    let showTerminal: @MainActor () -> Void
    /// A selection or setting landed (applied, typed, restarted or held for the turn's end):
    /// the host closes. nil keeps the screen, as mission control's pushes do.
    var onSettled: (@MainActor () -> Void)? = nil
    /// Set when this screen is the root of its presentation: the loader then supplies the
    /// navigation container (a stack, or the picker's split view) and Done on every page.
    var root: ScreenDone? = nil
    @Environment(\.horizontalSizeClass) private var sizeClass
    /// The root's sign-in push, for screens pushed onto its stack.
    @Environment(\.pushSignIn) private var pushSignIn
    @State private var phase: Phase = .loading
    @State private var path = NavigationPath()
    @State private var width: CGFloat = 0
    /// A screen was opened from a split view: the root becomes a stack so it can push.
    @State private var stacked = false

    var body: some View {
        Group {
            if let root {
                container(root)
                    .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
            } else {
                page
            }
        }
        // Here, not on the page, so a root changing its container does not read again.
        .task(id: screenID) { await load(showingProgress: true) }
    }

    /// The root's container: the picker's own split view where the policy picks one, else a
    /// stack whose pages all carry Done.
    @ViewBuilder private func container(_ done: ScreenDone) -> some View {
        if !stacked, case .loaded(let screen) = phase, layout(of: screen) == .split {
            page
                .environment(\.browseLayout, .split)
                .environment(\.screenDone, done)
        } else {
            NavigationStack(path: $path) {
                page
                    .toolbar { done.item }
                    .navigationBarTitleDisplayMode(.inline)
                    .navigationDestination(for: String.self) { id in
                        HarnessScreenLoader(
                            store: store, screenID: id, openScreen: { path.append($0) },
                            open: { target in
                                if target.kind == .screen { path.append(target.ref) } else { open(target) }
                            },
                            showTerminal: showTerminal, onSettled: onSettled
                        )
                        .toolbar { done.item }
                        .navigationBarTitleDisplayMode(.inline)
                    }
                    .navigationDestination(for: SignInPageModel.self) { page in
                        SignInPage(model: page)
                            .toolbar { done.item }
                    }
            }
            .environment(\.pushSignIn, PushSignIn { page in path.append(page) })
        }
    }

    private func layout(of screen: HarnessScreen) -> BrowseLayout {
        guard case .picker(let picker) = PrimitiveScreenLogic.payload(screen) else { return .stack }
        return AdaptivePresentation.browseLayout(
            sizeClass: sizeClass, containerWidth: width,
            groups: PickerModel.groupNames(picker).count, ownsNavigation: true
        )
    }

    private var page: some View {
        Group {
            switch phase {
            case .loading:
                StateView(kind: .loading, title: PrimitiveCopy.loading, message: "")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Color.togoInk)
                    .navigationTitle(title)
            case .loaded(let screen):
                PrimitiveScreenView(screen: screen, context: context)
            case .failed(let fallback):
                TerminalHandOff(
                    context: context, title: PrimitiveCopy.couldNotLoad,
                    message: fallback ? PrimitiveCopy.fallbackMessage : PrimitiveCopy.noAnswer
                )
                .navigationTitle(title)
                .toolbar {
                    if !fallback {
                        ToolbarItem(placement: .topBarTrailing) {
                            Button(PrimitiveCopy.tryAgain, systemImage: "arrow.clockwise") {
                                Task { await load(showingProgress: true) }
                            }
                        }
                    }
                }
            }
        }
        // A refresh of a loaded screen changes it in place.
        // motion: state change — loading becomes the screen (or its failure) in place, a crossfade with no shift.
        .motion(.fade, value: phaseKind)
    }

    private var phaseKind: Int {
        switch phase {
        case .loading: 0
        case .loaded: 1
        case .failed: 2
        }
    }

    /// The screen's title from the descriptor until the screen itself arrives.
    private var title: String {
        store.descriptor?.screens.first { $0.id == screenID }?.title ?? ""
    }

    private var context: PrimitiveContext {
        // A root's screens push onto its own stack.
        let push: @MainActor (String) -> Void = { id in
            stacked = true
            path.append(id)
        }
        let openScreen: @MainActor (String) -> Void = root == nil ? self.openScreen : push
        var context = PrimitiveContext.store(store, openScreen: openScreen, open: { target in
            if root != nil, target.kind == .screen { push(target.ref) } else { open(target) }
        }, showTerminal: showTerminal)
        context.reload = { await load(showingProgress: false) }
        context.settled = onSettled
        context.readScreen = { [store, screenID] in try? await store.screen(screenID) }
        // A root's sign-in pushes onto its own stack, turning a split view into one first.
        let pushPage: @MainActor (SignInPageModel) -> Void = { page in
            stacked = true
            path.append(page)
        }
        if root != nil {
            context.signIn = pushPage
        } else if let pushSignIn {
            context.signIn = pushSignIn.run
        }
        return context
    }

    /// Reads the screen. A refresh that fails keeps what is on screen.
    private func load(showingProgress: Bool) async {
        if showingProgress, case .loaded = phase {} else if showingProgress { phase = .loading }
        do {
            phase = .loaded(try await store.screen(screenID))
        } catch is CancellationError {
            return
        } catch {
            if case .loaded = phase, !showingProgress { return }
            phase = .failed(fallback: (error as? HelperError)?.isFallback ?? false)
        }
    }
}
