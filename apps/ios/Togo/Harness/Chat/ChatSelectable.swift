import SwiftUI
import TogoKit
import UIKit

/// The chat's words as UIKit text for `SelectableText`: the attributed strings the chat used to
/// hand SwiftUI's `Text` (`ChatLinks`), in the same type, weight and colour, so a reply reads
/// the same and any part of it can be selected.
@MainActor enum ChatSelectable {
    /// How a piece of chat text is set.
    struct Style: Equatable {
        var textStyle: UIFont.TextStyle = .body
        var monospaced = false
        var color: UIColor = .togoSnow
        /// Extra room between lines, as SwiftUI's `lineSpacing`.
        var lineSpacing: CGFloat = 0
        /// Lines run as long as they are written (code); otherwise they wrap.
        var wraps = true
        /// Semibold, as a title's `.weight(.semibold)`.
        var bold = false
        var alignment: NSTextAlignment = .natural

        static let prose = Style(lineSpacing: 3)
        static let heading = Style(textStyle: .headline)
        static let prompt = Style()
        static let code = Style(textStyle: .footnote, monospaced: true, wraps: false)

        /// A table cell: the subheadline size, a header semibold, aligned as its column.
        static func cell(_ alignment: MarkdownTable.Alignment, header: Bool) -> Style {
            Style(textStyle: .subheadline, bold: header, alignment: alignment.text)
        }
    }

    /// Room between a reply's paragraphs, as the chat spaces its blocks.
    static let blockSpacing: CGFloat = 12

    /// A reply's text, cut where a code block sits between paragraphs: each run of prose and
    /// headings is one text, so a selection reaches across its paragraphs.
    enum Segment: Equatable {
        case text([MarkdownBlocks.Block])
        case code(String)
        case table(MarkdownTable)
    }

    static func segments(_ blocks: [MarkdownBlocks.Block]) -> [Segment] {
        var out: [Segment] = []
        for block in blocks {
            switch block {
            case .code(_, let code):
                out.append(.code(code))
            case .table(let table):
                out.append(.table(table))
            case .prose, .heading:
                if case .text(let run) = out.last {
                    out[out.count - 1] = .text(run + [block])
                } else {
                    out.append(.text([block]))
                }
            }
        }
        return out
    }

    // MARK: Fonts

    static func font(_ style: Style, traits: UITraitCollection) -> UIFont {
        let preferred = UIFont.preferredFont(forTextStyle: style.textStyle, compatibleWith: traits)
        if style.monospaced {
            return UIFont.monospacedSystemFont(ofSize: preferred.pointSize, weight: style.bold ? .semibold : .regular)
        }
        guard style.bold else { return preferred }
        return UIFont.systemFont(ofSize: preferred.pointSize, weight: .semibold)
    }

    // MARK: Attributed

    /// Inline markdown (bold, italics, code spans, strikes, links) as UIKit attributes. The
    /// characters are exactly the attributed string's: nothing is added or curled.
    static func attributed(_ text: AttributedString, style: Style, traits: UITraitCollection) -> NSMutableAttributedString {
        let base = font(style, traits: traits)
        let paragraph = paragraphStyle(style)
        let out = NSMutableAttributedString()
        for run in text.runs {
            let intent = run.inlinePresentationIntent ?? []
            var shaped = intent.contains(.code) ? UIFont.monospacedSystemFont(ofSize: base.pointSize, weight: .regular) : base
            var symbolic: UIFontDescriptor.SymbolicTraits = []
            if intent.contains(.stronglyEmphasized) { symbolic.insert(.traitBold) }
            if intent.contains(.emphasized) { symbolic.insert(.traitItalic) }
            if !symbolic.isEmpty,
               let descriptor = shaped.fontDescriptor.withSymbolicTraits(shaped.fontDescriptor.symbolicTraits.union(symbolic)) {
                shaped = UIFont(descriptor: descriptor, size: shaped.pointSize)
            }
            var attributes: [NSAttributedString.Key: Any] = [
                .font: shaped, .foregroundColor: style.color, .paragraphStyle: paragraph,
            ]
            if intent.contains(.strikethrough) { attributes[.strikethroughStyle] = NSUnderlineStyle.single.rawValue }
            if let link = run.link { attributes[.link] = link }
            out.append(NSAttributedString(string: String(text[run.range].characters), attributes: attributes))
        }
        return out
    }

    /// A run of a reply's paragraphs and headings as one text, each block on its own lines and
    /// the blocks spaced as the chat spaces them.
    static func text(_ blocks: [MarkdownBlocks.Block], traits: UITraitCollection) -> NSAttributedString {
        let out = NSMutableAttributedString()
        var lastLines: [NSRange] = []
        for (index, block) in blocks.enumerated() {
            let piece: NSAttributedString
            switch block {
            case .prose(let prose): piece = hangingListItems(attributed(ChatLinks.markdown(prose), style: .prose, traits: traits))
            case .heading(let heading): piece = attributed(ChatLinks.markdown(heading), style: .heading, traits: traits)
            case .code(_, let code): piece = attributed(ChatLinks.plain(code), style: .code, traits: traits)
            case .table(let table): piece = attributed(AttributedString(table.tabSeparated()), style: .prose, traits: traits)
            }
            if index > 0 {
                // The break takes the previous block's look, so it belongs to that paragraph.
                let previous = out.length > 0 ? out.attributes(at: out.length - 1, effectiveRange: nil) : [:]
                out.append(NSAttributedString(string: "\n", attributes: previous))
                lastLines[lastLines.count - 1].length += 1
            }
            let start = out.length
            out.append(piece)
            let ns = out.string as NSString
            let last = ns.lineRange(for: NSRange(location: max(start, out.length - 1), length: 0))
            lastLines.append(NSRange(location: max(start, last.location), length: NSMaxRange(last) - max(start, last.location)))
        }
        // Every block's last line but the reply's last is followed by the space between blocks.
        for range in lastLines.dropLast() where range.length > 0 {
            guard let style = out.attribute(.paragraphStyle, at: range.location, effectiveRange: nil) as? NSParagraphStyle,
                  let spaced = style.mutableCopy() as? NSMutableParagraphStyle else { continue }
            spaced.paragraphSpacing = blockSpacing
            out.addAttribute(.paragraphStyle, value: spaced, range: range)
        }
        return out
    }

