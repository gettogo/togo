import SwiftUI
import TogoKit
import UIKit

/// An open needs card as the chat's scrolling sees it.
struct NeedsMark: Hashable, Sendable {
    var id: String
    /// Its answers are on screen: an approval's request card arrived; a question always is.
    var ready: Bool
}

/// When the chat scrolls on its own, worked out from plain values so it can be tested.
///
/// One thing owns the scroll offset: the scroll view's own anchor (`foot`, for the first
/// layout and for every size change). The composer growing a line, the keyboard coming or
/// going, a fold, a streaming reply: the scroll view keeps its foot by itself, and nothing
/// here scrolls for a size change. The chat scrolls only for an event: a needs card arriving
/// or growing its answers, the user's own send and the live line that follows it, a field a
/// card revealed, a "Go to request". Never while the user's finger is on the chat.
enum ChatScroll {
    enum Target: Equatable {
        case row(String)
        case bottom
    }

    /// What happens to a chat, as its scrolling sees it.
    enum Event: Equatable {
        /// The user's own send.
        case sent
        /// The live line appeared; `afterSend` says the user's own prompt started the turn.
        case liveAppeared(afterSend: Bool)
        /// The open needs cards changed.
        case needsChanged(old: [NeedsMark], new: [NeedsMark])
        /// A card revealed a field, or "Go to request" named a row.
        case reveal(String)
        /// The scroll view changed size: the composer, the keyboard, a fold.
        case resized
        /// The content changed size: a streaming reply, a card growing, Earlier loading.
        case contentChanged
    }

    /// The point every scroll of a chat keeps: the bottom edge, at the leading side. Its x is
    /// not neutral: the anchor lines a point of the content up with the same point of the
    /// scroll view on both axes, and nothing in a vertical scroll view takes a horizontal offset
    /// back. With `.bottom` (x 0.5) a chat resting at its foot sat 16 pt left, the column's
    /// inset, text flush with the pane's edge, until a needs card was scrolled to (the same
    /// build, the approval state, had its margins). At x 0, with every row exactly as wide as
    /// the scroll view (`chatColumn()`), every scroll lands at horizontal offset 0.
    static let foot = UnitPoint(x: 0, y: 1)

    /// The chat's content as a whole, so a scroll to the foot has a view to aim at: its bottom
    /// edge at `foot` is the foot, above the bottom margin.
    static let contentID = "chat.content"

    /// The one decision: given what happened and whether a finger is on the chat, where the
    /// chat scrolls, or nowhere. A size change is never a reason: the anchor keeps the foot.
    static func decide(_ event: Event, dragging: Bool) -> Target? {
        guard !dragging else { return nil }
        switch event {
        case .sent:
            return .bottom
        case .liveAppeared(let afterSend):
            return afterSend ? .bottom : nil
        case .needsChanged(let old, let new):
            return newest(old: old, new: new).map(Target.row)
        case .reveal(let id):
            return .row(id)
        case .resized, .contentChanged:
            return nil
        }
    }

    /// The open needs cards in row order. `ready` says whether an approval's card has arrived.
    static func marks(_ rows: [ChatRow], ready: (HarnessItem.Approval) -> Bool) -> [NeedsMark] {
        rows.compactMap { row in
            guard case .needs(let item) = row, WorkingLogic.hasOpenNeeds([item]) else { return nil }
            if case .approval(let approval) = item.body { return NeedsMark(id: item.id, ready: ready(approval)) }
            return NeedsMark(id: item.id, ready: true)
        }
    }

    /// The card to scroll to after the open cards changed: the newest that arrived, or whose
    /// answers just landed (waiting → card). Nil when nothing new came.
    static func newest(old: [NeedsMark], new: [NeedsMark]) -> String? {
        let before = Dictionary(old.map { ($0.id, $0.ready) }, uniquingKeysWith: { first, _ in first })
        return new.last { mark in
            guard let wasReady = before[mark.id] else { return true }
            return !wasReady && mark.ready
        }?.id
    }

    /// At the foot, within a few points: the last line shows above the composer. The visible
    /// region starts below the top inset (the tab strip's bar the chat scrolls under, and the
    /// chat's own top margin); the bottom margin is no more content than the top one is.
    static func isPinned(offset: CGFloat, topInset: CGFloat, container: CGFloat, content: CGFloat) -> Bool {
        offset + topInset + container >= content - 8
    }

