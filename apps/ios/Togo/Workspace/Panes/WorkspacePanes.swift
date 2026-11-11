import SwiftUI
import UIKit

/// The terminal area of the workspace: one pane, two side by side, or on a Duo held as a
/// tabletop the terminal above the fold and the deck below it. One large terminal is the
/// default on every window; a second pane appears only once the user puts a tab beside.
///
/// The panes keep their place in the view tree whatever the arrangement, and the pool owns the
/// terminal controllers, so folding, unfolding or resizing the window moves terminals without
/// rebuilding or reconnecting them.
struct WorkspacePanes<Overlay: View>: View {
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.fold) private var fold
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: AppStore
    let terminals: TerminalPool
    let window: WindowID
    /// Drawn over the panes in the same layout pass, so what it places from the geometry is
    /// right from the first frame. The insets are the safe area the panes reach under.
    private let overlay: (PaneGeometry, EdgeInsets) -> Overlay

    /// The panes' last settle, so a new arrangement or size fades the panes up into it.
    @State private var settle = PaneSettle()

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
                .overlay(alignment: .topLeading) {
                    let bottom = max(0, proxy.size.height - safe.height)
                    overlay(report.geometry, EdgeInsets(top: 0, leading: 0, bottom: bottom, trailing: 0))
                }
                // The terminal never animates its frame (motion spec §4): a fold, Split View, Stage
                // Manager, the sidebar or a second pane lands every pane, and the chat over it, at its
                // final frame in one step, so each grid is resized once. What moves is a cover: the
                // panes dim as they land and brighten into the new arrangement.
                .transaction(value: report.geometry) { transaction in
                    transaction.animation = nil
                }
                .overlay {
                    // A new cover for each settle: it arrives dimming the panes and lifts away.
                    PaneCover(color: canvas(report.geometry)).id(settle.generation)
                }
                .preference(key: PanesReport.Key.self, value: report)
                .onChange(of: report.geometry.showsTwoPanes, initial: true) { _, visible in
                    // A first pass before the window has a size says nothing about the panes.
                    guard proxy.size != .zero else { return }
                    store.arrangePanes(visible: visible)
                }
                .onChange(of: PaneSettle.Key(report)) { old, new in
                    // motion: preventing a jump — the cover lifts off the panes as the new arrangement lands, hiding the terminal's one resize.
                    withAnimation(Motion.animation(.reveal, reduce: reduceMotion)) { _ = settle.begin(from: old, to: new) }
                }
                .onChange(of: KeyboardOwner(focused: store.focusedTab?.id, twoPanes: report.geometry.showsTwoPanes)) {
                    Self.moveKeyboard(to: store, terminals: terminals)
                }
                // A terminal that appears takes the keyboard as it does; when it was the other
                // pane's, the focused one takes it back in the same keyboard change.
                .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in
                    Self.moveKeyboard(to: store, terminals: terminals)
                }
        }
        // Terminals size themselves to the keyboard and the home indicator, so the panes reach
        // the bottom edge and a keyboard never rearranges them.
        .ignoresSafeArea(.container, edges: .bottom)
        .ignoresSafeArea(.keyboard)
    }

    /// Typing goes to the focused pane. When the other pane's terminal has the keyboard, as
    /// after a chip tap, ⌘1-9, Swap or a terminal appearing on unfold, the focused one takes
    /// it. A keyboard held by anything else, such as the compose field, is left alone.
    static func moveKeyboard(to store: AppStore, terminals: TerminalPool) {
        guard store.panesVisible, let focused = store.focusedTab?.id,
              let other = store.focusedTab?.id == store.workspace.selected ? store.workspace.secondary : store.workspace.selected,
              terminals.handle(for: other)?.holdsKeyboard == true else { return }
        terminals.handle(for: focused)?.raiseKeyboard(automatic: true)
    }

    private func panes(_ geometry: PaneGeometry, width: CGFloat, handle: Bool) -> some View {
        let ground = canvas(geometry)
        return ZStack(alignment: .topLeading) {
            ground
            pane(.primary, in: geometry.primary, header: geometry.showsTwoPanes)
            if let rect = geometry.secondary {
                // Leaves at once rather than fading: a pane on its way out still holds a terminal
                // the remaining pane may be about to show.
                pane(.secondary, in: rect, header: true)
                    // motion: continuity — the leaving pane goes at once: its terminal may be about to show in the other pane.
                    .transition(.identity)
            }
            if let band = geometry.band {
                if geometry.bandIsFold {
                    // The fold itself: nothing is drawn or placed in it.
                    ground
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

    /// What shows between and under the panes: the terminal theme's background while every
    /// pane on screen shows its terminal, so no app-coloured strip runs down the fold; ink
    /// beside a chat.
    private func canvas(_ geometry: PaneGeometry) -> Color {
        let shown = geometry.secondary == nil ? [store.workspace.selected] : [store.workspace.selected, store.workspace.secondary]
        guard shown.allSatisfy(store.showsTerminal) else { return .togoInk }
        return TerminalBackdrop.resolve(store.services.settings, system: colorScheme).background
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

/// The cover the panes land under. A change of arrangement or of the panes' size lands at once
/// and the panes brighten from `coverOpacity`; a divider dragged to a new split does the same.
/// The panes never animate their frames, so a terminal's grid is resized once per settle.
struct PaneSettle: Equatable {
    /// How far the panes dim as a new arrangement lands.
    static let coverOpacity = 0.55

    /// What a settle is keyed on: the arrangement, the panes' size and the split. Rounded to
    /// whole points, so a sub-point wobble in a measure is not a new arrangement.
    struct Key: Equatable {
        var kind: PaneArrangement.Kind
        var width: Int
        var height: Int
        var split: Int

        init(_ report: PanesReport) {
            kind = report.arrangement.kind
            let bounds = report.geometry.primary.union(report.geometry.secondary ?? report.geometry.primary)
            width = Int(bounds.maxX.rounded())
            height = Int(bounds.maxY.rounded())
            split = Int(report.geometry.primary.width.rounded())
        }

        init(kind: PaneArrangement.Kind, width: Int, height: Int, split: Int) {
            self.kind = kind
            self.width = width
            self.height = height
            self.split = split
        }

        /// The split as a share of the panes' width, in thousandths: what the user or the fold
        /// chose, apart from how wide the window or column is.
        var share: Int { width > 0 ? Int((Double(split) / Double(width) * 1000).rounded()) : 0 }
    }

    /// How many settles began. Each one is a new cover, and lands each terminal's grid once.
    private(set) var generation = 0

    /// Starts a settle for a move from `old` to `new`: a second pane, a released divider or a
    /// fold moving the split. False when only the room changed (the sidebar or the inspector
    /// column coming or going, a window resize): the split view's own column animation is the
    /// motion then, and a cover dimming over it would fight it. False too when the panes had no
    /// size yet (the first layout lands bare).
    @discardableResult
    mutating func begin(from old: Key, to new: Key) -> Bool {
        guard old.width > 0, old.height > 0 else { return false }
        guard old.kind != new.kind || abs(old.share - new.share) > 5 else { return false }
        generation += 1
        return true
    }
}

/// Ink over the panes as a new arrangement lands. Its resting state is clear; it arrives at
/// full strength and lifts away on the inserting transaction, so the panes brighten into place.
private struct PaneCover: View {
    /// The panes' canvas, so the cover dims a terminal toward its own background.
    let color: Color

    var body: some View {
        color
            .opacity(1 - PaneSettle.coverOpacity)
            // motion: preventing a jump — ink over the panes lifts away as they settle, so the resize is never seen.
            .transition(CoverLift())
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }
}

/// Shown on insertion, clear at rest and on removal.
private struct CoverLift: Transition {
    func body(content: Content, phase: TransitionPhase) -> some View {
        content.opacity(phase == .willAppear ? 1 : 0)
    }
}

/// What decides which terminal should have the keyboard.
private struct KeyboardOwner: Equatable {
    var focused: TabID?
    var twoPanes: Bool
}

/// The panes' last report, read only by the small views that place things around the panes, so
/// a new report never rebuilds the whole workspace.
@Observable final class PanesState {
    var report = PanesReport.single
}

/// Content built from the panes' report.
struct WithPanes<Content: View>: View {
    let state: PanesState
    @ViewBuilder let content: (PanesReport) -> Content

    var body: some View {
        content(state.report)
    }
}

/// Keeps a full-width bar to the columns the panes give it, such as the leading page of a book,
/// so nothing in it lands on the fold. Without columns it keeps the full width.
struct PaneColumns: ViewModifier {
    let state: PanesState
    let pick: (PanesReport) -> CGRect?

    func body(content: Content) -> some View {
        if let frame = pick(state.report), frame.width > 0 {
            content
                .frame(width: frame.width)
                .frame(maxWidth: .infinity, alignment: .leading)
                .offset(x: frame.minX)
        } else {
            content
        }
    }
}
