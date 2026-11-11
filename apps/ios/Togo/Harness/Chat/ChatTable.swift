import SwiftUI
import TogoKit
import UIKit

/// A Markdown table in a reply, drawn as a table the way the agent's own terminal draws one:
/// a raised header row, hairlines between rows, each column aligned as the Markdown says and
/// as wide as its widest cell up to a readable measure, where a long cell wraps by words.
/// A table wider than the column scrolls sideways, its hidden side fading at the edge.
///
/// Every cell is selectable text; Copy table (its corner and the edit menu) puts the table on
/// the pasteboard as Markdown, tab-separated values and HTML, so it pastes into Notes, Numbers
/// and Slack as a table. VoiceOver reads it a row at a time: "Component: Diff Viewer, Status:
/// Ready".
struct MarkdownTableView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// The row's own actions (Quote, Copy message), kept after Copy table in the edit menu.
    @Environment(\.selectableTextActions) private var rowActions
    let table: MarkdownTable

    @State private var copied = false
    @State private var edges = HiddenEdges()

    /// The widest a column grows before its cells wrap: a readable line at phone width.
    static let columnWidth: CGFloat = 240

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .topLeading, horizontalSpacing: 0, verticalSpacing: 0) {
                GridRow {
                    ForEach(table.header.indices, id: \.self) { column in
                        cell(table.header[column], column: column, header: true)
                    }
                }
                .background(Color.togoRaised)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(ChatCopy.tableColumns(table.header.map(ChatTableText.words)))
                ForEach(table.rows.indices, id: \.self) { index in
                    Rectangle().fill(Color.togoLine).frame(height: 1)
                    GridRow {
                        ForEach(table.header.indices, id: \.self) { column in
                            cell(table.rows[index][column], column: column, header: false)
                        }
                    }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(spoken[index])
                }
            }
            // The corner's copy button keeps clear of the header's last cell.
            .padding(.trailing, 36)
        }
        .onScrollGeometryChange(for: HiddenEdges.self) { geometry in
            HiddenEdges(
                leading: geometry.contentOffset.x > 1,
                trailing: geometry.contentOffset.x + geometry.containerSize.width < geometry.contentSize.width - 1
            )
        } action: { _, hidden in
            edges = hidden
        }
        .mask { fade }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
        .clipShape(.rect(cornerRadius: 12, style: .continuous))
        .overlay(alignment: .topTrailing) { copyButton }
        .environment(\.selectableTextActions, SelectableTextActions { [rowActions] selected in
            [UIAction(title: ChatCopy.copyTable, image: UIImage(systemName: "tablecells")) { _ in copy() }]
                + (rowActions?.items(selected) ?? [])
        })
        .accessibilityElement(children: .contain)
        .accessibilityLabel(ChatCopy.table(rows: table.rows.count))
        .accessibilityActions {
            Button(ChatCopy.copyTable) { copy() }
        }
    }

    private var spoken: [String] { table.spokenRows(ChatTableText.words) }

    private func cell(_ text: String, column: Int, header: Bool) -> some View {
        let alignment = table.alignments.indices.contains(column) ? table.alignments[column] : .leading
        return SelectableText(
            key: [text, header ? "header" : "cell", "\(column)"], maxWidth: Self.columnWidth,
            copy: { ChatPasteboard.copy(selection: $0) }
        ) { traits in
            ChatSelectable.attributed(ChatLinks.markdown(text), style: .cell(alignment, header: header), traits: traits)
        }
        .frame(minWidth: 24, maxWidth: .infinity, alignment: alignment.frame)
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
    }

    /// The side still hidden fades out, so the table reads as going on.
    private var fade: some View {
        HStack(spacing: 0) {
            LinearGradient(colors: [.clear, .black], startPoint: .leading, endPoint: .trailing)
                .frame(width: edges.leading ? 24 : 0)
            Rectangle().fill(.black)
            LinearGradient(colors: [.black, .clear], startPoint: .leading, endPoint: .trailing)
                .frame(width: edges.trailing ? 24 : 0)
        }
    }

    private var copyButton: some View {
        Button(action: copy) {
            Image(systemName: copied ? "checkmark" : "doc.on.doc")
                // motion: state change — the glyph becomes a check in place once the table is copied.
                .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
                .font(.footnote.weight(.medium))
                .foregroundStyle(Color.togoMute)
                .frame(width: 44, height: 40)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .hoverEffect(.highlight)
        .accessibilityLabel(copied ? ChatCopy.copied : ChatCopy.copyTable)
        .task(id: copied) {
            guard copied else { return }
            try? await Task.sleep(for: .seconds(1.5))
            copied = false
        }
    }

    private func copy() {
        ChatPasteboard.copy(table: table)
        Haptic.done.play()
        copied = true
    }

    struct HiddenEdges: Equatable {
        var leading = false
        var trailing = false
    }
}

/// A cell's words without their marks, for VoiceOver, spreadsheets and plain text.
enum ChatTableText {
    static func words(_ cell: String) -> String {
        RichMarkdown.runs(cell).map(\.text).joined()
    }

    /// The table as HTML, inline formatting kept, for apps that paste a real table.
    static func html(_ table: MarkdownTable) -> String {
        func row(_ cells: [String], tag: String) -> String {
            "<tr>" + cells.enumerated().map { column, cell in
                let align = table.alignments.indices.contains(column) ? table.alignments[column] : .leading
                let style = switch align {
                case .leading: ""
                case .center: " style=\"text-align:center\""
                case .trailing: " style=\"text-align:right\""
                }
                return "<\(tag)\(style)>\(RichMarkdown.html(runs: RichMarkdown.runs(cell)))</\(tag)>"
            }.joined() + "</tr>"
        }
        let body = table.rows.map { row($0, tag: "td") }.joined()
        return "<table><thead>\(row(table.header, tag: "th"))</thead><tbody>\(body)</tbody></table>"
    }
}
