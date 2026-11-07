import SwiftUI
import TogoKit

/// A section's header: its symbol and name, the row count in mono, and the beacon on Needs
/// you while anything waits. A header to VoiceOver.
struct MissionSectionHeader: View {
    @ScaledMetric(relativeTo: .footnote) private var mark: CGFloat = 16
    let kind: MissionSectionKind
    let count: Int

    private var lit: Bool { kind == .needs && count > 0 }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            // One fixed slot, so the beacon and the symbol crossfade and the name never moves.
            ZStack {
                if lit {
                    BeaconDot()
                        .transition(.opacity)
                } else {
                    Image(systemName: MissionCopy.symbol(kind))
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Color.togoMute)
                        .accessibilityHidden(true)
                        .transition(.opacity)
                }
            }
            .slot(width: mark, height: mark, key: lit)
            .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
            Text(MissionCopy.header(kind))
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
            if count > 0, kind != .status {
                Text(verbatim: "\(count)")
                    .font(.mono(.caption))
                    .foregroundStyle(Color.togoMute)
                    .rollingNumber(Double(count))
                    .transition(.opacity)
            }
            Spacer(minLength: 0)
        }
        .motion(.respond, value: count > 0)
        .padding(.horizontal, 4)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

/// Rows on the raised surface, one hairline between them.
/// Rows keep their own ids (the subview ids of the `ForEach` inside), so a row that arrives or
/// leaves is that row, never its neighbour's content morphing into it. The card clips, so a row
/// concealing while the card shrinks never draws outside it.
struct MissionCard<Content: View>: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ViewBuilder let content: Content

    var body: some View {
        VStack(spacing: 0) {
            Group(subviews: content) { rows in
                ForEach(rows) { row in
                    VStack(spacing: 0) {
                        if row.id != rows.first?.id {
                            Rectangle().fill(Color.togoLine).frame(height: 1).padding(.leading, 44)
                        }
                        row
                    }
                    .transition(Motion.revealConceal(reduce: reduceMotion))
                }
            }
        }
        .background(Color.togoRaised, in: .rect(cornerRadius: 18, style: .continuous))
        .clipShape(.rect(cornerRadius: 18, style: .continuous))
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
    @Environment(\.dynamicTypeSize) private var typeSize
    let row: HarnessRow
    var opens = false
    /// What VoiceOver says the row opens.
    var openLabel: String?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            PrimitiveStateGlyph(state: row.state, font: .body)
                .frame(width: 20)
            VStack(alignment: .leading, spacing: 3) {
                // The timer moves under the title at accessibility sizes so the title keeps the width.
                let layout = typeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: 3))
                    : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 8))
                layout {
                    Text(verbatim: row.title)
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                        .fixedSize(horizontal: false, vertical: true)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if let start = row.startedAt?.date, row.state.isLive {
                        MissionClock(start: start)
                    }
                }
                if let subtitle = row.subtitle, !subtitle.isEmpty {
                    Text(verbatim: subtitle)
                        .font(.mono(.caption))
                        .foregroundStyle(Color.togoMute)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if let progress = row.progress {
                    ProgressView(value: min(max(progress, 0), 1))
                        .tint(Color.togoSnow)
                        .padding(.top, 2)
                        .motion(.progress, value: progress)
                        .transition(.opacity)
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

/// How long a live row has run, ticking once a second; each tick rolls its digits (motion
/// spec §2.7).
struct MissionClock: View {
    let start: Date

    var body: some View {
        TimelineView(.periodic(from: start, by: 1)) { context in
            let seconds = max(0, context.date.timeIntervalSince(start)).rounded(.down)
            Text(verbatim: BackgroundLabel.timer(seconds))
                .font(.mono(.caption))
                .foregroundStyle(Color.togoMute)
                // trunc-ok: a timer, at most 8 characters
                .lineLimit(1)
                .fixedSize()
                .rollingNumber(seconds)
        }
    }
}

/// A background job: a short name for it, then the last line it printed and how long it has
/// run, ticking while it runs. The full command is in its accessibility label and one tap away
/// in the live terminal.
struct MissionBackgroundRow: View {
    @Environment(\.dynamicTypeSize) private var typeSize
    let row: HarnessRow

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            PrimitiveStateGlyph(state: row.state, font: .body)
                .frame(width: 20)
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: BackgroundLabel.title(row))
                    .font(.body)
                    .foregroundStyle(Color.togoSnow)
                    .fixedSize(horizontal: false, vertical: true)
                if row.state.isLive {
                    TimelineView(.periodic(from: .now, by: 1)) { context in subtitle(at: context.date) }
                } else {
                    subtitle(at: .now)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Image(systemName: "chevron.right")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoMuteDim)
                .accessibilityHidden(true)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
        .accessibilityHint(MissionCopy.openTerminal)
        .help(row.subtitle ?? row.title)
    }

    /// The last output line, middle-cut, with the time beside it or under it at large sizes,
    /// so the clock is never the part that gets cut.
    @ViewBuilder private func subtitle(at now: Date) -> some View {
        let last = BackgroundLabel.lastLine(row)
        let clock = BackgroundLabel.clock(row, now: now)
        if last != nil || clock != nil {
            let layout = typeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 2))
                : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 6))
            layout {
                if let last {
                    Text(verbatim: last)
                        // trunc-ok: live output, middle-cut so its start and end show; the row
                        // opens the live terminal and the label reads it in full.
                        .lineLimit(2)
                        .truncationMode(.middle)
                }
                if let clock {
                    Text(verbatim: clock)
                        .fixedSize()
                        .rollingNumber(row.startedAt.map { now.timeIntervalSince($0.date).rounded(.down) } ?? 0)
                }
            }
            .font(.mono(.caption))
            .monospacedDigit()
            .foregroundStyle(Color.togoMute)
        }
    }

    private var spoken: String {
        let last = (row.tail ?? []).last { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        return [BackgroundLabel.title(row), PrimitiveStateLook(row.state).label, row.subtitle, last]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
            .joined(separator: ", ")
    }
}

/// Something that needs the user, answered right here: the chat's own approval or question
/// row when the item is loaded, else its title under the beacon with a way to the chat.
struct MissionNeedsRow: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let row: HarnessRow
    let store: HarnessSessionStore
    let actions: ChatActions
    let onShowChat: () -> Void
    /// Whether the answerable row shows: it follows the item loading (network) through the
    /// swap choreography, so the plain line conceals before the card grows in (spec §2.3).
    @State private var loaded: Mirrored<Bool>

    init(row: HarnessRow, store: HarnessSessionStore, actions: ChatActions, onShowChat: @escaping () -> Void) {
        self.row = row
        self.store = store
        self.actions = actions
        self.onShowChat = onShowChat
        _loaded = State(initialValue: Mirrored(Self.answerable(row, store)))
    }

    private static func answerable(_ row: HarnessRow, _ store: HarnessSessionStore) -> Bool {
        guard let item = MissionLayout.needsItem(for: row, in: store.timeline) else { return false }
        switch item.body {
        case .approval, .question: return true
        default: return false
        }
    }

    var body: some View {
        Group {
            if loaded.value, let item = MissionLayout.needsItem(for: row, in: store.timeline) {
                answer(item)
            } else {
                plain
            }
        }
        .settling(loaded)
        .onChange(of: Self.answerable(row, store)) { _, now in
            Motion.settle($loaded, to: now, reduce: reduceMotion)
        }
    }

    @ViewBuilder private func answer(_ item: HarnessItem) -> some View {
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
                        .fixedSize(horizontal: false, vertical: true)
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
