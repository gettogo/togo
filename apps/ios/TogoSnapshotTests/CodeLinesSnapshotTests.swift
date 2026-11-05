import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor
struct CodeLinesSnapshotTests {
    private func file(_ diff: String) -> DiffFile {
        DiffParser.parse(GitDiff(diff: diff, truncated: false, binary: [])).first!
    }

    /// Tokens for one line on its own; enough for fixtures without block comments.
    private func tokens(_ language: Language) -> (DiffLine) -> [TokenSpan] {
        { line in
            var state = Tokenizer.State()
            return Tokenizer.line(line.text, language: language, state: &state)
        }
    }

    private var tsFiles: [DiffFile] {
        [ReviewFixtures.routerDiff, ReviewFixtures.itemsDiff, ReviewFixtures.renameDiff].map(file)
    }

    @Test func unifiedWithWordHighlights() {
        let rows = tsFiles.flatMap { CodeRow.unified($0, tokens: tokens(.typescript)) }
        assertScreen(named: "unified") {
            CodeLinesView(rows: rows, wrap: false, showsOldNumbers: true)
        }
    }

    @Test func longLineClippedWithMarker() {
        let long = String(repeating: "a=b+1;", count: 334).prefix(ReviewLimits.maxColumns)
        let text = TextFile(
            lines: [
                "{", "\t\"name\": \"api\",", "\t\"title\": \"日本語のタイトル\",", String(long), "\t\"ok\": true", "}",
            ],
            clipped: [3]
        )
        let rows = CodeRow.file(text, tokens: Tokenizer.tokenize(text.lines, language: .json))
        assertScreen(named: "clipped") {
            ScrolledToEnd(rows: rows)
        }
    }

    @Test func hunkHeadersAndNotices() {
        let crlf = file("""
        diff --git a/notes.txt b/notes.txt
        --- a/notes.txt
        +++ b/notes.txt
        @@ -1,3 +1,3 @@ Notes
         check the 404 path\r
        -then the pagination test\r
        +then the pagination test
         done\r

        """)
        let rows = [CodeRow.notice("Showing the first 512 KB of this diff.")]
            + CodeRow.unified(file(ReviewFixtures.itemsDiff), tokens: tokens(.typescript))
            + CodeRow.unified(crlf)
            + [.notice("Binary file, not shown")]
        assertScreen(named: "hunks-notices") {
            CodeLinesView(rows: rows, wrap: false, showsOldNumbers: true)
        }
    }

    /// Side by side needs a wide window, so this one renders at iPad width.
    @Test func splitPairOnIPad() {
        let rows = tsFiles.flatMap { CodeRow.split($0, tokens: tokens(.typescript)) }
        for variant in ScreenVariant.allCases {
            let view = CodeLinesView(rows: rows, wrap: false, showsOldNumbers: false)
                .background(Color.togoInk)
                .environment(\.colorScheme, variant.colorScheme)
            let traits = UITraitCollection { traits in
                traits.userInterfaceStyle = variant.style
                traits.preferredContentSizeCategory = variant.largeText ? .accessibilityExtraExtraExtraLarge : .large
            }
            assertSnapshot(
                of: UIHostingController(rootView: view),
                as: .image(on: .iPadPro11(.landscape), precision: 0.99, perceptualPrecision: 0.98, traits: traits),
                named: "split-\(variant.rawValue)"
            )
        }
    }
}

/// Shows the code scrolled to the far right, where a clipped line ends in its marker.
private struct ScrolledToEnd: UIViewRepresentable {
    var rows: [CodeRow]

    func makeUIView(context: Context) -> EndScrolledLines {
        EndScrolledLines()
    }

    func updateUIView(_ view: EndScrolledLines, context: Context) {
        view.update(rows: rows, wrap: false, showsOldNumbers: false, scrollTo: nil, onLongPress: nil)
    }
}

private final class EndScrolledLines: CodeLinesUIView {
    override func layoutSubviews() {
        super.layoutSubviews()
        let collection = collectionView
        collection.layoutIfNeeded()
        let end = collection.contentSize.width + collection.adjustedContentInset.right - collection.bounds.width
        let x = max(-collection.adjustedContentInset.left, end)
        guard collection.contentOffset.x != x else { return }
        collection.contentOffset.x = x
        collection.layoutIfNeeded()
    }
}
