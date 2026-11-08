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

    /// Inline markdown with its links: markdown links, bare URLs and file references
    /// (`ChatLinks`). The plain text when it does not parse.
    static func attributed(_ text: String) -> AttributedString {
        ChatLinks.markdown(text)
    }
}

/// A reply's markdown, laid out block by block.
struct MarkdownText: View, Equatable {
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ForEach(Array(ChatMarkdown.blocks(text).enumerated()), id: \.offset) { _, block in
                switch block {
                case .prose(let prose):
                    // Links are underlined in snow and open on a tap (the chat's `openURL`);
                    // selecting text is "Select text" in the row's menu, off the tap path.
                    Text(ChatMarkdown.attributed(prose))
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                        .lineSpacing(3)
                        .tint(Color.togoSnow)
                        .frame(maxWidth: .infinity, alignment: .leading)
                case .heading(let heading):
                    Text(ChatMarkdown.attributed(heading))
                        .font(.headline)
                        .foregroundStyle(Color.togoSnow)
                        .tint(Color.togoSnow)
                        .accessibilityAddTraits(.isHeader)
                case .code(_, let code):
                    CodeWell(text: code)
                }
            }
        }
    }
}

/// Machine text in a well: SF Mono on the deepest surface, scrolling sideways rather than
/// wrapping, so a command or a stack trace keeps its shape. Its URLs and file references open
/// on a tap; Copy and Select text are in its menu.
struct CodeWell: View {
    let text: String

    @State private var selecting = false

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Text(ChatLinks.plain(text))
                .font(.system(.footnote, design: .monospaced))
                .foregroundStyle(Color.togoSnow)
                .tint(Color.togoSnow)
                .fixedSize(horizontal: true, vertical: false)
                .padding(12)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
        .contextMenu {
            Button(ChatCopy.copy, systemImage: "doc.on.doc") { UIPasteboard.general.string = text }
            Button(ChatCopy.selectText, systemImage: "selection.pin.in.out") { selecting = true }
        }
        // Nests by policy: selecting text from a reply inside a sheet (a helper's thread, a step's
        // details) returns to it, the way a share sheet does.
        .presentSheet("chat.markdownSelect", isPresented: $selecting, nests: true) { SelectTextSheet(text: text) }
    }
}