    /// A list item as one text: its marker in the muted colour, and wrapped lines hanging under
    /// the words rather than the marker. The marker is copied with the words, as a list pastes.
    static func item(marker: String, _ text: AttributedString, style: Style, traits: UITraitCollection) -> NSAttributedString {
        let font = font(style, traits: traits)
        let lead = marker + " "
        let indent = ceil((lead as NSString).size(withAttributes: [.font: font]).width)
        let paragraph = (paragraphStyle(style).mutableCopy() as? NSMutableParagraphStyle) ?? NSMutableParagraphStyle()
        paragraph.headIndent = indent
        let out = NSMutableAttributedString(string: lead, attributes: [.font: font, .foregroundColor: UIColor.togoMute])
        out.append(attributed(text, style: style, traits: traits))
        out.addAttribute(.paragraphStyle, value: paragraph, range: NSRange(location: 0, length: out.length))
        return out
    }

    /// A list item's wrapped lines hang under its words, not under its marker or number, at
    /// every nesting depth: "• ", "☐ ", "☑ ", "1. " and "2) " after any indent.
    static func hangingListItems(_ text: NSMutableAttributedString) -> NSMutableAttributedString {
        let ns = text.string as NSString
        var location = 0
        while location < ns.length {
            let line = ns.lineRange(for: NSRange(location: location, length: 0))
            defer { location = NSMaxRange(line) }
            let match = listMarker.firstMatch(in: text.string, range: line)
            guard let match, match.range.location == line.location,
                  let font = text.attribute(.font, at: line.location, effectiveRange: nil) as? UIFont,
                  let style = (text.attribute(.paragraphStyle, at: line.location, effectiveRange: nil) as? NSParagraphStyle)?
                    .mutableCopy() as? NSMutableParagraphStyle else { continue }
            let lead = ns.substring(with: match.range).replacingOccurrences(of: "\t", with: "    ")
            style.headIndent = ceil((lead as NSString).size(withAttributes: [.font: font]).width)
            text.addAttribute(.paragraphStyle, value: style, range: line)
        }
        return text
    }

    private static let listMarker = try! NSRegularExpression(pattern: "^[ \\t]*(?:[\u{2022}\u{2610}\u{2611}]|\\d+[.)])[ ]+(?:[\u{2610}\u{2611}] )?", options: [.anchorsMatchLines])

    private static func paragraphStyle(_ style: Style) -> NSParagraphStyle {
        let paragraph = NSMutableParagraphStyle()
        paragraph.alignment = style.alignment
        paragraph.lineSpacing = style.lineSpacing
        // Code keeps its shape: a line runs on rather than wrapping.
        paragraph.lineBreakMode = style.wraps ? .byWordWrapping : .byClipping
        return paragraph
    }
}

/// A row's own actions in its words' edit menu: the selected part can be quoted, the whole row
/// copied, and the row found in the terminal.
enum ChatTextMenu {
    @MainActor static func actions(
        item: HarnessItem, quote: (@MainActor (String) -> Void)?, showInTerminal: (@MainActor () -> Void)?
    ) -> SelectableTextActions {
        SelectableTextActions { selected in
            var items: [UIMenuElement] = []
            if let text = item.chatText {
                if let quote {
                    let quoted = selected.isEmpty ? text : selected
                    items.append(UIAction(title: ChatCopy.quote, image: UIImage(systemName: "text.quote")) { _ in
                        quote(ChatQuote.quoted(quoted))
                    })
                }
                items.append(UIAction(title: ChatCopy.copyMessage, image: UIImage(systemName: "doc.on.doc")) { _ in
                    item.copyWords()
                })
            }
            if let showInTerminal {
                items.append(UIAction(title: ChatCopy.showInTerminal, image: UIImage(systemName: "terminal")) { _ in
                    showInTerminal()
                })
            }
            return items
        }
    }
}

/// A row's words opened in the Select text sheet.
struct SelectedText: Identifiable {
    let id: String
    let text: String
    /// A reply's markdown shows rendered, so a selected part copies without its marks.
    var markdown = false
}

extension MarkdownTable.Alignment {
    var text: NSTextAlignment {
        switch self {
        case .leading: .natural
        case .center: .center
        case .trailing: .right
        }
    }

    var frame: SwiftUI.Alignment {
        switch self {
        case .leading: .leading
        case .center: .center
        case .trailing: .trailing
        }
    }
}
