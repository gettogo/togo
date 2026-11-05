import SwiftUI
import TogoKit

/// A request from another session, dropped in above the key bar. Only the oldest shows; the
/// rest are a count that leads to the full list. An answered card shrinks into its session's
/// tab chip, so the user sees where the answer went.
struct IncomingRequests: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// `cards` as last animated. Arrivals drop in and answers collapse, so each change picks
    /// its own curve.
    @State private var shown: [RequestCard]?

    let cards: [RequestCard]
    let failures: [String: String]
    /// Sessions with a tab chip to shrink into.
    let chipIDs: Set<TabID>
    let namespace: Namespace.ID
    let machineName: (HostID) -> String
    let onOpen: (RequestCard) -> Void
    let onAnswer: (RequestCard, Decision) -> Void
    let onMore: () -> Void

    var body: some View {
        let cards = shown ?? cards
        VStack(alignment: .trailing, spacing: 6) {
            if let card = cards.first {
                RequestCardView(
                    card: card, machineName: machineName(card.hostID), failure: failures[card.id],
                    onOpen: card.sessionID == nil ? nil : { onOpen(card) }
                ) { onAnswer(card, $0) }
                .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(Color.togoLine))
                .id(card.id)
                .transition(transition(for: card))
            }
            if cards.count > 1 {
                Button(WorkspaceCopy.more(cards.count - 1), action: onMore)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.togoSnow)
                    .padding(.horizontal, 14)
                    .frame(minHeight: 44)
                    .background(Color.togoInk, in: .capsule)
                    .contentShape(.capsule)
                    .accessibilityHint(WorkspaceCopy.moreHint)
                    .transition(.opacity)
            }
        }
        .onChange(of: self.cards, initial: true) { old, new in
            let answered = old.first.map { first in first.isAnswered && !new.contains { $0.id == first.id } } ?? false
            withAnimation(Motion.motion(answered ? Motion.collapse : Motion.dropIn, reduce: reduceMotion)) {
                shown = new
            }
        }
    }

    private func transition(for card: RequestCard) -> AnyTransition {
        if reduceMotion {
            return .opacity
        }
        // A card that leaves because its own tab came on screen was not answered, so it fades
        // rather than shrinking into the chip as if it had been.
        let dropIn = AnyTransition.offset(y: -28).combined(with: .opacity)
        guard card.isAnswered else { return .asymmetric(insertion: dropIn, removal: .opacity) }
        let chip = card.tabID.flatMap { chipIDs.contains($0) ? $0 : nil }
        return .asymmetric(
            insertion: dropIn,
            removal: .modifier(
                active: ShrinkIntoChip(chip: chip, namespace: namespace, active: true),
                identity: ShrinkIntoChip(chip: chip, namespace: namespace, active: false)
            )
        )
    }
}

/// Moves a leaving card onto its tab chip while it scales down and fades. Without a chip it
/// shrinks where it is.
private struct ShrinkIntoChip: ViewModifier {
    let chip: TabID?
    let namespace: Namespace.ID
    let active: Bool

    func body(content: Content) -> some View {
        content
            .matchedGeometryEffect(
                id: chip, in: namespace, properties: active && chip != nil ? .position : [], anchor: .center,
                isSource: false
            )
            .scaleEffect(active ? 0.12 : 1, anchor: .center)
            .opacity(active ? 0 : 1)
    }
}

private extension RequestCard {
    /// An answer is on its way or has landed. The store drops a card from the incoming list
    /// the moment it lands, so `.sending` is the last phase the view sees.
    var isAnswered: Bool {
        switch phase {
        case .open: false
        case .sending, .done: true
        }
    }
}
