import SwiftUI
import TogoKit
import UIKit

/// An open needs card as the chat's scrolling sees it.
struct NeedsMark: Hashable, Sendable {
    var id: String
    /// Its answers are on screen: an approval's request card arrived; a question always is.
    var ready: Bool
}

/// When the chat scrolls on its own, worked out from plain values so it can be tested. It
/// scrolls only for what needs the user or what the user just did: a needs card arriving or
/// growing its answers, the user's own send and the live line that follows it. Never while
/// the user's finger is on the chat.
enum ChatScroll {
    enum Target: Equatable {
        case row(String)
        case bottom
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
    /// answers just landed (waiting → card). Nil while dragging, or when nothing new came.
    static func target(old: [NeedsMark], new: [NeedsMark], dragging: Bool) -> String? {
        guard !dragging else { return nil }
        let before = Dictionary(old.map { ($0.id, $0.ready) }, uniquingKeysWith: { first, _ in first })
        return new.last { mark in
            guard let wasReady = before[mark.id] else { return true }
            return !wasReady && mark.ready
        }?.id
    }

    /// The user's own send, or the live line's first appearance after it, scrolls to the foot.
    static func followsSend(dragging: Bool) -> Target? {
        dragging ? nil : .bottom
    }

    static func liveAppeared(was: Bool, now: Bool, afterSend: Bool, dragging: Bool) -> Target? {
        !was && now && afterSend && !dragging ? .bottom : nil
    }

    /// What the chat's scroll view reports: whether it is at the foot, and its height.
    struct Frame: Equatable {
        var pinned: Bool
        var height: Int
    }

    /// The scroll view changed height (the keyboard came or went) while the reader was at the
    /// foot and not dragging: it returns to the foot. A scroll alone, at the same height, never
    /// pulls the reader down.
    static func repinsOnResize(old: Frame, new: Frame, dragging: Bool) -> Bool {
        old.pinned && old.height != new.height && !dragging
    }

    /// At the foot, within a few points: the last line shows above the composer. The visible
    /// region starts below the top inset (the tab strip's bar the chat scrolls under).
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

/// What the chat's scrolling keeps between renders. It never holds the scroll view's own
/// `ScrollPosition`: bound to an observed property, the scroll view wrote it while committing a
/// scroll, the write invalidated the modifier that binds it, the binding re-applied it, and the
/// scroll view committed again, forever (a hard freeze on the Terminal toggle, and on relaunch).
/// The position lives in `NeedsScrolling`'s own state; this keeps requests going in and the
/// settled row coming out.
@MainActor @Observable final class ChatScrollModel {
    /// A scroll asked of the chat. `seed` tells two asks for the same place apart.
    struct Request: Equatable {
        var target: ChatScroll.Target
        var reduce: Bool
        var seed: Int
    }

    /// The latest scroll asked for; the chat applies it to its own position.
    private(set) var request: Request?
    /// Needs cards on screen now.
    private(set) var visibleNeeds: Set<String> = []
    /// The card the capsule offers, once it has stayed off screen a moment.
    var capsule: String?
    @ObservationIgnored var dragging = false
    @ObservationIgnored var pinned = true
    /// The row at the chat's bottom edge when the reader's last scroll came to rest, so a
    /// rebuilt chat returns to it. Unobserved: nothing draws from it.
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

    /// The reader's scroll came to rest with `row` at the bottom edge. Writes only a change.
    /// Returns whether it changed.
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

/// Scrolls a conversation to a needs card when one arrives or its answers land, keeps track
/// of the user's finger, and keeps a reader at the foot at the foot when the chat changes
/// height. The main chat and a subagent's chat both use it.
///
/// The scroll position is this modifier's own state: only the scroll view, the model's
/// requests and the one restore write it, and the model hears of it only when a scroll comes
/// to rest, and only if the row changed.
struct NeedsScrolling: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let marks: [NeedsMark]
    /// Arrivals before the first load are not arrivals.
    let loaded: Bool
    let scroll: ChatScrollModel
    /// Whether a row is in the chat, for returning to the reader's place once. Nil: nothing to return to.
    var has: ((String) -> Bool)?