    /// Where a rebuilt chat returns to: nothing at the foot (the default anchor gives it), the
    /// reader's row while it is still in the chat, the foot once it is gone.
    static func restore(row: String?, pinned: Bool, has: (String) -> Bool) -> Target? {
        guard !pinned, let row else { return nil }
        return has(row) ? .row(row) : .bottom
    }

    /// The card the "Needs you" capsule jumps to: the oldest open card off screen. Nothing
    /// before the first load, and nothing while every open card is visible.
    static func capsule(open: [NeedsMark], visible: Set<String>, loaded: Bool) -> String? {
        guard loaded else { return nil }
        return open.first { !visible.contains($0.id) }?.id
    }
}

/// What the chat's scrolling keeps between renders: requests going in, and what the scroll
/// view reports coming out. It never holds a `ScrollPosition`, and the chat binds none: a
/// bound position is a standing order the scroll view re-applies on every size change (it
/// scrolls the bound row or edge back to the anchor), a second owner of the offset beside the
/// anchor; and bound to an observed property it froze the app (the write invalidated the
/// modifier that bound it, which re-applied it, forever). Scrolls here are one-shot asks.
@MainActor @Observable final class ChatScrollModel {
    /// A scroll asked of the chat. `seed` tells two asks for the same place apart.
    struct Request: Equatable {
        var target: ChatScroll.Target
        var reduce: Bool
        var seed: Int
    }

    /// The latest scroll asked for; the chat applies it once.
    private(set) var request: Request?
    /// Needs cards on screen now.
    private(set) var visibleNeeds: Set<String> = []
    /// The card the capsule offers, once it has stayed off screen a moment.
    var capsule: String?
    /// What the scroll view reports. Unobserved: nothing draws from them, so the scroll view's
    /// own reports never invalidate the views that drive it.
    @ObservationIgnored var dragging = false
    @ObservationIgnored var pinned = true
    /// The row at the chat's bottom edge, so a rebuilt chat returns to it.
    @ObservationIgnored private(set) var settledRow: String?

    /// Scrolls on `resize`, the same role that grows a card, so the chat makes room and brings
    /// the card up as one movement. A card still growing is followed once more when it has
    /// grown (`settle`), unless the user has taken the scroll meanwhile.
    func go(_ target: ChatScroll.Target, reduce: Bool, settle: Bool = false) {
        ask(target, reduce: reduce)
        guard settle else { return }
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: .milliseconds(400))
            guard let self, !self.dragging else { return }
            self.ask(target, reduce: reduce)
        }
    }

    private func ask(_ target: ChatScroll.Target, reduce: Bool) {
        request = Request(target: target, reduce: reduce, seed: (request?.seed ?? 0) &+ 1)
    }

    /// The row at the chat's bottom edge is `row`. Writes only a change. Returns whether it changed.
    @discardableResult func settle(at row: String?) -> Bool {
        guard settledRow != row else { return false }
        settledRow = row
        return true
    }

    /// Where a chat made again for the same session (a fold or unfold moves the workspace to
    /// another presenter) returns to: the row the reader had at the bottom, if it is still
    /// there, else the foot. Nil at the foot, which the default anchor already gives.
    func restoreTarget(has: (String) -> Bool) -> ChatScroll.Target? {
        ChatScroll.restore(row: settledRow, pinned: pinned, has: has)
    }

    func setVisible(_ id: String, _ visible: Bool) {
        if visible {
            if !visibleNeeds.contains(id) { visibleNeeds.insert(id) }
        } else if visibleNeeds.contains(id) {
            visibleNeeds.remove(id)
        }
    }
}

