import SwiftUI

/// Markdown as blocks the app draws with system text: headings, paragraphs, list items, quotes,
/// rules and fenced code. Inline marks (bold, code spans, links) are left to `AttributedString`.
enum PrimitiveMarkdownBlock: Equatable {
    case heading(level: Int, text: String)
    case paragraph(String)
    case item(marker: String, text: String)
    case quote(String)
    case code(language: String?, text: String)
    case rule

    static func parse(_ markdown: String) -> [PrimitiveMarkdownBlock] {
        var blocks: [PrimitiveMarkdownBlock] = []
        var paragraph: [String] = []
        var fence: (language: String?, lines: [String])?

        func flush() {
            let text = paragraph.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
            if !text.isEmpty { blocks.append(.paragraph(text)) }
            paragraph = []
        }

        for raw in markdown.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if var open = fence {
                if line.hasPrefix("```") {
                    blocks.append(.code(language: open.language, text: open.lines.joined(separator: "\n")))
                    fence = nil
                } else {
                    open.lines.append(raw)
                    fence = open
                }
                continue
            }
            if line.hasPrefix("```") {
                flush()
                let language = String(line.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                fence = (language.isEmpty ? nil : language, [])
            } else if line.isEmpty {
                flush()
            } else if let heading = Self.heading(line) {
                flush()
                blocks.append(heading)
            } else if line == "---" || line == "***" || line == "___" {
                flush()
                blocks.append(.rule)
            } else if let item = Self.item(line) {
                flush()
                blocks.append(item)
            } else if line.hasPrefix(">") {
                flush()
                blocks.append(.quote(String(line.dropFirst()).trimmingCharacters(in: .whitespaces)))
            } else {
                paragraph.append(line)
            }
        }
        if let open = fence { blocks.append(.code(language: open.language, text: open.lines.joined(separator: "\n"))) }
        flush()
        return blocks
    }

    private static func heading(_ line: String) -> PrimitiveMarkdownBlock? {
        let hashes = line.prefix { $0 == "#" }.count
        guard (1...6).contains(hashes), line.dropFirst(hashes).first == " " else { return nil }
        return .heading(level: hashes, text: String(line.dropFirst(hashes + 1)))
    }

    private static func item(_ line: String) -> PrimitiveMarkdownBlock? {
        for bullet in ["- ", "* ", "+ "] where line.hasPrefix(bullet) {
            let rest = String(line.dropFirst(2))
            if rest.hasPrefix("[ ] ") { return .item(marker: "\u{25CB}", text: String(rest.dropFirst(4))) }
            if rest.hasPrefix("[x] ") || rest.hasPrefix("[X] ") { return .item(marker: "\u{2713}", text: String(rest.dropFirst(4))) }
            return .item(marker: "\u{2022}", text: rest)
        }
        let digits = line.prefix { $0.isNumber }
        if !digits.isEmpty, digits.count <= 3, line.dropFirst(digits.count).hasPrefix(". ") {
            return .item(marker: "\(digits).", text: String(line.dropFirst(digits.count + 2)))
        }
        return nil
    }

    /// Inline markdown as an attributed string, or the plain text when it does not parse.
    static func inline(_ text: String) -> AttributedString {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        return (try? AttributedString(markdown: text, options: options)) ?? AttributedString(text)
    }
}

/// Draws markdown blocks: SF for prose, SF Mono in deepest wells for code.
struct PrimitiveMarkdown: View {
    let blocks: [PrimitiveMarkdownBlock]

    init(_ markdown: String) {
        blocks = PrimitiveMarkdownBlock.parse(markdown)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                view(block)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .textSelection(.enabled)
    }

    @ViewBuilder private func view(_ block: PrimitiveMarkdownBlock) -> some View {
        switch block {
        case let .heading(level, text):
            Text(PrimitiveMarkdownBlock.inline(text))
                .font(level <= 1 ? .title3.weight(.semibold) : .headline)
                .foregroundStyle(Color.togoSnow)
                .accessibilityAddTraits(.isHeader)
        case let .paragraph(text):
            Text(PrimitiveMarkdownBlock.inline(text))
                .font(.body)
                .foregroundStyle(Color.togoSnow)
        case let .item(marker, text):
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(verbatim: marker)
                    .font(marker.last == "." ? .mono(.subheadline) : .body)
                    .foregroundStyle(Color.togoMute)
                    .accessibilityHidden(true)
                Text(PrimitiveMarkdownBlock.inline(text))
                    .font(.body)
                    .foregroundStyle(Color.togoSnow)
            }
        case let .quote(text):
            HStack(spacing: 10) {
                Capsule().fill(Color.togoLine).frame(width: 3)
                Text(PrimitiveMarkdownBlock.inline(text)).font(.body).foregroundStyle(Color.togoMute)
            }
            .fixedSize(horizontal: false, vertical: true)
        case let .code(_, text):
            ScrollView(.horizontal) {
                Text(verbatim: text)
                    .font(.mono(.footnote))
                    .foregroundStyle(Color.togoSnow)
                    .fixedSize(horizontal: true, vertical: false)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
            }
            .scrollIndicators(.hidden)
            .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
        case .rule:
            Rectangle().fill(Color.togoLine).frame(height: 1)
        }
    }
}
