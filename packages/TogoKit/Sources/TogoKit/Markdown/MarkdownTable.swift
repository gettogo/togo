import Foundation

/// A GitHub-flavoured Markdown table: a header row, each column's alignment and the body rows.
/// Cells keep their inline markdown (bold, code, links) for the view to style; an escaped
/// `\|` is a pipe inside a cell. Every row has as many cells as the header.
public struct MarkdownTable: Hashable, Sendable {
    public enum Alignment: Hashable, Sendable {
        case leading, center, trailing
    }

    public var header: [String]
    public var alignments: [Alignment]
    public var rows: [[String]]

    public init(header: [String], alignments: [Alignment], rows: [[String]]) {
        self.header = header
        self.alignments = alignments
        self.rows = rows.map { Self.fitted($0, to: header.count) }
    }

    /// The cells of a row line, or nil when the line is no table row (no pipe at all).
    public static func cells(_ line: some StringProtocol) -> [String]? {
        var text = line.trimmingCharacters(in: .whitespaces)
        guard text.contains("|") else { return nil }
        if text.hasPrefix("|") { text.removeFirst() }
        if text.hasSuffix("|"), !text.hasSuffix("\\|") { text.removeLast() }
        var cells: [String] = []
        var cell = ""
        var inCode = false
        var escaped = false
        for character in text {
            if escaped {
                cell.append(character == "|" ? "|" : "\\\(character)")
                escaped = false
                continue
            }
            switch character {
            // `\|` is a pipe in a cell, in a code span too (GFM); any other escape stays.
            case "\\": escaped = true
            case "`": inCode.toggle(); cell.append(character)
            case "|" where !inCode:
                cells.append(cell.trimmingCharacters(in: .whitespaces))
                cell = ""
            default: cell.append(character)
            }
        }
        if escaped { cell.append("\\") }
        cells.append(cell.trimmingCharacters(in: .whitespaces))
        return cells
    }

    /// The alignments a delimiter row (`| :--- | :---: | ---: |`) sets, or nil when the line
    /// is no delimiter row.
    public static func alignments(_ line: some StringProtocol) -> [Alignment]? {
        guard let cells = cells(line), !cells.isEmpty else { return nil }
        var out: [Alignment] = []
        for cell in cells {
            let dashes = cell.trimmingCharacters(in: CharacterSet(charactersIn: ":"))
            guard !dashes.isEmpty, dashes.allSatisfy({ $0 == "-" }) else { return nil }
            switch (cell.hasPrefix(":"), cell.hasSuffix(":")) {
            case (true, true): out.append(.center)
            case (false, true): out.append(.trailing)
            default: out.append(.leading)
            }
        }
        return out
    }

    static func fitted(_ row: [String], to count: Int) -> [String] {
        row.count >= count ? Array(row.prefix(count)) : row + Array(repeating: "", count: count - row.count)
    }

    // MARK: Out

    /// The table as Markdown again, pipes in cells escaped, for pasting where Markdown reads.
    public var markdown: String {
        func line(_ cells: [String]) -> String {
            "| " + cells.map { $0.replacingOccurrences(of: "|", with: "\\|") }.joined(separator: " | ") + " |"
        }
        let rule = alignments.map { alignment -> String in
            switch alignment {
            case .leading: "---"
            case .center: ":---:"
            case .trailing: "---:"
            }
        }
        return ([line(header), "| " + rule.joined(separator: " | ") + " |"] + rows.map(line)).joined(separator: "\n")
    }

    /// One line per row, cells parted by tabs, for spreadsheets (Numbers, Sheets).
    public func tabSeparated(_ words: (String) -> String = { $0 }) -> String {
        ([header] + rows).map { row in
            row.map { words($0).replacingOccurrences(of: "\t", with: " ").replacingOccurrences(of: "\n", with: " ") }
                .joined(separator: "\t")
        }
        .joined(separator: "\n")
    }

    /// Each body row read as its header's names and its values: "Component: Diff Viewer,
    /// Status: Ready".
    public func spokenRows(_ words: (String) -> String = { $0 }) -> [String] {
        rows.map { row in
            zip(header, row).compactMap { name, value in
                let value = words(value)
                guard !value.isEmpty else { return nil }
                let name = words(name)
                return name.isEmpty ? value : "\(name): \(value)"
            }
            .joined(separator: ", ")
        }
    }
}
