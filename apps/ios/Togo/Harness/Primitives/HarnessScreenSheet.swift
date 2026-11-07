import SwiftUI
import TogoKit

/// A native screen in a sheet: reads it from the session, draws it, and pushes any screen it
/// opens onto the same stack. "Terminal" closes the sheet and switches the session to its
/// terminal; open targets other than screens go to `onOpen`. A pick that lands closes the sheet
/// to the chat, which says what changed (one rule for every host but mission control's pushes,
/// which keep Back).
struct HarnessScreenSheet: View {
    @Environment(\.dismiss) private var dismiss
    let store: HarnessSessionStore
    let screenID: String
    var onOpen: (HarnessOpen) -> Void = { _ in }
    @State private var path: [String] = []

    var body: some View {
        NavigationStack(path: $path) {
            loader(screenID)
                .navigationDestination(for: String.self) { loader($0) }
        }
        .presentationDetents([.medium, .large])
        .presentationBackground(Color.togoInk)
    }

    private func loader(_ id: String) -> some View {
        HarnessScreenLoader(
            store: store, screenID: id,
            openScreen: { path.append($0) },
            open: { target in
                if target.kind == .screen {
                    path.append(target.ref)
                } else {
                    dismiss()
                    onOpen(target)
                }
            },
            showTerminal: {
                dismiss()
                store.layer = .terminal
            },
            onSettled: { dismiss() }
        )
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button(PrimitiveCopy.done, systemImage: "checkmark", role: .confirm) { dismiss() }
            }
        }
        .navigationBarTitleDisplayMode(.inline)
    }
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
    @State private var phase: Phase = .loading

    var body: some View {
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
        .task(id: screenID) { await load(showingProgress: true) }
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
        var context = PrimitiveContext.store(store, openScreen: openScreen, open: open, showTerminal: showTerminal)
        context.reload = { await load(showingProgress: false) }
        context.settled = onSettled
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
