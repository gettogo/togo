import CoreText
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct CodeLinesLayoutTests {
    private let inset = CodeLinesLayout.verticalInset

    /// A plain row `columns` wide.
    private func text(_ columns: Int, banner: Bool = false) -> CodeText {
        CodeText(String(repeating: "x", count: columns), words: banner)
    }

    private func layout(_ rows: [CodeLinesLayout.Row], wrap: Bool) -> CodeLinesLayout {
        let layout = CodeLinesLayout()
        layout.lineHeight = 20
        layout.advance = 10
        layout.gutterWidth = 50
        layout.sideGutterWidth = 30
        layout.wrap = wrap
        layout.rows = rows
        return layout
    }

    @Test func noWrapRowsHaveFixedHeight() {
        let layout = layout([.code(text(5)), .code(text(500)), .banner(text(300, banner: true)), .code(text(0))], wrap: false)
        layout.update(width: 400)
        for row in 0..<4 {
            #expect(layout.height(ofRow: row) == 20)
            #expect(layout.top(ofRow: row) == inset + CGFloat(row) * 20)
        }
        #expect(layout.row(at: inset + 45) == 2)
        #expect(layout.contentSize.height == 80 + 2 * inset)
    }

    @Test func wrapHeightFromColumns() {
        let layout = layout([.code(text(90)), .code(text(40)), .code(text(0)), .banner(text(10, banner: true))], wrap: true)
        layout.update(width: 50 + 40 * 10 + CodeLinesLayout.trailingInset)
        #expect(layout.codeColumns == 40)
        #expect(layout.height(ofRow: 0) == 60)
        #expect(layout.height(ofRow: 1) == 20)
        #expect(layout.height(ofRow: 2) == 20)
        #expect(layout.top(ofRow: 2) == inset + 80)
        #expect(layout.row(at: inset + 70) == 1)
        #expect(layout.row(at: inset + 59) == 0)
    }

    @Test func splitRowsWrapEachHalf() {
        let layout = layout([.split(text(30), text(90)), .banner(text(10, banner: true))], wrap: false)
        layout.update(width: 2 * (30 + 40 * 10 + CodeLinesLayout.trailingInset))
        #expect(layout.sideColumns == 40)
        #expect(layout.height(ofRow: 0) == 60)
        #expect(layout.height(ofRow: 1) == 20)
        #expect(!layout.scrollsHorizontally)
    }

    // MARK: Hanging indent

    @Test func continuationIndentsPastLeadingWhitespace() {
        let line = CodeText("    let x = " + String(repeating: "a", count: 80))
        #expect(CodeLinesLayout.continuationIndent(for: line, columns: 40) == 6)
        #expect(CodeLinesLayout.continuationIndent(for: CodeText("let x = 1"), columns: 40) == 2)
        // A tab is measured after it is expanded to its stop.
        #expect(CodeLinesLayout.continuationIndent(for: CodeText("\tx"), columns: 40) == ReviewLimits.tabWidth + 2)

        let layout = layout([.code(line)], wrap: true)
        layout.update(width: 50 + 40 * 10 + CodeLinesLayout.trailingInset)
        let starts = layout.breaks(line, limit: layout.codeLimit)
        // The first line fills 40 columns; each continuation starts 6 in and holds 34.
        #expect(starts == [0, 40, 74])
        #expect(layout.lineCount(line, limit: layout.codeLimit) == 3)
    }

    @Test func indentCappedAtHalfWidth() {
        let deep = CodeText(String(repeating: " ", count: 30) + String(repeating: "b", count: 50))
        #expect(CodeLinesLayout.continuationIndent(for: deep, columns: 40) == 20)
        #expect(CodeLinesLayout.continuationIndent(for: deep, columns: 41) == 20)
        #expect(CodeLinesLayout.continuationIndent(for: deep, columns: 100) == 32)
        let blank = CodeText(String(repeating: " ", count: 90))
        #expect(CodeLinesLayout.continuationIndent(for: blank, columns: 40) == 20)
        // Even at the cap every continuation line still takes at least one character.
        #expect(CodeLinesLayout.continuationIndent(for: deep, columns: 1) == 0)
    }

    @Test func rowHeightMatchesIndentedBreaks() throws {
        let plain = CodeText("        return " + String(repeating: "x", count: 150))
        let mixed = CodeText("    let name = \"" + String(repeating: "日本語", count: 20) + "\" // 😀 done")
        let banner = CodeText("@@ -1,4 +1,9 @@ " + String(repeating: "word ", count: 30), words: true)
        let rows: [CodeLinesLayout.Row] = [.code(plain), .code(mixed), .banner(banner), .split(plain, mixed)]
        for columns in [12, 24, 40, 77] {
            let layout = measured(rows, columns: columns)
            let font = try #require(layout.measure?.font)
            for (index, text) in [plain, mixed].enumerated() {
                let starts = layout.breaks(text, limit: layout.codeLimit)
                #expect(layout.height(ofRow: index) == CGFloat(starts.count) * layout.lineHeight)
                let indent = CGFloat(CodeLinesLayout.continuationIndent(for: text, columns: layout.codeColumns)) * layout.advance
                for (line, start) in starts.enumerated() {
                    let end = line + 1 < starts.count ? starts[line + 1] : text.length
                    let room = layout.codeLimit - (line == 0 ? 0 : indent)
                    #expect(drawnWidth(text, start..<end, font: font) <= room + 0.5, "line \(line) at \(columns) columns")
                }
            }
            // Hunk headers keep breaking between words, without an indent.
            let bannerStarts = layout.breaks(banner, limit: layout.bannerLimit)
            #expect(layout.height(ofRow: 2) == CGFloat(bannerStarts.count) * layout.lineHeight)
            let side = max(layout.breaks(plain, limit: layout.sideLimit).count, layout.breaks(mixed, limit: layout.sideLimit).count)
            #expect(layout.height(ofRow: 3) == CGFloat(side) * layout.lineHeight)
        }
    }

    @Test func displayColumnsTabsAndWide() {
        #expect(CodeLinesLayout.displayColumns("\tab", tabWidth: 4) == 6)
        #expect(CodeLinesLayout.displayColumns("日本", tabWidth: 4) == 4)
        #expect(CodeLinesLayout.displayColumns("ab\tc", tabWidth: 4) == 5)
        #expect(CodeLinesLayout.displayColumns("日\tx", tabWidth: 4) == 5)
        #expect(CodeLinesLayout.displayColumns("ok 👍", tabWidth: 4) == 5)
        #expect(CodeLinesLayout.displayColumns("", tabWidth: 4) == 0)
    }

    @Test func contentWidthFromLongestRow() {
        let layout = layout([.code(text(10)), .code(text(120)), .banner(text(500, banner: true))], wrap: false)
        layout.update(width: 400)
        #expect(layout.contentSize.width == 50 + 1_200 + CodeLinesLayout.trailingInset)
        #expect(layout.scrollsHorizontally)
        layout.wrap = true
        layout.update(width: 400)
        #expect(layout.contentSize.width == 400)
        let short = self.layout([.code(text(3))], wrap: false)
        short.update(width: 400)
        #expect(short.contentSize.width == 400)
    }

    @Test func prefixSumsRecomputeOnlyOnWidthChange() {
        let layout = layout(Array(repeating: .code(text(90)), count: 1_000), wrap: true)
        layout.update(width: 400)
        #expect(layout.recomputeCount == 1)
        layout.update(width: 400)
        layout.update(width: 400)
        #expect(layout.recomputeCount == 1)
        layout.update(width: 300)
        #expect(layout.recomputeCount == 2)
        layout.lineHeight = 24
        layout.update(width: 300)
        #expect(layout.recomputeCount == 3)
        let fixed = self.layout(Array(repeating: .code(text(90)), count: 1_000), wrap: false)
        fixed.update(width: 400)
        fixed.update(width: 300)
        #expect(fixed.recomputeCount == 0)
    }

    @Test func accessibilityLabelsNameKindAndNumber() {
        let added = CodeRow.line(
            number: 42, oldNumber: nil, text: "let x = 1", kind: .added, tokens: [], changes: [], clipped: false
        )
        let removed = CodeRow.line(
            number: nil, oldNumber: 40, text: "let x = 0", kind: .removed, tokens: [], changes: [], clipped: false
        )
        let plain = CodeRow.line(number: 12, oldNumber: nil, text: "}", kind: nil, tokens: [], changes: [], clipped: false)
        let blank = CodeRow.line(number: 7, oldNumber: 7, text: "", kind: .context, tokens: [], changes: [], clipped: false)
        let long = CodeRow.line(number: 3, oldNumber: nil, text: "a=1;", kind: nil, tokens: [], changes: [], clipped: true)
        #expect(added.accessibilityLabel == "Added line 42: let x = 1")
        #expect(removed.accessibilityLabel == "Removed line 40: let x = 0")
        #expect(plain.accessibilityLabel == "Line 12: }")
        #expect(blank.accessibilityLabel == "Line 7: blank")
        #expect(long.accessibilityLabel == "Line 3: a=1;, cut at 2,000 characters")
        #expect(CodeRow.hunk("@@ -10,7 +10,8 @@ export function routes(app: App) {").accessibilityLabel
            == "Changes at lines 10 to 17, export function routes(app: App) {")
        #expect(CodeRow.notice(CodeCopy.lineEnding(fromCRLF: true)).accessibilityLabel == "Line ending changed (CRLF to LF)")
        let pair = CodeRow.split(
            left: CodeRowSide(number: 40, text: "a", kind: .removed, tokens: [], changes: []),
            right: CodeRowSide(number: 41, text: "b", kind: .added, tokens: [], changes: [])
        )
        #expect(pair.accessibilityLabel == "Removed line 40: a. Added line 41: b")

        let cell = CodeLineCell(frame: .zero)
        let metrics = CodeMetrics(traits: UITraitCollection(), digits: 2, showsOld: true, showsGlyphs: true)
        cell.configure(row: added, render: CodeRender(halves: [], scrolls: false), metrics: metrics)
        #expect(cell.isAccessibilityElement)
        #expect(cell.accessibilityLabel == "Added line 42: let x = 1")
    }

    @Test func lineEndingChangeAddsNotice() throws {
        let diff = "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-same\r\n+same\n"
        let file = try #require(DiffParser.parse(GitDiff(diff: diff, truncated: false, binary: [])).first)
        let unified = CodeRow.unified(file)
        #expect(unified.last == .notice("Line ending changed (CRLF -> LF)"))
        #expect(CodeRow.split(file).last == .notice("Line ending changed (CRLF -> LF)"))
    }

    @Test func tabsMoveWordRanges() {
        let (text, map) = CodeText.expandTabs("\tab\tc", tabWidth: 4)
        #expect(text == "    ab  c")
        #expect(map == [0, 4, 5, 6, 8, 9])
        let shown = CodeText("\tab\tc")
        #expect(shown.position(1) == 4)
        #expect(shown.position(-3) == 0)
        #expect(shown.position(99) == 9)
        #expect(CodeFlow.breaks(CodeText("abcdefghij"), limit: 40, advance: 10, measure: nil) == [0, 4, 8])
    }

    @Test func splitClippedLineAddsNotice() throws {
        let long = String(repeating: "x", count: 2_100)
        let diff = "diff --git a/a.js b/a.js\n--- a/a.js\n+++ b/a.js\n@@ -1 +1 @@\n-\(long)\n+y\(long)\n"
        let file = try #require(DiffParser.parse(GitDiff(diff: diff, truncated: false, binary: [])).first)
        let rows = CodeRow.split(file)
        #expect(rows.last == .notice("This line is cut at 2,000 characters."))
        #expect(rows.last?.accessibilityLabel == "This line is cut at 2,000 characters.")
        #expect(!CodeRow.split(self.file(lines: ["-a", "+b"])).contains { if case .notice = $0 { true } else { false } })
    }

    private func file(lines: [String]) -> DiffFile {
        let diff = "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n" + lines.joined(separator: "\n") + "\n"
        return DiffParser.parse(GitDiff(diff: diff, truncated: false, binary: [])).first!
    }

    /// A layout with the real code font, wrapping at `columns`.
    private func measured(_ rows: [CodeLinesLayout.Row], columns: Int, wrap: Bool = true) -> CodeLinesLayout {
        let metrics = CodeMetrics(traits: UITraitCollection(), digits: 3, showsOld: false, showsGlyphs: false)
        let layout = CodeLinesLayout()
        layout.lineHeight = metrics.lineHeight
        layout.advance = metrics.advance
        layout.gutterWidth = metrics.gutterWidth
        layout.measure = CodeMeasure(font: metrics.font)
        layout.wrap = wrap
        layout.rows = rows
        layout.update(width: metrics.gutterWidth + CGFloat(columns) * metrics.advance + CodeLinesLayout.trailingInset + 0.5)
        return layout
    }

    private func drawnWidth(_ text: CodeText, _ range: Range<Int>, font: UIFont) -> CGFloat {
        let string = NSAttributedString(string: text.string, attributes: [.font: font])
        let line = CTTypesetterCreateLine(
            CTTypesetterCreateWithAttributedString(string), CFRange(location: range.lowerBound, length: range.count)
        )
        return CGFloat(CTLineGetTypographicBounds(line, nil, nil, nil))
    }

    @Test func wideTextGetsTheLinesItNeeds() throws {
        let cjk = CodeText("日本語")
        let layout = measured([.code(cjk)], columns: 3)
        #expect(layout.codeColumns == 3)
        #expect(layout.breaks(cjk, limit: layout.codeLimit).count == 3)
        #expect(layout.height(ofRow: 0) == 3 * layout.lineHeight)

        let emoji = CodeText(String(repeating: "😀", count: 8))
        let wrapped = measured([.code(emoji)], columns: 10)
        let font = try #require(wrapped.measure?.font)
        let starts = wrapped.breaks(emoji, limit: wrapped.codeLimit)
        #expect(wrapped.height(ofRow: 0) == CGFloat(starts.count) * wrapped.lineHeight)
        for (index, start) in starts.enumerated() {
            let end = index + 1 < starts.count ? starts[index + 1] : emoji.length
            #expect(drawnWidth(emoji, start..<end, font: font) <= wrapped.codeLimit + 0.5)
        }

        let flat = measured([.code(emoji)], columns: 10, wrap: false)
        #expect(flat.contentSize.width >= flat.gutterWidth + drawnWidth(emoji, 0..<emoji.length, font: font))
    }

    /// Random mixes of ASCII, CJK, emoji (with ZWJ sequences and skin tones), combining marks,
    /// tabs and very wide glyphs, broken at many widths: every line fits, the lines join back
    /// into the shown text, no character is split, and the layout reserves exactly that many lines.
    @Test func breaksKeepEveryCharacterInsideTheRow() throws {
        let pieces = ["a", "Z", " ", "{", "\t", "日", "本語", "한", "😀", "👨‍👩‍👧", "👍🏽", "e\u{301}", "\u{FDFD}", "ß", "→"]
        var seed: UInt64 = 0x2545_F491_4F6C_DD1D
        func next(_ bound: Int) -> Int {
            seed = seed &* 6_364_136_223_846_793_005 &+ 1_442_695_040_888_963_407
            return Int((seed >> 33) % UInt64(bound))
        }
        let base = measured([], columns: 10)
        let font = try #require(base.measure?.font)
        for _ in 0..<150 {
            let source = (0..<(1 + next(60))).map { _ in pieces[next(pieces.count)] }.joined()
            for words in [false, true] {
                let shown = CodeText(source, clipped: next(4) == 0, words: words)
                #expect(shown.string.hasPrefix(CodeText.expandTabs(source, tabWidth: ReviewLimits.tabWidth).0))
                for columns in [8, 11, 17, 24, 40] {
                    let layout = measured([words ? .banner(shown) : .code(shown)], columns: columns)
                    let limit = words ? layout.bannerLimit : layout.codeLimit
                    let starts = layout.breaks(shown, limit: limit)
                    #expect(layout.lineCount(shown, limit: limit) == starts.count)
                    if !words { #expect(layout.height(ofRow: 0) == CGFloat(starts.count) * layout.lineHeight) }
                    var joined = ""
                    let units = Array(shown.string.utf16)
                    for (index, start) in starts.enumerated() {
                        let end = index + 1 < starts.count ? starts[index + 1] : shown.length
                        let index16 = String.Index(utf16Offset: start, in: shown.string)
                        #expect(index16.samePosition(in: shown.string) != nil, "split inside a character")
                        joined += String(decoding: units[start..<end], as: UTF16.self)
                        #expect(drawnWidth(shown, start..<end, font: font) <= limit + 0.5, "\(source) at \(columns)")
                    }
                    #expect(joined == shown.string)
                }
            }
        }
    }

    @Test func cellsFollowHorizontalScroll() throws {
        let scene = try #require(UIApplication.shared.connectedScenes.lazy.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 402, height: 874)
        let view = CodeLinesUIView(frame: window.bounds)
        let controller = UIViewController()
        controller.view = view
        window.rootViewController = controller
        window.isHidden = false
        defer { window.isHidden = true }
        let rows = CodeRow.file(TextFile(lines: ["short", String(repeating: "x", count: 400)]))
        view.update(rows: rows, wrap: false, showsOldNumbers: false, scrollTo: nil, onLongPress: nil)
        window.layoutIfNeeded()
        let collection = view.collectionView
        #expect(collection.contentSize.width > 1_000)
        collection.contentOffset.x = 300 - collection.adjustedContentInset.left
        collection.layoutIfNeeded()
        let cells = collection.visibleCells
        #expect(!cells.isEmpty)
        #expect(cells.allSatisfy { $0.frame.minX == 300 })
    }
}
