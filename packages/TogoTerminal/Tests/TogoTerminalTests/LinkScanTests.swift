import Foundation
import Testing
@testable import TogoTerminal

@Suite struct LinkScanTests {
    private typealias Row = LinkScan.Row

    private func links(_ rows: [Row], columns: Int) -> [String] {
        LinkScan.links(in: rows, columns: columns).map(\.absoluteString)
    }

    @Test func rejoinsAURLHardBrokenAtTheMargin() {
        #expect(links([Row("https://a.dev/abcdef"), Row("ghij?x=1 done")], columns: 20) == ["https://a.dev/abcdefghij?x=1"])
    }

    @Test func leavesARowShortOfTheMarginAlone() {
        #expect(links([Row("https://a.dev/abc"), Row("defghi")], columns: 40) == ["https://a.dev/abc"])
    }

    @Test func joinsSoftWrappedRows() {
        #expect(links([Row("see https://a.dev/abcd"), Row("efgh", wrapped: true)], columns: 22) == ["https://a.dev/abcdefgh"])
    }

    @Test func rejoinsInsideABox() {
        let rows = [
            Row("╭──────────────────────────────╮"),
            Row("│ Open this link to sign in:   │"),
            Row("│ https://claude.ai/oauth/auth │"),
            Row("│ orize?code=true&state=xyz    │"),
            Row("╰──────────────────────────────╯"),
        ]
        #expect(links(rows, columns: 32) == ["https://claude.ai/oauth/authorize?code=true&state=xyz"])
    }

    @Test func rejoinsUnderAnIndent() {
        #expect(links([Row("  ⎿ https://x.dev/aaaaaaaa"), Row("    bbbb&c=d")], columns: 25) == ["https://x.dev/aaaaaaaabbbb&c=d"])
    }

    @Test func rejoinsAcrossThreeRows() {
        let rows = [Row("https://a.dev/0123"), Row("456789012345678901"), Row("234 end")]
        #expect(links(rows, columns: 18) == ["https://a.dev/0123456789012345678901234"])
    }

    @Test func aNewURLOnTheNextRowStandsAlone() {
        #expect(links([Row("https://a.dev/abcdefghij"), Row("https://b.dev/")], columns: 24)
            == ["https://b.dev/", "https://a.dev/abcdefghij"])
    }

    @Test func aRowStartingWithOtherTextIsNotJoined() {
        #expect(links([Row("https://a.dev/abcdefghij"), Row("> prompt")], columns: 24) == ["https://a.dev/abcdefghij"])
        #expect(links([Row("https://a.dev/ word"), Row("next")], columns: 19) == ["https://a.dev/"])
    }

    @Test func listsNewestFirstEachOnce() {
        let rows = [Row("a https://one.dev/ b"), Row("https://two.dev/x."), Row("again https://one.dev/")]
        #expect(links(rows, columns: 80) == ["https://one.dev/", "https://two.dev/x"])
    }

    @Test func keepsOnlyWebLinks() {
        #expect(links([Row("javascript://x file:///etc http://ok.dev")], columns: 80) == ["http://ok.dev"])
    }
}
