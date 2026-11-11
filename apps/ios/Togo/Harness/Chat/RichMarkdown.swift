import Foundation
import TogoKit
import UIKit

/// A reply's markdown as rich text, for the pasteboard and the Select text sheet: the same
/// blocks the chat draws (`MarkdownBlocks`), with inline bold, italics, code spans, strikes and
/// links as real formatting instead of `**` and backticks.
///
/// Exactness rules: prose characters are kept as written (no quotes curled, no dashes made),
/// code is byte for byte what the code block holds, and a link's address is the one the
/// markdown or the text gave, never one the app made up. The app's own `togo-file` links stay
/// in the app: outside it a file reference is its words.
enum RichMarkdown {
    struct Run: Equatable {
        var text: String
        var bold = false
        var italic = false
        var code = false
        var strike = false
        var link: URL?
    }

    enum Block: Equatable {
        case paragraph([Run])
        case heading([Run])
        case code(String)
        case table(MarkdownTable)
    }

    static func blocks(_ markdown: String) -> [Block] {
        MarkdownBlocks.parse(markdown).blocks.map { block in
            switch block {
            case .prose(let text): .paragraph(runs(text))
            case .heading(let text): .heading(runs(text))
            case .code(_, let text): .code(text)
            case .table(let table): .table(table)
            }
        }
    }

    /// Inline markdown as runs, line breaks kept. Text that does not parse is one plain run.
    static func runs(_ inline: String) -> [Run] {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        guard let parsed = try? AttributedString(markdown: inline, options: options) else {
            return linkedBare([Run(text: inline)])
        }
        var out: [Run] = []
        for run in parsed.runs {
            let intent = run.inlinePresentationIntent ?? []
            out.append(Run(
                text: String(parsed[run.range].characters),
                bold: intent.contains(.stronglyEmphasized),
                italic: intent.contains(.emphasized),
                code: intent.contains(.code),
                strike: intent.contains(.strikethrough),
                link: run.link.flatMap(exported)
            ))
        }
        return linkedBare(out)
    }

    /// Words as they read: the text without its marks, the way the Select text sheet shows it.
    static func plain(_ markdown: String) -> String {
        blocks(markdown).map { block in
            switch block {
            case .paragraph(let runs), .heading(let runs): runs.map(\.text).joined()
            case .code(let text): text
            case .table(let table): table.tabSeparated(ChatTableText.words)
            }
        }
        .joined(separator: "\n\n")
    }

    /// The reply as VoiceOver reads it: the words without marks, a table a row at a time.
    static func spoken(_ markdown: String) -> String {
        blocks(markdown).map { block in
            switch block {
            case .paragraph(let runs), .heading(let runs): runs.map(\.text).joined()
            case .code(let text): text
            case .table(let table):
                ([ChatCopy.table(rows: table.rows.count)] + table.spokenRows(ChatTableText.words)).joined(separator: ". ")
            }
        }
        .joined(separator: "\n")
    }

    // MARK: HTML

    /// HTML for apps that paste formatting (Notes, Mail, Slack, Docs). Inline styles only, so
    /// it reads the same wherever it lands.
    static func html(_ markdown: String) -> String {
        let body = blocks(markdown).map { block in
            switch block {
            case .paragraph(let runs): "<p>\(html(runs: runs))</p>"
            case .heading(let runs): "<h3>\(html(runs: runs))</h3>"
            case .code(let text): "<pre><code>\(escape(text))</code></pre>"
            case .table(let table): ChatTableText.html(table)
            }
        }
        return "<meta charset=\"utf-8\">" + body.joined(separator: "\n")
    }

    static func html(runs: [Run]) -> String {
        runs.map { run in
            var piece = escape(run.text).replacingOccurrences(of: "\n", with: "<br>")
            if run.code { piece = "<code>\(piece)</code>" }
            if run.strike { piece = "<s>\(piece)</s>" }
            if run.italic { piece = "<i>\(piece)</i>" }
            if run.bold { piece = "<b>\(piece)</b>" }
            if let link = run.link { piece = "<a href=\"\(escape(link.absoluteString))\">\(piece)</a>" }
            return piece
        }
        .joined()
    }

    static func escape(_ text: String) -> String {
        var out = ""
        out.reserveCapacity(text.utf8.count)
        for character in text {
            switch character {
            case "&": out += "&amp;"
            case "<": out += "&lt;"
            case ">": out += "&gt;"
            case "\"": out += "&quot;"
            default: out.append(character)
            }
        }
        return out
    }

