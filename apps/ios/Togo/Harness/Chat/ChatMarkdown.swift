import SwiftUI
import TogoKit
import UIKit

/// Assistant markdown split into prose and fenced code. Prose keeps inline markdown (bold,
/// italics, inline code, links); code blocks are drawn in SF Mono in a well.
enum ChatMarkdown {
    typealias Block = MarkdownBlocks.Block

    /// Shared by every reply on screen. A streamed update extends the text it saw last, so only
    /// the last block is read again; earlier blocks and their attributed text come from caches.
    static let cache = MarkdownBlocksCache(limit: 64)

    static func blocks(_ text: String) -> [Block] {
        cache.blocks(text)
    }
}

/// A reply's markdown, laid out block by block. Prose and headings are selectable text
/// (`SelectableText`): a long press picks a word with handles to drag, and a selection reaches
/// across paragraphs up to a code block. Links open on a tap.
struct MarkdownText: View, Equatable {
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: ChatSelectable.blockSpacing) {
            ForEach(Array(ChatSelectable.segments(ChatMarkdown.blocks(text)).enumerated()), id: \.offset) { _, segment in
                switch segment {
                case .text(let blocks):
                    // A streamed reply changes only its last blocks; the text before keeps its
                    // key, so it is neither rebuilt nor measured again.
                    SelectableText(key: blocks, copy: { ChatPasteboard.copy(selection: $0) }) { traits in
                        ChatSelectable.text(blocks, traits: traits)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                case .code(let code):
                    CodeWell(text: code)
                case .table(let table):
                    MarkdownTableView(table: table)
                }
            }
        }
    }
}

/// Machine text in a well: SF Mono on the deepest surface, scrolling sideways rather than
/// wrapping, so a command or a stack trace keeps its shape. Any part of it can be selected and
/// copies exactly, whitespace and all; its URLs and file references open on a tap. Copy in its
/// corner, and in the edit menu, copies the whole code with nothing added.
struct CodeWell: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// The row's own actions (Quote, Copy message), kept after Copy code in the edit menu.
    @Environment(\.selectableTextActions) private var rowActions
    let text: String

    @State private var copied = false

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            // The corner's copy button keeps clear of the first line's end.
            SelectableText(
                key: text, layout: .lines, insets: UIEdgeInsets(top: 12, left: 12, bottom: 12, right: 40),
                copy: { ChatPasteboard.copy(exact: $0.string) }
            ) { traits in
                ChatSelectable.attributed(ChatLinks.plain(text), style: .code, traits: traits)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
        .overlay(alignment: .topTrailing) { copyButton }
        .environment(\.selectableTextActions, SelectableTextActions { [rowActions] selected in
            [UIAction(title: ChatCopy.copyCode, image: UIImage(systemName: "doc.on.doc")) { _ in copy() }]
                + (rowActions?.items(selected) ?? [])
        })
    }

    private var copyButton: some View {
        Button(action: copy) {
            Image(systemName: copied ? "checkmark" : "doc.on.doc")
                // motion: state change — the glyph becomes a check in place once the code is copied.
                .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
                .font(.footnote.weight(.medium))
                .foregroundStyle(Color.togoMute)
                .frame(width: 44, height: 40)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .hoverEffect(.highlight)
        .accessibilityLabel(copied ? ChatCopy.copied : ChatCopy.copyCode)
        .task(id: copied) {
            guard copied else { return }
            try? await Task.sleep(for: .seconds(1.5))
            copied = false
        }
    }

    private func copy() {
        ChatPasteboard.copy(exact: text)
        Haptic.done.play()
        copied = true
    }
}
