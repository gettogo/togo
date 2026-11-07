import SwiftUI
import TogoKit

/// Assistant markdown split into prose and fenced code. Prose keeps inline markdown (bold,
/// italics, inline code, links); code blocks are drawn in SF Mono in a well.
enum ChatMarkdown {
    enum Block: Hashable {
        case prose(String)
        case heading(String)
        case code(language: String?, text: String)
    }

    static func blocks(_ text: String) -> [Block] {
        var blocks: [Block] = []
        var prose: [String] = []
        var code: [String] = []
        var language: String?
        var inCode = false

        func flushProse() {
            let joined = prose.joined(separator: "\n").trimmingCharacters(in: .newlines)
            if !joined.isEmpty { blocks.append(.prose(joined)) }
            prose = []
        }

        for raw in text.split(separator: "\n", omittingEmptySubsequences: false) {
            let line = String(raw)
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("```") {
                if inCode {
                    blocks.append(.code(language: language, text: code.joined(separator: "\n")))
                    code = []
                    language = nil
                } else {
                    flushProse()
                    let tag = trimmed.dropFirst(3).trimmingCharacters(in: .whitespaces)
                    language = tag.isEmpty ? nil : tag
                }
                inCode.toggle()
                continue
            }
            if inCode {
                code.append(line)
            } else if let heading = heading(trimmed) {
                flushProse()
                blocks.append(.heading(heading))
            } else {
                prose.append(bullet(line))
            }
        }
        // A reply still streaming may stop inside a fence; show what came so far as code.
        if inCode { blocks.append(.code(language: language, text: code.joined(separator: "\n"))) }
        flushProse()
        return blocks
    }

    /// Inline markdown, or the plain text when it does not parse.
    static func attributed(_ text: String) -> AttributedString {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        return (try? AttributedString(markdown: text, options: options)) ?? AttributedString(text)
    }

    private static func heading(_ line: String) -> String? {
        guard line.hasPrefix("#") else { return nil }
        let text = line.drop { $0 == "#" }
        guard text.first == " " else { return nil }
        return text.trimmingCharacters(in: .whitespaces)
    }

    private static func bullet(_ line: String) -> String {
        let indent = line.prefix { $0 == " " }
        let rest = line.dropFirst(indent.count)
        guard rest.hasPrefix("- ") || rest.hasPrefix("* ") else { return line }
        return String(indent) + "\u{2022} " + String(rest.dropFirst(2))
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
                    Text(ChatMarkdown.attributed(prose))
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                        .lineSpacing(3)
                        .tint(Color.togoSnow)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                case .heading(let heading):
                    Text(ChatMarkdown.attributed(heading))
                        .font(.headline)
                        .foregroundStyle(Color.togoSnow)
                        .accessibilityAddTraits(.isHeader)
                case .code(_, let code):
                    CodeWell(text: code)
                }
            }
        }
    }
}

/// Machine text in a well: SF Mono on the deepest surface, scrolling sideways rather than
/// wrapping, so a command or a stack trace keeps its shape.
struct CodeWell: View {
    let text: String

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Text(verbatim: text)
                .font(.system(.footnote, design: .monospaced))
                .foregroundStyle(Color.togoSnow)
                .textSelection(.enabled)
                .fixedSize(horizontal: true, vertical: false)
                .padding(12)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
    }
}