    @State private var position = ScrollPosition(edge: .bottom)
    @State private var restored = false

    func body(content: Content) -> some View {
        content
            // The position is the row at the chat's bottom edge, and that row is what a size
            // change keeps in place: the latest lines stay right above the composer as it grows
            // or shrinks and as the keyboard comes or goes. Anchored at the top (the default),
            // the top row stayed put and the latest lines slid under the composer.
            .scrollPosition($position, anchor: .bottom)
            .onScrollPhaseChange { old, phase in
                let dragging = phase == .interacting || phase == .tracking
                if scroll.dragging != dragging { scroll.dragging = dragging }
                // Once a scroll has come to rest, never while it moves; an unobserved write.
                if phase == .idle, old != .idle {
                    scroll.settle(at: position.viewID(type: String.self))
                }
            }
            .onScrollGeometryChange(for: ChatScroll.Frame.self) { geometry in
                ChatScroll.Frame(
                    pinned: ChatScroll.isPinned(
                        offset: geometry.contentOffset.y, topInset: geometry.contentInsets.top,
                        container: geometry.containerSize.height, content: geometry.contentSize.height
                    ),
                    height: Int(geometry.containerSize.height.rounded())
                )
            } action: { old, new in
                // The keyboard (or a fold, or the composer growing) shrank or grew the chat: a
                // reader at the foot stays at the foot, the latest line right above the composer.
                // The bottom anchor already keeps it there; this only lands a reader whose foot
                // was reached by a scroll the anchor has not caught up with.
                if ChatScroll.repinsOnResize(old: old, new: new, dragging: scroll.dragging) {
                    move(.bottom, reduce: true)
                    if !scroll.pinned { scroll.pinned = true }
                } else if scroll.pinned != new.pinned {
                    scroll.pinned = new.pinned
                }
            }
            .onChange(of: scroll.request) { _, request in
                guard let request else { return }
                move(request.target, reduce: request.reduce)
            }
            // A chat made again returns to the reader's row once, after its first layout; a
            // row that has gone sends it to the foot.
            .task {
                guard !restored else { return }
                restored = true
                guard let has else { return }
                await Task.yield()
                guard !scroll.dragging, let target = scroll.restoreTarget(has: has) else { return }
                move(target, reduce: true)
            }
            .onChange(of: marks) { old, new in
                guard loaded, let id = ChatScroll.target(old: old, new: new, dragging: scroll.dragging) else { return }
                scroll.go(.row(id), reduce: reduceMotion, settle: true)
            }
            // A field a card just revealed rises above the composer, and once more after the
            // keyboard has come up (`settle`).
            .environment(\.revealRow, RevealRowAction { [reduceMotion, scroll] id in
                scroll.go(.row(id), reduce: reduceMotion, settle: true)
            })
    }

    private func move(_ target: ChatScroll.Target, reduce: Bool) {
        let apply = {
            switch target {
            case .row(let id): position.scrollTo(id: id, anchor: .bottom)
            case .bottom: position.scrollTo(edge: .bottom)
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
/// request", the composer's height kept clear, and the "Needs you" capsule while a card is off
/// screen.
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
                guard let target = ChatScroll.followsSend(dragging: scroll.dragging) else { return }
                scroll.go(target, reduce: reduceMotion)
            }
            .onChange(of: store.scrollRequest) { _, request in
                guard let request else { return }
                scroll.go(.row(request.row), reduce: reduceMotion, settle: true)
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

extension View {
    /// Reports whether a needs card is on screen, for the "Needs you" capsule.
    func needsVisibility(_ id: String, in scroll: ChatScrollModel) -> some View {
        onScrollVisibilityChange(threshold: 0.25) { visible in scroll.setVisible(id, visible) }
    }
}