    // MARK: Attributed

    /// How the attributed text looks: fixed sizes and no colour on the pasteboard, so it takes
    /// the destination's ink; the app's type and colour in the Select text sheet.
    struct Style {
        var body: UIFont
        var color: UIColor?

        static var pasteboard: Style { Style(body: .systemFont(ofSize: 15), color: nil) }

        @MainActor static var screen: Style {
            Style(body: .preferredFont(forTextStyle: .body), color: .togoSnow)
        }
    }

    static func attributed(_ markdown: String, style: Style) -> NSAttributedString {
        let out = NSMutableAttributedString()
        let size = style.body.pointSize
        let mono = UIFont.monospacedSystemFont(ofSize: size * 0.9, weight: .regular)
        for (index, block) in blocks(markdown).enumerated() {
            if index > 0 { out.append(NSAttributedString(string: "\n\n", attributes: base(style.body, style))) }
            switch block {
            case .paragraph(let runs):
                for run in runs { out.append(attributed(run, font: style.body, mono: mono, style: style)) }
            case .heading(let runs):
                let heading = style.body.withSize(size * 1.15)
                for var run in runs {
                    run.bold = true
                    out.append(attributed(run, font: heading, mono: mono, style: style))
                }
            case .code(let text):
                out.append(NSAttributedString(string: text, attributes: base(mono, style)))
            case .table(let table):
                out.append(NSAttributedString(string: table.tabSeparated(ChatTableText.words), attributes: base(style.body, style)))
            }
        }
        return out
    }

    private static func attributed(_ run: Run, font: UIFont, mono: UIFont, style: Style) -> NSAttributedString {
        var traits: UIFontDescriptor.SymbolicTraits = []
        if run.bold { traits.insert(.traitBold) }
        if run.italic { traits.insert(.traitItalic) }
        var shaped = run.code ? mono : font
        if !traits.isEmpty, let descriptor = shaped.fontDescriptor.withSymbolicTraits(shaped.fontDescriptor.symbolicTraits.union(traits)) {
            shaped = UIFont(descriptor: descriptor, size: shaped.pointSize)
        }
        var attributes = base(shaped, style)
        if run.strike { attributes[.strikethroughStyle] = NSUnderlineStyle.single.rawValue }
        if let link = run.link { attributes[.link] = link }
        return NSAttributedString(string: run.text, attributes: attributes)
    }

    private static func base(_ font: UIFont, _ style: Style) -> [NSAttributedString.Key: Any] {
        var attributes: [NSAttributedString.Key: Any] = [.font: font]
        if let color = style.color { attributes[.foregroundColor] = color }
        return attributes
    }

    /// RTF of the attributed text, for apps that read RTF before HTML.
    static func rtf(_ text: NSAttributedString) -> Data? {
        try? text.data(
            from: NSRange(location: 0, length: text.length),
            documentAttributes: [.documentType: NSAttributedString.DocumentType.rtf]
        )
    }

    // MARK: Links

    /// Web and mail links leave the app; anything else (the chat's own `togo-file` links, an
    /// app's scheme) stays words.
    static func exported(_ url: URL) -> URL? {
        switch url.scheme?.lowercased() {
        case "http", "https", "mailto": url
        default: nil
        }
    }

    private static let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue)

    /// Bare web addresses in prose become links whose address is exactly the text written, so
    /// a paste never carries a link the reader did not see. Code spans stay as they are.
    private static func linkedBare(_ runs: [Run]) -> [Run] {
        guard let detector else { return runs }
        var out: [Run] = []
        for run in runs {
            guard run.link == nil, !run.code else {
                out.append(run)
                continue
            }
            let ns = run.text as NSString
            var cursor = 0
            for match in detector.matches(in: run.text, range: NSRange(location: 0, length: ns.length)) {
                let written = ns.substring(with: match.range)
                guard written.lowercased().hasPrefix("http://") || written.lowercased().hasPrefix("https://"),
                      let url = URL(string: written) else { continue }
                if match.range.location > cursor {
                    var before = run
                    before.text = ns.substring(with: NSRange(location: cursor, length: match.range.location - cursor))
                    out.append(before)
                }
                var linked = run
                linked.text = written
                linked.link = url
                out.append(linked)
                cursor = match.range.location + match.range.length
            }
            if cursor == 0 {
                out.append(run)
            } else if cursor < ns.length {
                var after = run
                after.text = ns.substring(from: cursor)
                out.append(after)
            }
        }
        return out
    }
}
