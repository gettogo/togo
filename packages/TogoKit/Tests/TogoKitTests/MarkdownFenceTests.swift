import Testing
@testable import TogoKit

struct MarkdownFenceTests {
    @Test(arguments: ["mermaid", "Mermaid", " mermaid", "Mermaid "])
    func closedFencesSelectTheSharedRenderer(_ tag: String) {
        let source = "flowchart LR\n  A --> B"
        let blocks = MarkdownBlocks.parse("```\(tag)\n\(source)\n```").blocks
        guard case .richCode(let fence) = blocks.first else { Issue.record("Missing rich fence"); return }
        #expect(fence.renderer == .mermaid && fence.language == "mermaid" && fence.source == source)
        #expect(MarkdownFence(language: tag, source: source, closed: true, streaming: true) == nil)
    }

    @Test func unclosedUnknownOversizedAndConfigurationStayCode() {
        let source = "flowchart LR\nA --> B"
        #expect(MarkdownBlocks.parse("```mermaid\n\(source)").blocks == [.code(language: "mermaid", text: source)])
        #expect(MarkdownBlocks.parse("```unknown\n\(source)\n```").blocks == [.code(language: "unknown", text: source)])
        for code in [String(repeating: "x", count: MarkdownFenceRegistry.sourceLimit + 1), "%%{init: {}}%%\n\(source)", "---\nconfig: {}\n---\n\(source)"] {
            #expect(MarkdownFence(language: "mermaid", source: code, closed: true) == nil)
        }
    }

    @Test func onlyTheMatchingClosingFenceCompletesADiagram() {
        #expect(MarkdownFenceMarker.opening("````mermaid")?.closes("```") == false)
        #expect(MarkdownFenceMarker.opening("~~~mermaid")?.closes("```") == false)
        #expect(MarkdownFenceMarker.opening("```mermaid")?.closes("```python") == false)
        #expect(MarkdownFenceMarker.opening(" ``` mermaid ")?.closes(" ``` \r") == true)
        let full = "Intro\n````mermaid\nflowchart LR\nA --> B\n````\nDone"
        var parsed = MarkdownBlocks.parse("")
        for end in full.indices {
            let next = String(full[...end])
            parsed = parsed.continued(to: next)
            #expect(parsed.blocks == MarkdownBlocks.parse(next).blocks)
        }
    }
}
