import SwiftUI
import TogoKit

/// A section's header: its symbol and name, the row count in mono, and the beacon on Needs
/// you while anything waits. A header to VoiceOver.
struct MissionSectionHeader: View {
    let kind: MissionSectionKind
    let count: Int

    private var lit: Bool { kind == .needs && count > 0 }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            if lit {
                BeaconDot()
                    .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
            } else {
                Image(systemName: MissionCopy.symbol(kind))
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoMute)
                    .accessibilityHidden(true)
            }
            Text(MissionCopy.header(kind))
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
            if count > 0, kind != .status {
                Text(verbatim: "\(count)")
                    .font(.mono(.caption))
                    .monospacedDigit()
                    .foregroundStyle(Color.togoMute)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 4)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

/// Rows on the raised surface, one hairline between them.
struct MissionCard<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        VStack(spacing: 0) {
            Group(subviews: content) { rows in
                ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                    if index > 0 {
                        Rectangle().fill(Color.togoLine).frame(height: 1).padding(.leading, 44)
                    }
                    row
                }
            }
        }
        .background(Color.togoRaised, in: .rect(cornerRadius: 18, style: .continuous))
    }
}

/// One quiet line where a supported section has nothing in it.
struct MissionEmptyLine: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.subheadline)
            .foregroundStyle(Color.togoMute)
            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
            .padding(.horizontal, 14)
            .background(Color.togoRaised, in: .rect(cornerRadius: 18, style: .continuous))
    }
}

/// An agent, a change or a port: its state, title and subtitle, how long it has run, its
/// progress when known, and a chevron when it opens somewhere.
struct MissionRowView: View {
    let row: HarnessRow
    var opens = false
    /// What VoiceOver says the row opens.
    var openLabel: String?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            PrimitiveStateGlyph(state: row.state, font: .body)
                .frame(width: 20)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(verbatim: row.title)
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                        .lineLimit(2)
                    Spacer(minLength: 4)
                    if let start = row.startedAt?.date, row.state.isLive {
                        Text(start, style: .timer)
                            .font(.mono(.caption))
                            .monospacedDigit()
                            .foregroundStyle(Color.togoMute)
                            .lineLimit(1)
                            .fixedSize()
                    }
                }
                if let subtitle = row.subtitle, !subtitle.isEmpty {
                    Text(verbatim: subtitle)
                        .font(.mono(.caption))
                        .foregroundStyle(Color.togoMute)
                        .lineLimit(2)
                }
                if let progress = row.progress {
                    ProgressView(value: min(max(progress, 0), 1))
                        .tint(Color.togoSnow)
                        .padding(.top, 2)
                }
            }
            if opens {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoMuteDim)
                    .accessibilityHidden(true)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
        .accessibilityHint(openLabel ?? "")
    }

    private var spoken: String {
        [row.title, PrimitiveStateLook(row.state).label, row.subtitle]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
            .joined(separator: ", ")
    }
}

/// A background job with its last lines of output in a mono well, newest last.
struct MissionTailRow: View {
    let row: HarnessRow

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            MissionRowView(row: row, opens: true, openLabel: MissionCopy.openTerminal)
                .padding(.bottom, tail.isEmpty ? 0 : -8)
            if !tail.isEmpty {
                VStack(alignment: .leading, spacing: 1) {
                    ForEach(Array(tail.enumerated()), id: \.offset) { index, line in
                        Text(verbatim: line.isEmpty ? " " : line)
                            .font(.mono(.caption2))
                            .foregroundStyle(index == tail.count - 1 ? Color.togoSnow : Color.togoMute)
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 10)
                .padding(.vertical, 8)
                .background(Color.togoDeepest, in: .rect(cornerRadius: 10, style: .continuous))
                .padding(.leading, 46)
                .padding(.trailing, 14)
                .padding(.bottom, 12)
                .accessibilityElement(children: .combine)
                .accessibilityLabel(tail.joined(separator: "\n"))
            }
        }
        .contentShape(.rect)
    }

    /// At most four lines, as the helper promises.
    private var tail: [String] { Array((row.tail ?? []).suffix(4)) }
}

/// Something that needs the user, answered right here: the chat's own approval or question
/// row when the item is loaded, else its title under the beacon with a way to the chat.
struct MissionNeedsRow: View {
    let row: HarnessRow
    let store: HarnessSessionStore
    let actions: ChatActions
    let onShowChat: () -> Void

    var body: some View {
        if let item = MissionLayout.needsItem(for: row, in: store.timeline) {
            switch item.body {
            case .approval(let approval):
                ApprovalRow(item: item, approval: approval, card: actions.card(approval.requestId)) { decision in
                    actions.answer(approval.requestId, decision)
                }
                .padding(14)
            case .question(let question):
                QuestionRow(item: item, question: question) { answers in
                    guard let result = try? await store.answer(item, answers: answers) else { return false }
                    return result.result != .failed
                }
                .padding(14)
            default:
                plain
            }
        } else {
            plain
        }
    }

    private var plain: some View {
        Button(action: onShowChat) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                BeaconDot()
                    .frame(width: 20)
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: row.title)
                        .font(.body.weight(.medium))
                        .foregroundStyle(Color.togoSnow)
                        .lineLimit(3)
                    Text(row.subtitle ?? MissionCopy.answerInChat)
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(MissionCopy.needs(1) + ", " + row.title)
        .accessibilityHint(MissionCopy.answerInChat)
    }
}