/// The one owner of a conversation's scrolling. The main chat and a subagent's chat both use it.
///
/// The scroll view's anchor keeps the foot: the chat opens there, and a reader at the foot
/// stays there as the composer grows or shrinks, as the keyboard comes or goes, as a reply
/// streams in (`defaultScrollAnchor`, for the first layout and for size changes). Nothing
/// else moves the offset for a size change. Events scroll once, through the reader's proxy:
/// a needs card arriving or its answers landing, a field a card revealed, and the model's
/// requests (a send, "Go to request", a rebuilt chat returning to its row). The scroll view
/// only reports back: the finger, the foot, the row at the bottom edge.
struct NeedsScrolling: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let marks: [NeedsMark]
    /// Arrivals before the first load are not arrivals.
    let loaded: Bool
    let scroll: ChatScrollModel
    /// Whether a row is in the chat, for returning to the reader's place once. Nil: nothing to return to.
    var has: ((String) -> Bool)?

    @State private var restored = false

    func body(content: Content) -> some View {
        ScrollViewReader { proxy in
            content
                // The chat opens at its foot, and keeps it through every size change: the row
                // above the composer stays above the composer as the composer grows or the
                // keyboard rises, and the latest line stays in view as a reply streams.
                .defaultScrollAnchor(ChatScroll.foot)
                .defaultScrollAnchor(ChatScroll.foot, for: .sizeChanges)
                .onScrollPhaseChange { _, phase in
                    let dragging = phase == .interacting || phase == .tracking
                    if scroll.dragging != dragging { scroll.dragging = dragging }
                }
                .onScrollGeometryChange(for: Bool.self) { geometry in
                    ChatScroll.isPinned(
                        offset: geometry.contentOffset.y, topInset: geometry.contentInsets.top,
                        container: geometry.containerSize.height, content: geometry.contentSize.height
                    )
                } action: { _, pinned in
                    // Only reported, never acted on: a size change is the anchor's to keep.
                    if scroll.pinned != pinned { scroll.pinned = pinned }
                }
                // The row at the bottom edge, so a rebuilt chat can return to it; an
                // unobserved write, only on a change.
                .onScrollTargetVisibilityChange(idType: String.self, threshold: 0) { visible in
                    scroll.settle(at: visible.last)
                }
                .onChange(of: scroll.request) { _, request in
                    guard let request else { return }
                    move(request.target, reduce: request.reduce, proxy: proxy)
                }
                // A chat made again returns to the reader's row once, after its first layout; a
                // row that has gone sends it to the foot.
                .task {
                    guard !restored else { return }
                    restored = true
                    guard let has else { return }
                    await Task.yield()
                    guard !scroll.dragging, let target = scroll.restoreTarget(has: has) else { return }
                    move(target, reduce: true, proxy: proxy)
                }
                .onChange(of: marks) { old, new in
                    guard loaded,
                          let target = ChatScroll.decide(.needsChanged(old: old, new: new), dragging: scroll.dragging)
                    else { return }
                    scroll.go(target, reduce: reduceMotion, settle: true)
                }
                // A field a card just revealed rises above the composer, and once more after the
                // keyboard has come up (`settle`).
                .environment(\.revealRow, RevealRowAction { [reduceMotion, scroll] id in
                    guard let target = ChatScroll.decide(.reveal(id), dragging: scroll.dragging) else { return }
                    scroll.go(target, reduce: reduceMotion, settle: true)
                })
        }
    }

    /// One scroll, through the proxy: a one-shot ask the scroll view does not keep.
    private func move(_ target: ChatScroll.Target, reduce: Bool, proxy: ScrollViewProxy) {
        let apply = {
            switch target {
            case .row(let id): proxy.scrollTo(id, anchor: ChatScroll.foot)
            case .bottom: proxy.scrollTo(ChatScroll.contentID, anchor: ChatScroll.foot)
            }
        }
        guard !reduce else {
            var transaction = Transaction(animation: nil)
            transaction.disablesAnimations = true
            withTransaction(transaction, apply)
            return
        }
        // motion: preventing a jump — the chat glides to the card that needs you (or to your
        // own prompt) instead of teleporting; under Reduce Motion it lands at once.
        withAnimation(Motion.animation(.resize, reduce: false), apply)
    }
}

