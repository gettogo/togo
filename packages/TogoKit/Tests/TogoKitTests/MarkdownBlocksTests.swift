import Foundation
import Testing
@testable import TogoKit

struct MarkdownBlocksTests {
    typealias B = MarkdownBlocks.Block

    @Test func splitsProseCodeAndHeadings() {
        let blocks = MarkdownBlocks.parse("Fixed it.\n```go\nfunc a() {}\n```\n- one\n## Next").blocks
        #expect(blocks == [.prose("Fixed it."), .code(language: "go", text: "func a() {}"), .prose("\u{2022} one"), .heading("Next")])
    }

    @Test func openFenceShowsAsCode() {
        #expect(MarkdownBlocks.parse("a\n```\nx").blocks == [.prose("a"), .code(language: nil, text: "x")])
    }

    /// Streaming a reply chunk by chunk gives the same blocks as parsing it whole, at every step.
    @Test func continuingMatchesAFullParseAtEveryStep() {
        let full = "Intro line\nmore\n\n# Head\n```swift\nlet a = 1\n\nlet b = 2\n```\n* item\n  - sub\nTail ```not a fence\n## End\n"
        let chars = Array(full)
        for step in [1, 2, 3, 5, 7] {
            var parsed = MarkdownBlocks.parse("")
            var i = 0
            while i < chars.count {
                i = min(chars.count, i + step)
                let text = String(chars[..<i])
                parsed = parsed.continued(to: text)
                #expect(parsed.blocks == MarkdownBlocks.parse(text).blocks, "step \(step) at \(i)")
            }
        }
    }

    @Test func continuingKeepsTheStableBlocks() {
        let first = MarkdownBlocks.parse("para one\n\n# H\nstart")
        let next = first.continued(to: "para one\n\n# H\nstart and more")
        #expect(next.blocks.prefix(2) == first.blocks.prefix(2))
        #expect(next.reparsedFrom == 2)
    }

    @Test func aTextThatIsNotAContinuationParsesWhole() {
        let first = MarkdownBlocks.parse("abc\n# x")
        let next = first.continued(to: "zzz")
        #expect(next.blocks == [.prose("zzz")])
        #expect(next.reparsedFrom == 0)
    }

    @Test func cacheFindsAnEarlierPrefix() {
        let cache = MarkdownBlocksCache(limit: 4)
        _ = cache.blocks("one\n# two\nthr")
        let grown = cache.blocks("one\n# two\nthree")
        #expect(grown == MarkdownBlocks.parse("one\n# two\nthree").blocks)
        #expect(cache.lastReparsedFrom == 2)
        _ = cache.blocks("one\n# two\nthree")
        #expect(cache.hits == 1)
    }

    @Test func cacheIsBounded() {
        let cache = MarkdownBlocksCache(limit: 2)
        for i in 0..<5 { _ = cache.blocks("text \(i)") }
        #expect(cache.count == 2)
    }
}
