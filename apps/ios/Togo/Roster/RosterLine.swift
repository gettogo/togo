import SwiftUI
import TogoKit

/// One session on one line: the mono bib, the name with its agent in mute, the status on the
/// right, and under the name the agent's last action in mono with its activity at the end.
/// `stale` lines belong to a machine that is away; they keep their place but read as old.
struct RosterLine: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .subheadline) private var bibWidth: CGFloat = 24
    let line: RosterModel.Line
    var stale = false
    /// The machine is still connecting, so the line's last state is not known to be live.
    var connecting = false
    /// Drawn on the raised surface; the one line whose spinner turns.
    var selected = false

    private var row: SessionRow { line.row }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(verbatim: row.bib ?? "")
                .font(.system(.footnote, design: .monospaced))
                .monospacedDigit()
                .foregroundStyle(Color.togoMute)
                .lineLimit(1)
                .frame(width: bibWidth, alignment: .leading)
            VStack(alignment: .leading, spacing: 2) {
                if dynamicTypeSize.isAccessibilitySize {
                    title
                    status
                } else {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        title
                        Spacer(minLength: 8)
                        status
                    }
                }
                lastAction
            }
        }
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spokenLabel)
        .accessibilityValue(row.activity ?? "")
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
    }

    private var title: some View {
        let name = Text(verbatim: displayName)
            .font(.body.weight(.medium))
            .foregroundStyle(stale ? Color.togoMute : Color.togoSnow)
        guard showsAgent else {
            return name.lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 1)
        }
        // The agent's mark leads its name, in the same mute as the name.
        let agent = Text("  \(AgentGlyph.text(row.agent)) \(row.agentName)")
            .font(.subheadline)
            .foregroundStyle(Color.togoMute)
        return Text("\(name)\(agent)")
            .lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 1)
    }

    /// A stale "Needs you" is a last-known state, not a live call, so it never lights.
    @ViewBuilder private var status: some View {
        if stale {
            Text(StatusWord.label(row.status, exit: row.exit))
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .lineLimit(1)
                .fixedSize()
        } else if connecting && !line.needsYou {
            RosterStatus(connecting: ControlCenterCopy.connecting)
                .fixedSize()
        } else {
            RosterStatus(status: line.status, exit: row.exit, animated: selected)
                .fixedSize()
        }
    }

    /// The agent's own words, in the terminal's font, with its activity at the trailing end.
    private var lastAction: some View {
        HStack(alignment: .center, spacing: 8) {
            Text(verbatim: row.activity.flatMap { $0.isEmpty ? nil : $0 } ?? PathText.abbreviate(row.cwd))
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(Color.togoMute)
                .lineLimit(dynamicTypeSize.isAccessibilitySize ? 2 : 1)
                .truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)
            ActivityBars(samples: row.activityHistory, live: row.status == .working && !stale)
                .accessibilityHidden(true)
        }
    }

    private var displayName: String {
        row.name.isEmpty ? row.agentName : row.name
    }

    /// A session named after its agent says the agent once.
    private var showsAgent: Bool {
        !row.name.isEmpty && !row.agentName.isEmpty && row.name.caseInsensitiveCompare(row.agentName) != .orderedSame
    }

    private var spokenLabel: String {
        var parts = [row.bib, displayName, showsAgent ? row.agentName : nil]
        parts.append(StatusWord.label(line.status, exit: row.exit))
        if row.muted { parts.append(ControlCenterCopy.muted) }
        return parts.compactMap { $0 }.joined(separator: ", ")
    }
}
