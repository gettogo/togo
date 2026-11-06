import SwiftUI

/// The terminal area of the workspace: one pane, two side by side, or on a Duo held as a
/// tabletop the terminal above the fold and the deck below it. One large terminal is the
/// default on every window; a second pane appears only once the user puts a tab beside.
///
/// The panes keep their place in the view tree whatever the arrangement, and the pool owns the
/// terminal controllers, so folding, unfolding or resizing the window moves terminals without
/// rebuilding or reconnecting them.
struct WorkspacePanes<Overlay: View>: View {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.fold) private var fold
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: AppStore
    let terminals: TerminalPool
    let window: WindowID
    /// Drawn over the panes in the same layout pass, so what it places from the geometry is
    /// right from the first frame. The insets are the safe area the panes reach under.
    private let overlay: (PaneGeometry, EdgeInsets) -> Overlay

    /// Under Reduce Motion a new arrangement lands at once and fades up from this.
    @State private var settling = false

    init(
        store: AppStore, terminals: TerminalPool, window: WindowID,
        @ViewBuilder overlay: @escaping (PaneGeometry, EdgeInsets) -> Overlay
    ) {
        self.store = store
        self.terminals = terminals
        self.window = window
        self.overlay = overlay
    }

    var body: some View {
        // The outer reader stops at the safe area, the inner one reaches under the home
        // indicator, the bars and the keyboard; the difference is what the overlay keeps clear of.
        GeometryReader { safe in
            panesReader(safe: safe.size)
        }
    }

    private func panesReader(safe: CGSize) -> some View {
        GeometryReader { proxy in
            let origin = proxy.frame(in: .named(PaneGeometry.space)).origin
            let local = fold.moved(by: CGPoint(x: -origin.x, y: -origin.y))
            let report = PanesReport(
                size: proxy.size, sizeClass: sizeClass, fold: local,
                hasSecondary: store.workspace.secondary != nil, primaryFraction: store.workspace.primaryFraction
            )
            panes(report.geometry, width: proxy.size.width, handle: local.inactiveDivision == nil && local.division == nil)
                // Folds, Split View and Stage Manager move the panes; under Reduce Motion the new
                // arrangement lands in place and fades up instead.
                .transaction(value: report.geometry) { transaction in
                    // A change that asks for none, such as zen mode, resizes the terminal once.
                    guard !transaction.disablesAnimations else { return }
                    transaction.animation = reduceMotion ? nil : Motion.animation(.paneFlow, reduce: false)
                }
                .opacity(settling ? 0.5 : 1)
                .overlay(alignment: .topLeading) {
                    let bottom = max(0, proxy.size.height - safe.height)
                    overlay(report.geometry, EdgeInsets(top: 0, leading: 0, bottom: bottom, trailing: 0))
                }
                .preference(key: PanesReport.Key.self, value: report)
                .onChange(of: report.geometry.showsTwoPanes, initial: true) { _, visible in
                    // A first pass before the window has a size says nothing about the panes.
                    guard proxy.size != .zero else { return }
                    store.arrangePanes(visible: visible)
                }
                .onChange(of: report.arrangement.kind) {
                    guard reduceMotion else { return }
                    settling = true
                    withAnimation(Motion.animation(.paneFlow, reduce: true)) { settling = false }
                }
                .onChange(of: KeyboardOwner(focused: store.focusedTab?.id, twoPanes: report.geometry.showsTwoPanes)) {
                    keepKeyboardWithFocus()
                }
        }
        // Terminals size themselves to the keyboard and the home indicator, so the panes reach
        // the bottom edge and a keyboard never rearranges them.
        .ignoresSafeArea(.container, edges: .bottom)
        .ignoresSafeArea(.keyboard)
        // A Compose key pressed in the other pane's terminal moves focus there with it.
        .onChange(of: store.composing) { _, id in
            guard let id, store.panesVisible else { return }
            if id == store.workspace.secondary {
                store.focus(.secondary)
            } else if id == store.workspace.selected {
                store.focus(.primary)
            }
        }
    }

    /// Typing goes to the focused pane. When the other pane's terminal has the keyboard, as
    /// after a chip tap, ⌘1-9, Swap or a terminal appearing on unfold, the focused one takes
    /// it. A keyboard held by anything else, such as the compose field, is left alone.
    private func keepKeyboardWithFocus() {
        Self.moveKeyboard(to: store, terminals: terminals)
        Task { @MainActor in
            // A terminal that appears takes the keyboard as it does; let that happen first.
            try? await Task.sleep(for: .milliseconds(120))
            Self.moveKeyboard(to: store, terminals: terminals)
        }
    }

    static func moveKeyboard(to store: AppStore, terminals: TerminalPool) {
        guard store.panesVisible, let focused = store.focusedTab?.id,
              let other = store.focusedTab?.id == store.workspace.selected ? store.workspace.secondary : store.workspace.selected,
              terminals.handle(for: other)?.holdsKeyboard == true else { return }
        terminals.handle(for: focused)?.raiseKeyboard()
    }

    private func panes(_ geometry: PaneGeometry, width: CGFloat, handle: Bool) -> some View {
        ZStack(alignment: .topLeading) {
            Color.togoInk
            pane(.primary, in: geometry.primary, header: geometry.showsTwoPanes)
            if let rect = geometry.secondary {
                // Leaves at once rather than fading: a pane on its way out still holds a terminal
                // the remaining pane may be about to show.
                pane(.secondary, in: rect, header: true)
                    .transition(.identity)
            }
            if let band = geometry.band {
                if geometry.bandIsFold {
                    // The fold itself: nothing is drawn or placed in it.
                    Color.togoInk
                        .frame(width: band.width, height: band.height)
                        .offset(x: band.minX, y: band.minY)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                } else {
                    PaneDivider(store: store, width: width, height: band.height, position: band.minX, handle: handle)
                        .offset(x: band.minX, y: band.minY)
                }
            }
        }
    }

    @ViewBuilder private func pane(_ pane: Pane, in rect: CGRect, header: Bool) -> some View {
        let id = pane == .primary ? store.workspace.selected : store.workspace.secondary
        let tab = store.workspace.tabs.first { $0.id == id }
        PaneView(tab: tab, pane: pane, store: store, terminals: terminals, window: window, header: header)
            .frame(width: rect.width, height: rect.height)
            .clipped()
            .offset(x: rect.minX, y: rect.minY)
    }
}


extension WorkspacePanes where Overlay == EmptyView {
    init(store: AppStore, terminals: TerminalPool, window: WindowID) {
        self.init(store: store, terminals: terminals, window: window) { _, _ in EmptyView() }
    }
}

/// What decides which terminal should have the keyboard.
private struct KeyboardOwner: Equatable {
    var focused: TabID?
    var twoPanes: Bool
}
