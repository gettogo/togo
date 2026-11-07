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

    /// The composer changed height while the chat sat at its foot: it stays there.
    static func repins(pinned: Bool, old: CGFloat, new: CGFloat, dragging: Bool) -> Bool {
        pinned && old != new && !dragging
    }

    /// At the foot, within a few points.
    static func isPinned(visibleMaxY: CGFloat, content: CGFloat) -> Bool {
        visibleMaxY >= content - 8
    }

    /// The card the "Needs you" capsule jumps to: the oldest open card off screen. Nothing
    /// before the first load, and nothing while every open card is visible.
    static func capsule(open: [NeedsMark], visible: Set<String>, loaded: Bool) -> String? {
        guard loaded else { return nil }
        return open.first { !visible.contains($0.id) }?.id
    }
}

/// What the chat's scrolling keeps between renders.
@MainActor @Observable final class ChatScrollModel {
    var position = ScrollPosition(edge: .bottom)
    /// Needs cards on screen now.
    private(set) var visibleNeeds: Set<String> = []
    /// The card the capsule offers, once it has stayed off screen a moment.
    var capsule: String?
    @ObservationIgnored var dragging = false
    @ObservationIgnored var pinned = true

    /// Scrolls on `resize`, the same role that grows a card, so the chat makes room and brings
    /// the card up as one movement. A card still growing is followed once more when it has
    /// grown (`settle`), unless the user has taken the scroll meanwhile.
    func go(_ target: ChatScroll.Target, reduce: Bool, settle: Bool = false) {
        perform(target, reduce: reduce)
        guard settle else { return }
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: .milliseconds(400))
            guard let self, !self.dragging else { return }
            self.perform(target, reduce: reduce)
        }
    }

    private func perform(_ target: ChatScroll.Target, reduce: Bool) {
        let move = { [self] in
            switch target {
            case .row(let id): position.scrollTo(id: id, anchor: .bottom)
            case .bottom: position.scrollTo(edge: .bottom)
            }
        }
        guard !reduce else {
            var transaction = Transaction(animation: nil)
            transaction.disablesAnimations = true
            withTransaction(transaction, move)
            return
        }
        // motion: preventing a jump — the chat glides to the card that needs you (or to your
        // own prompt) instead of teleporting; under Reduce Motion it lands at once.
        withAnimation(Motion.animation(.resize, reduce: false), move)
    }

    func setVisible(_ id: String, _ visible: Bool) {
        if visible {
            if !visibleNeeds.contains(id) { visibleNeeds.insert(id) }
        } else if visibleNeeds.contains(id) {
            visibleNeeds.remove(id)
        }
    }
}

/// Scrolls a conversation to a needs card when one arrives or its answers land, and keeps
/// track of the user's finger. The main chat and a subagent's chat both use it.
struct NeedsScrolling: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let marks: [NeedsMark]
    /// Arrivals before the first load are not arrivals.
    let loaded: Bool
    let scroll: ChatScrollModel

    func body(content: Content) -> some View {
        @Bindable var scroll = scroll
        content
            .scrollPosition($scroll.position)
            .onScrollPhaseChange { _, phase in
                scroll.dragging = phase == .interacting || phase == .tracking
            }
            .onChange(of: marks) { old, new in
                guard loaded, let id = ChatScroll.target(old: old, new: new, dragging: scroll.dragging) else { return }
                scroll.go(.row(id), reduce: reduceMotion, settle: true)
            }
            // A field a card just revealed rises above the composer, and once more after the
            // keyboard has come up (`settle`).
            .environment(\.revealRow, RevealRowAction { [reduceMotion] id in
                scroll.go(.row(id), reduce: reduceMotion, settle: true)
            })
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

    func body(content: Content) -> some View {
        let candidate = ChatScroll.capsule(open: marks, visible: scroll.visibleNeeds, loaded: store.hasLoaded)
        content
            .modifier(NeedsScrolling(marks: marks, loaded: store.hasLoaded, scroll: scroll))
            .onScrollGeometryChange(for: Bool.self) { geometry in
                ChatScroll.isPinned(visibleMaxY: geometry.visibleRect.maxY, content: geometry.contentSize.height)
            } action: { _, pinned in
                scroll.pinned = pinned
            }
            // The scroll view itself keeps the composer's height clear, so the last card is
            // never under it, whatever the keyboard does. The capsule sits just above it.
            .safeAreaInset(edge: .bottom, spacing: 0) {
                Color.clear
                    .frame(height: store.composerClearance)
                    .accessibilityHidden(true)
                    .overlay(alignment: .top) {
                        if let target = scroll.capsule {
                            // motion: attention — fades in place, never rises (timed by the task below).
                            NeedsJumpPill { store.revealNeeds(id: target) }
                                .alignmentGuide(.top) { $0[.bottom] + 10 }
                                .transition(.opacity)
                        }
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
            .onChange(of: store.composerClearance) { old, new in
                guard ChatScroll.repins(pinned: scroll.pinned, old: old, new: new, dragging: scroll.dragging) else { return }
                var transaction = Transaction(animation: nil)
                transaction.disablesAnimations = true
                withTransaction(transaction) { scroll.position.scrollTo(edge: .bottom) }
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
