import SwiftUI
import TogoKit

/// One session: bib and name with its status, what the agent is doing, and where.
/// `stale` rows belong to a machine that is away; they keep their place but read as old.
struct SessionRowView: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let row: SessionRow
    var stale = false
    /// The row's tab is on screen beside the sidebar.
    var shown = false

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
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
            if let activity = row.activity, !activity.isEmpty {
                Text(activity)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? 2 : 1)
            }
            PathText(row.cwd)
        }
        .padding(.vertical, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spokenLabel)
        .accessibilityValue(row.activity ?? "")
        .accessibilityAddTraits(shown ? .isSelected : [])
    }

    private var title: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            if let bib = row.bib {
                BibText(bib, style: .body)
            }
            Text(verbatim: displayName)
                .font(.body)
                .foregroundStyle(stale ? Color.togoMute : Color.togoSnow)
                .lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 1)
        }
    }

    /// At accessibility sizes "Muted" takes its own line, so neither word is cut short.
    @ViewBuilder private var status: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 2) {
                statusWord
                if row.muted {
                    mutedWord
                }
            }
        } else {
            HStack(alignment: .firstTextBaseline, spacing: 0) {
                statusWord
                if row.muted {
                    Text(verbatim: " · ")
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                    mutedWord
                }
            }
            .fixedSize()
        }
    }

    /// A stale "Needs you" is a last-known state, not a live call for attention, so it dims
    /// with the rest of the row.
    @ViewBuilder private var statusWord: some View {
        if stale {
            Text(StatusWord.label(row.status))
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .lineLimit(1)
        } else {
            StatusWord(status: row.status)
        }
    }

    private var mutedWord: some View {
        Text(ControlCenterCopy.muted)
            .font(.subheadline)
            .foregroundStyle(Color.togoMute)
            .lineLimit(1)
    }

    private var displayName: String {
        row.name.isEmpty ? row.agentName : row.name
    }

    private var spokenLabel: String {
        row.muted ? "\(row.spokenSummary), \(ControlCenterCopy.muted)" : row.spokenSummary
    }
}
