import SwiftUI
import TogoKit

/// One session on the board: bib, name and status, what the agent is doing, the machine, and
/// the last lines of its screen. A machine that cannot send its screen shows the current action
/// in that place instead.
struct SessionCard: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let card: BoardCard
    /// The last lines of the screen; nil until the first capture arrives.
    var thumbnail: String?
    /// The machine can send its screen.
    var capturing = true
    /// The card's tab is on screen in the workspace.
    var shown = false

    private var row: SessionRow { card.row }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            header
            if capturing, let activity = row.activity, !activity.isEmpty {
                Text(activity)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 1)
            }
            screen
            machine
        }
        .padding(14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Color.togoGrouped, in: .rect(cornerRadius: 16, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(shown ? Color.togoSnow : Color.togoLine, lineWidth: shown ? 2 : 1)
        }
        .contentShape(.rect(cornerRadius: 16, style: .continuous))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(row.spokenSummary)
        .accessibilityValue([row.activity, card.machine].compactMap(\.self).joined(separator: ", "))
        .accessibilityAddTraits(shown ? [.isButton, .isSelected] : .isButton)
    }

    // MARK: Parts

    @ViewBuilder private var header: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 4) {
                title
                StatusWord(status: row.status)
            }
        } else {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                title
                Spacer(minLength: 8)
                StatusWord(status: row.status)
            }
        }
    }

    private var title: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            if let bib = row.bib {
                BibText(bib, style: .headline)
            }
            Text(verbatim: row.name.isEmpty ? row.agentName : row.name)
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                .lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 1)
        }
    }

    private var machine: some View {
        Label {
            Text(verbatim: card.machine)
                .lineLimit(1)
        } icon: {
            Image(systemName: card.symbol.systemImage)
        }
        .font(.footnote)
        .foregroundStyle(Color.togoMute)
    }

    /// The screen, as small as it takes for 80 columns to fit, always 12 lines tall so a card
    /// keeps its size from the first capture on.
    @ViewBuilder private var screen: some View {
        if capturing {
            ThumbnailScreen(text: thumbnail ?? "")
        } else {
            Text(row.activity.flatMap { $0.isEmpty ? nil : $0 } ?? BoardCopy.noPreview)
                .font(.body)
                .foregroundStyle(Color.togoMute)
                .lineLimit(dynamicTypeSize.isAccessibilitySize ? 6 : 4)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(10)
                .background(Color.togoDeepest, in: .rect(cornerRadius: 8, style: .continuous))
        }
    }
}

/// The last lines of a screen in SF Mono, one text per line so a line never wraps.
private struct ThumbnailScreen: View {
    let text: String
    @State private var fontSize: CGFloat = 5

    var body: some View {
        let lines = text.isEmpty ? [] : text.components(separatedBy: "\n")
        let font = Font.system(size: fontSize, design: .monospaced)
        let lineHeight = (fontSize * 1.25).rounded(.up)
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                Text(verbatim: line.isEmpty ? " " : line)
                    .font(font)
                    .foregroundStyle(Color.togoMute)
                    .lineLimit(1)
                    .frame(height: lineHeight, alignment: .leading)
            }
        }
        .frame(maxWidth: .infinity, minHeight: lineHeight * CGFloat(ThumbnailModel.lines), alignment: .bottomLeading)
        .padding(8)
        .background(Color.togoDeepest, in: .rect(cornerRadius: 8, style: .continuous))
        .onGeometryChange(for: CGFloat.self) { $0.size.width - 16 } action: { width in
            let size = ThumbnailText.fontSize(fitting: width)
            if size != fontSize { fontSize = size }
        }
        // A picture of the screen; the card's label says what VoiceOver needs.
        .accessibilityHidden(true)
        // The thumbnail is a picture: it does not grow with Dynamic Type.
        .dynamicTypeSize(.large)
    }
}
