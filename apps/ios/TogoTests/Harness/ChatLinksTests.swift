import Foundation
import Testing
@testable import Togo

/// The chat's links (owner #6, IMG_0452): the file reference in a code span, the markdown link
/// and the bare URL each become a link, and the router sends each to the right place.
@MainActor
struct ChatLinksTests {
    private func links(_ text: AttributedString) -> [(text: String, url: URL)] {
        text.runs.compactMap { run in
            run.link.map { (String(text[run.range].characters), $0) }
        }
    }

    @Test func theThreeLinksOfIMG0452() {
        let reply = "- File link: `web/LICENSE:1`\n- Web link: [Claude Code](https://claude.com/claude-code)\n"
            + "- Plain URL: https://claude.com/claude-code"
        let found = links(ChatLinks.markdown(reply))
        #expect(found.count == 3)
        #expect(found[0].text == "web/LICENSE:1")
        #expect(ChatLinks.route(found[0].url) == .file(path: "web/LICENSE", line: 1, changed: false))
        #expect(found[1].text == "Claude Code")
        #expect(ChatLinks.route(found[1].url) == .web(URL(string: "https://claude.com/claude-code")!))
        #expect(found[2].text == "https://claude.com/claude-code")
        #expect(ChatLinks.route(found[2].url) == .web(URL(string: "https://claude.com/claude-code")!))
    }

    @Test func linksAreUnderlined() {
        let text = ChatLinks.markdown("see https://example.com")
        let link = text.runs.first { $0.link != nil }
        #expect(link?.underlineStyle == .single)
    }

    @Test func aURLInACodeSpanIsNotALink() {
        #expect(links(ChatLinks.markdown("run `curl https://example.com/x`")).isEmpty)
    }

    @Test func codeSpansThatAreNotFilesStayText() {
        #expect(links(ChatLinks.markdown("`user.name` and `npm test` and `v1.2`")).isEmpty)
        #expect(links(ChatLinks.markdown("`README.md`")).count == 1)
        #expect(links(ChatLinks.markdown("`src/app.ts:12:4`")).map(\.text) == ["src/app.ts:12:4"])
    }

    @Test func proseFindsOnlyClearFileReferences() {
        #expect(links(ChatLinks.markdown("and/or, Node.js, e.g. this")).isEmpty)
        let found = links(ChatLinks.markdown("I changed src/app.ts, then web/LICENSE:3."))
        #expect(found.map(\.text) == ["src/app.ts", "web/LICENSE:3"])
        #expect(ChatLinks.route(found[1].url) == .file(path: "web/LICENSE", line: 3, changed: false))
    }

    @Test func promptMentionsLink() {
        let found = links(ChatLinks.prompt("look at @web/LICENSE, and mail sam@example.com"))
        #expect(found.map(\.text) == ["@web/LICENSE"])
        #expect(ChatLinks.route(found[0].url) == .file(path: "web/LICENSE", line: nil, changed: false))
    }

    @Test func codeWellsLinkURLsAndPaths() {
        let found = links(ChatLinks.plain("see https://x.dev/a and ./scripts/run.sh:2"))
        #expect(found.map(\.text) == ["https://x.dev/a", "./scripts/run.sh:2"])
    }

    @Test func routerDecisions() {
        #expect(ChatLinks.route(URL(string: "mailto:sam@example.com")!) == .system(URL(string: "mailto:sam@example.com")!))
        let changed = ChatLinks.fileURL(path: "/Users/sam/src/app/web/a b.ts", line: nil, changed: true)
        #expect(ChatLinks.route(changed) == .file(path: "/Users/sam/src/app/web/a b.ts", line: nil, changed: true))
    }

    @Test func pathsResolveAgainstTheSessionFolder() {
        #expect(ChatLinks.sessionPath("/Users/sam/src/app/web/LICENSE", cwd: "/Users/sam/src/app") == "web/LICENSE")
        #expect(ChatLinks.sessionPath("./web/LICENSE", cwd: "/Users/sam/src/app") == "web/LICENSE")
        #expect(ChatLinks.sessionPath("/etc/hosts", cwd: "/Users/sam/src/app") == "/etc/hosts")
        let changes = ["app/web/LICENSE", "app/src/a.ts"]
        #expect(ChatLinks.changedPath("web/LICENSE", cwd: "/Users/sam/src/app", changes: changes) == "app/web/LICENSE")
        #expect(ChatLinks.changedPath("/Users/sam/src/app/src/a.ts", cwd: "/Users/sam/src/app", changes: changes) == "app/src/a.ts")
        #expect(ChatLinks.changedPath("README.md", cwd: "/Users/sam/src/app", changes: changes) == nil)
    }
}