/// The main chat's scrolling: needs cards (`NeedsScrolling`), the user's own sends, "Go to
/// request", and the "Needs you" capsule while a card is off screen.
struct ChatScrolling: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: HarnessSessionStore
    let marks: [NeedsMark]
    let scroll: ChatScrollModel
    /// Whether a row is in the chat, for returning to the reader's place (`NeedsScrolling`).
    let has: (String) -> Bool

    func body(content: Content) -> some View {
        let candidate = ChatScroll.capsule(open: marks, visible: scroll.visibleNeeds, loaded: store.hasLoaded)
        content
            .modifier(NeedsScrolling(marks: marks, loaded: store.hasLoaded, scroll: scroll, has: has))
            // The chat ends at the composer's top edge (`ChatPanes`); the capsule sits just above it.
            .overlay(alignment: .bottom) {
                if let target = scroll.capsule {
                    // motion: attention — fades in place, never rises (timed by the task below).
                    NeedsJumpPill { store.revealNeeds(id: target) }
                        .padding(.bottom, 10)
                        .transition(.opacity)
                }
            }
            .onChange(of: store.sends) {
                guard let target = ChatScroll.decide(.sent, dragging: scroll.dragging) else { return }
                scroll.go(target, reduce: reduceMotion)
            }
            .onChange(of: store.scrollRequest) { _, request in
                guard let request else { return }
                if let target = ChatScroll.decide(.reveal(request.row), dragging: scroll.dragging) {
                    scroll.go(target, reduce: reduceMotion, settle: true)
                }
                store.scrollRequest = nil
            }
            .task(id: candidate) {
                guard let candidate else {
                    // motion: state change — the card is in view, so the capsule fades where it was.
                    withAnimation(Motion.animation(.respond, reduce: reduceMotion)) { scroll.capsule = nil }
                    return
                }
                // A card scrolled past for a moment is not news; one that stays off screen is.
                try? await Task.sleep(for: .milliseconds(600))
                guard !Task.isCancelled else { return }
                // motion: attention — a card that needs you is below, off screen; the capsule
                // fades in place above the composer and leaves once the card is in view.
                withAnimation(Motion.animation(.respond, reduce: reduceMotion)) { scroll.capsule = candidate }
            }
    }
}

/// The chat's reading column, shared by everything that sits in it or under it (the composer,
/// a helper's thread, a background terminal), so on a wide window they line up.
enum ChatLayout {
    /// The column's widest, centred in wider panes.
    static let columnWidth: CGFloat = 720
    /// The column's own side padding; text starts this far inside it.
    static let inset: CGFloat = 16
    /// The space above the first row and below the row a scroll lands on: the last line at
    /// rest, or a needs card scrolled up to the composer. The column's own padding, so text
    /// keeps as far from the composer as from the pane's edge.
    static let endMargin: CGFloat = inset
    /// The composer's widest: the column's text width, so the field starts where replies do.
    static let composerWidth: CGFloat = columnWidth - 2 * inset

    /// The text's width in a scroll view `width` wide: the column, less its padding.
    static func textWidth(in width: CGFloat) -> CGFloat {
        max(0, min(width, columnWidth) - 2 * inset)
    }

    /// Where the text starts in a row `width` wide (`chatColumn()`), and how far in a prompt
    /// bubble ends: `inset` up to the column's widest, centred beyond it.
    static func textLeading(in width: CGFloat) -> CGFloat {
        max(0, (width - textWidth(in: width)) / 2)
    }
}

extension View {
    /// One row of a chat's scroll content: exactly as wide as the scroll view, its content in
    /// the reading column centred in it, `ChatLayout.textLeading` in from either edge. The row
    /// owns its width, so one that measures wider than offered (a code well, a long token)
    /// overflows trailing and never moves the others; and a row spanning the scroll view
    /// gives the scroll anchor (`ChatScroll.foot`) nothing to offset horizontally.
    func chatColumn() -> some View {
        // The reading column: the text width, any overflow trailing.
        containerRelativeFrame(.horizontal, alignment: .leading) { width, _ in ChatLayout.textWidth(in: width) }
            // The row: the scroll view's width, the column centred in it.
            .containerRelativeFrame(.horizontal)
    }

    /// A chat's scroll view keeps `ChatLayout.endMargin` clear at its top and bottom as content
    /// margins, not as padding inside the content. Padding is part of the content, so it showed
    /// only at the very end: a scroll to a row (`ChatScroll.foot`) put that row's bottom edge
    /// on the scroll view's, a needs card flush against the composer. A margin is where every
    /// scroll lands, to the foot or to a row, with or without the keyboard, and what the
    /// anchor keeps through every size change.
    func chatScrollMargins() -> some View {
        contentMargins(.vertical, ChatLayout.endMargin, for: .scrollContent)
    }

    /// Reports whether a needs card is on screen, for the "Needs you" capsule.
    func needsVisibility(_ id: String, in scroll: ChatScrollModel) -> some View {
        onScrollVisibilityChange(threshold: 0.25) { visible in scroll.setVisible(id, visible) }
    }
}

/// Tells the scroll model whether a needs card is on screen; other rows pass through.
struct NeedsVisibility: ViewModifier {
    let id: String?
    let scroll: ChatScrollModel

    func body(content: Content) -> some View {
        if let id {
            content.needsVisibility(id, in: scroll)
        } else {
            content
        }
    }
}
