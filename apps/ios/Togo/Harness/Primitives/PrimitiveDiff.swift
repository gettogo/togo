import SwiftUI
import TogoKit

/// A unified diff read by the review's parser and drawn compactly for a card: one header per
/// file with its counts, folding open, then numbered lines on the added and removed tints.
/// Long files stop at `lineCap` and point at the terminal for the rest.
///
/// Each file folds as a two-phase disclosure on `resize` (spec §2.1/§2.2); the card clips, so
/// lines never draw outside it while it shrinks.
struct PrimitiveDiff: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let files: [PrimitiveDiffLogic.File]
    @State private var open: [String: Disclosure]

    static let lineCap = 300

    init(_ text: String) {
        let files = PrimitiveDiffLogic.files(text)
        self.files = files
        let initially = PrimitiveDiffLogic.initiallyOpen(files)
        _open = State(initialValue: Dictionary(
            files.map { ($0.id, Disclosure(open: initially.contains($0.id))) }, uniquingKeysWith: { first, _ in first }
        ))
    }

    private func disclosure(_ file: PrimitiveDiffLogic.File) -> Disclosure { open[file.id] ?? Disclosure() }

    private func isOpen(_ file: PrimitiveDiffLogic.File) -> Bool { disclosure(file).isOpen }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(files.enumerated()), id: \.element.id) { index, file in
                if index > 0 { Rectangle().fill(Color.togoLine).frame(height: 1) }
                header(file)
                if disclosure(file).open {
                    lines(file)
                        .disclosed(disclosure(file))
                }
            }
        }
        .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
        .clipShape(.rect(cornerRadius: 12, style: .continuous))
    }

    private func header(_ file: PrimitiveDiffLogic.File) -> some View {
        Button {
            let binding = Binding { open[file.id] ?? Disclosure() } set: { open[file.id] = $0 }
            Motion.toggle(binding, reduce: reduceMotion)
        } label: {
            HStack(spacing: 8) {
                Image(systemName: "chevron.right")
                    .font(.caption2.weight(.bold))
                    .foregroundStyle(Color.togoMute)
                    .rotationEffect(.degrees(isOpen(file) && !reduceMotion ? 90 : 0))
                    .motion(.respond, value: isOpen(file))
                    .accessibilityHidden(true)
                Text(verbatim: file.path.isEmpty ? PrimitiveCopy.changes : file.path)
                    .font(.mono(.caption))
                    .foregroundStyle(Color.togoSnow)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Text(verbatim: "+\(file.added)").font(.mono(.caption)).foregroundStyle(PrimitiveTint.ok)
                Text(verbatim: "-\(file.removed)").font(.mono(.caption)).foregroundStyle(PrimitiveTint.fail)
            }
            .padding(.horizontal, 10)
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(file.path.isEmpty ? PrimitiveCopy.changes : file.path)
        .accessibilityValue(PrimitiveCopy.linesSpoken(added: file.added, removed: file.removed))
        .accessibilityHint(isOpen(file)
            ? String(localized: "primitive.diff.fold", defaultValue: "Hides the lines")
            : String(localized: "primitive.diff.unfold", defaultValue: "Shows the lines"))
    }

    private func lines(_ file: PrimitiveDiffLogic.File) -> some View {
        let shown = file.lines.prefix(Self.lineCap)
        return VStack(alignment: .leading, spacing: 0) {
            ScrollView(.horizontal) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(shown.enumerated()), id: \.offset) { _, line in
                        row(line)
                    }
                }
                .padding(.vertical, 4)
            }
            .scrollIndicators(.hidden)
            if file.lines.count > Self.lineCap {
                Text(PrimitiveCopy.moreLines)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .padding(10)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private func row(_ line: PrimitiveDiffLogic.Line) -> some View {
        HStack(spacing: 0) {
            Text(verbatim: line.number.map(String.init) ?? "")
                .foregroundStyle(Color.togoGutterText)
                .frame(minWidth: 34, alignment: .trailing)
                .padding(.trailing, 8)
            Text(verbatim: line.prefix + line.text)
                .foregroundStyle(line.kind == .hunk ? Color.togoMute : Color.togoSnow)
                .fixedSize(horizontal: true, vertical: false)
                .padding(.trailing, 10)
        }
        .font(.mono(.caption2))
        .padding(.vertical, 1)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(background(line.kind))
    }

    private func background(_ kind: PrimitiveDiffLogic.Kind) -> Color {
        switch kind {
        case .added: .togoDiffAddedBg
        case .removed: .togoDiffRemovedBg
        case .hunk: .togoDiffHunkBg
        case .context: .clear
        }
    }
}

