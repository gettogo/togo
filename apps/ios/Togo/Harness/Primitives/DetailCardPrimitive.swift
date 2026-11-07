import SwiftUI
import TogoKit

/// One thing in full: a card with a subtitle, a body (markdown, a diff, label and value pairs or
/// plain machine text) and its actions as buttons under it. A diff or text body sits in a
/// deepest well inside the card.
struct DetailCardPrimitive: View {
    let detail: HarnessScreen.Detail
    let context: PrimitiveContext
    /// Drawn inside another scroll view: no scroll of its own.
    var embedded = false
    @State private var actions = PrimitiveActions()

    var body: some View {
        Group {
            if embedded {
                card
            } else {
                ScrollView { card.padding(.horizontal, 12).padding(.vertical, 12) }
                    .background(Color.togoInk)
            }
        }
        .primitiveActions(actions, context: context)
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 16) {
            if let subtitle = detail.subtitle, !subtitle.isEmpty {
                Text(verbatim: subtitle)
                    .font(.mono(.footnote))
                    .foregroundStyle(Color.togoMute)
                    .textSelection(.enabled)
            }
            DetailBody(content: detail.body)
            if !detail.actions.isEmpty {
                PrimitiveActionRow(refs: DetailCardLogic.ordered(detail.actions), large: true, actions: actions, context: context)
            }
        }
        .primitiveCard(radius: 24, padding: 18)
    }
}

enum DetailCardLogic {
    enum BodyKind: Equatable { case markdown, diff, kv, text, empty }

    /// The body's kind, falling back to text for a kind this build does not know and to empty
    /// when there is nothing to draw.
    static func kind(_ body: HarnessScreen.Detail.Body) -> BodyKind {
        switch body.kind {
        case "kv": return (body.kv ?? []).isEmpty ? .empty : .kv
        case "diff": return (body.text ?? "").isEmpty ? .empty : .diff
        case "markdown": return (body.text ?? "").isEmpty ? .empty : .markdown
        default: return (body.text ?? "").isEmpty ? .empty : .text
        }
    }

    /// The primary action last, nearest the thumb, and destructive ones first, away from it.
    static func ordered(_ refs: [HarnessActionRef]) -> [HarnessActionRef] {
        refs.filter { $0.role == .destructive } + refs.filter { $0.role != .primary && $0.role != .destructive }
            + refs.filter { $0.role == .primary }
    }
}

private struct DetailBody: View {
    let content: HarnessScreen.Detail.Body

    var body: some View {
        switch DetailCardLogic.kind(content) {
        case .markdown:
            PrimitiveMarkdown(content.text ?? "")
        case .diff:
            PrimitiveDiff(content.text ?? "")
        case .kv:
            VStack(spacing: 0) {
                ForEach(Array((content.kv ?? []).enumerated()), id: \.offset) { index, fact in
                    if index > 0 { Rectangle().fill(Color.togoLine).frame(height: 1) }
                    ViewThatFits(in: .horizontal) {
                        HStack(alignment: .firstTextBaseline, spacing: 12) {
                            Text(fact.label).foregroundStyle(Color.togoMute)
                            Spacer(minLength: 12)
                            value(fact.value).multilineTextAlignment(.trailing)
                        }
                        VStack(alignment: .leading, spacing: 2) {
                            Text(fact.label).foregroundStyle(Color.togoMute)
                            value(fact.value)
                        }
                    }
                    .font(.subheadline)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                    .frame(minHeight: 44)
                    .accessibilityElement(children: .combine)
                }
            }
            .background(Color.togoGrouped, in: .rect(cornerRadius: 14, style: .continuous))
        case .text:
            Text(verbatim: content.text ?? "")
                .font(.mono(.footnote))
                .foregroundStyle(Color.togoSnow)
                .textSelection(.enabled)
                .primitiveWell()
        case .empty:
            Text(PrimitiveCopy.noBody).font(.body).foregroundStyle(Color.togoMute)
        }
    }

    private func value(_ text: String) -> some View {
        Text(verbatim: text)
            .font(.mono(.subheadline))
            .foregroundStyle(Color.togoSnow)
            .textSelection(.enabled)
    }
}