/// Turns diff text into files of drawable lines, through `DiffParser` when it finds file
/// headers and line by line when the text is only hunks.
enum PrimitiveDiffLogic {
    enum Kind: Equatable { case context, added, removed, hunk }

    struct Line: Equatable {
        var kind: Kind
        var number: Int?
        var text: String

        var prefix: String {
            switch kind {
            case .added: "+ "
            case .removed: "- "
            case .context: "  "
            case .hunk: ""
            }
        }
    }

    struct File: Equatable, Identifiable {
        var path: String
        var added: Int
        var removed: Int
        var lines: [Line]
        var id: String { path }
    }

    static func files(_ text: String) -> [File] {
        let parsed = DiffParser.parse(GitDiff(diff: text, truncated: false, binary: []))
        let files = parsed.compactMap { file -> File? in
            var lines: [Line] = []
            for hunk in file.hunks {
                lines.append(Line(kind: .hunk, number: nil, text: hunk.header))
                for line in hunk.lines {
                    switch line.kind {
                    case .added: lines.append(Line(kind: .added, number: line.newNumber, text: line.text))
                    case .removed: lines.append(Line(kind: .removed, number: line.oldNumber, text: line.text))
                    case .context: lines.append(Line(kind: .context, number: line.newNumber, text: line.text))
                    case .noNewline: continue
                    }
                }
            }
            let path = file.newPath ?? file.oldPath ?? ""
            guard !path.isEmpty || !lines.isEmpty else { return nil }
            return File(path: path, added: file.additions, removed: file.deletions, lines: lines)
        }
        return files.isEmpty ? [loose(text)] : files
    }

    /// Lines without file headers, classified by their first character.
    static func loose(_ text: String) -> File {
        var lines: [Line] = []
        var added = 0, removed = 0
        var oldLine = 0, newLine = 0
        for raw in text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            if raw.hasPrefix("@@") {
                lines.append(Line(kind: .hunk, number: nil, text: raw))
                let numbers = raw.split(separator: " ").dropFirst().prefix(2)
                oldLine = numbers.first.flatMap { Int($0.dropFirst().split(separator: ",").first ?? "") } ?? 0
                newLine = numbers.dropFirst().first.flatMap { Int($0.dropFirst().split(separator: ",").first ?? "") } ?? 0
            } else if raw.hasPrefix("+") {
                added += 1
                lines.append(Line(kind: .added, number: newLine, text: String(raw.dropFirst())))
                newLine += 1
            } else if raw.hasPrefix("-") {
                removed += 1
                lines.append(Line(kind: .removed, number: oldLine, text: String(raw.dropFirst())))
                oldLine += 1
            } else if !raw.isEmpty || !lines.isEmpty {
                lines.append(Line(kind: .context, number: newLine == 0 ? nil : newLine, text: String(raw.dropFirst(raw.hasPrefix(" ") ? 1 : 0))))
                if newLine > 0 { newLine += 1; oldLine += 1 }
            }
        }
        while lines.last?.kind == .context, lines.last?.text.isEmpty == true { lines.removeLast() }
        return File(path: "", added: added, removed: removed, lines: lines)
    }

    /// Small diffs open whole; a larger one opens its first file.
    static func initiallyOpen(_ files: [File]) -> Set<String> {
        let total = files.reduce(0) { $0 + $1.lines.count }
        if files.count <= 3 && total <= 120 { return Set(files.map(\.id)) }
        return Set(files.prefix(1).map(\.id))
    }
}
