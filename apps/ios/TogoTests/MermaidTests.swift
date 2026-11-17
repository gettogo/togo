import CryptoKit
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor @Suite(.serialized) struct MermaidTests {
    private func fence(_ source: String) throws -> MarkdownFence {
        try #require(MarkdownFence(language: "mermaid", source: source, closed: true))
    }

    @Test func realAgentDiagramAndOtherCommonShapesRenderOffline() async throws {
        let url = try #require(Bundle(for: ResourceProbe.self).url(forResource: "mermaid-real-agent", withExtension: "md") ?? Bundle(for: ResourceProbe.self).url(forResource: "mermaid-real-agent", withExtension: "md", subdirectory: "Fixtures"))
        let markdown = try String(contentsOf: url, encoding: .utf8)
        guard case .richCode(let real) = MarkdownBlocks.parse(markdown).blocks.first else { Issue.record("Fixture fence lost"); return }
        let renderer = MermaidRenderer.shared
        let actual = try #require(await renderer.image(real, dark: true, width: 340), "Renderer stage: \(renderer.lastFailure ?? "none")")
        #expect(actual.size.width > 0 && actual.size.height > 0)
        #expect(await renderer.hasUnbrokenLabel(".cursor/hooks.json"), "File labels must occupy one SVG line, not split off their final character")
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.preferredRange = .standard
        let sample = UIGraphicsImageRenderer(size: CGSize(width: 64, height: 64), format: format).image { _ in
            actual.draw(in: CGRect(x: 0, y: 0, width: 64, height: 64))
        }
        if let png = actual.pngData() { try png.write(to: FileManager.default.temporaryDirectory.appending(path: "togo-mermaid-agent-proof.png")) }
        let pixels = try #require(sample.cgImage?.dataProvider?.data)
        #expect(Set((CFDataGetBytePtr(pixels)!).withMemoryRebound(to: UInt32.self, capacity: 4096) {
            Array(UnsafeBufferPointer(start: $0, count: 4096))
        }).count > 2, "The snapshot must contain the diagram, not a blank web view")
        #expect(await renderer.image(real, dark: true, width: 340) === actual, "Scroll reuse must hit the image cache")
        for source in ["flowchart TD\nsubgraph Work\nA[Start] --> B[Finish]\nend", "sequenceDiagram\nAlice->>Bob: Hello\nBob-->>Alice: Ready", "flowchart LR\n" + (0..<18).map { "N\($0)[Long workflow step \($0)] --> N\($0+1)" }.joined(separator: "\n")] {
            #expect(await renderer.image(try fence(source), dark: false, width: 700) != nil)
        }
    }

    @Test func invalidDiagramFailsQuietlyAndTheWorkerCanRenderAgain() async throws {
        let renderer = MermaidRenderer.shared
        #expect(await renderer.image(try fence("not a diagram %% { deliberately invalid"), dark: true, width: 340) == nil)
        #expect(await renderer.image(try fence("flowchart LR\nA --> B"), dark: true, width: 340) != nil, "Renderer stage: \(renderer.lastFailure ?? "none")")
    }

    @Test func bothParsersAndCopyKeepTheOriginalSource() throws {
        let source = "flowchart LR\n  A[Start] --> B[Finish]"
        let markdown = "``` Mermaid \n\(source)\n```"
        let rich = try fence(source)
        #expect(PrimitiveMarkdownBlock.parse(markdown) == [.richCode(rich)])
        #expect(PrimitiveMarkdownBlock.parse("```mermaid\n\(source)") == [.code(language: "mermaid", text: source)])
        #expect(RichMarkdown.plain(markdown) == source)
        #expect(RichMarkdown.html(markdown).contains("language-mermaid"))
        #expect(RichMarkdown.attributed(markdown, style: .pasteboard).string == source)
        #expect(ChatSelectable.segments(ChatMarkdown.blocks(markdown)) == [.richCode(rich)])
    }

    @Test func assetsAndPreviewAreBoundedAndIndependentOfHarness() throws {
        let assets = try #require(MermaidAssets())
        let html = try #require(String(data: assets.pageData, encoding: .utf8))
        #expect(html.contains("default-src 'none'") && html.contains("connect-src 'none'"))
        #expect(html.contains("img-src 'none'") && html.contains("frame-src 'none'"))
        #expect(MermaidAssets.rules.contains("block"))
        for width: CGFloat in [320, 402, 700, 1024] {
            let height = RichFenceView.previewHeight(width: width)
            #expect(height >= 180 && height <= 280)
        }
        #expect(MermaidRenderer.Key("A", dark: true, width: 340) != MermaidRenderer.Key("A", dark: false, width: 340))
        #expect(MermaidRenderer.Key("A", dark: true, width: 340) != MermaidRenderer.Key("B", dark: true, width: 340))
    }

    @Test func nativeViewerCanZoomPanAndKeepZoomWhenResized() {
        let format = UIGraphicsImageRendererFormat(); format.scale = 1
        let image = UIGraphicsImageRenderer(size: CGSize(width: 1000, height: 400), format: format).image { ctx in
            UIColor.black.setFill(); ctx.fill(CGRect(x: 0, y: 0, width: 1000, height: 400))
        }
        let scroll = DiagramScrollView(frame: CGRect(x: 0, y: 0, width: 320, height: 480))
        let delegate = DiagramZoom.Coordinator()
        scroll.delegate = delegate
        scroll.addSubview(scroll.picture)
        scroll.picture.image = image
        scroll.picture.frame = CGRect(origin: .zero, size: image.size)
        scroll.contentSize = image.size
        scroll.layoutIfNeeded()
        #expect(delegate.viewForZooming(in: scroll) === scroll.picture)
        #expect(scroll.minimumZoomScale < 1 && scroll.maximumZoomScale >= 8)
        scroll.setZoomScale(2, animated: false)
        scroll.layoutIfNeeded()
        #expect(scroll.contentSize.width > scroll.bounds.width)
        let relative = scroll.zoomScale / scroll.minimumZoomScale
        scroll.frame.size.width = 700
        scroll.layoutIfNeeded()
        #expect(abs(scroll.zoomScale / scroll.minimumZoomScale - relative) < 0.01)
    }

    @Test func longCodeKeepsItsFullHorizontalExtentAcrossLayouts() async throws {
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let code = String(repeating: "retire / newer / ", count: 24)
        for width: CGFloat in [320, 402, 700, 1024] {
            let view = UIHostingController(rootView: CodeWell(text: code).frame(width: width))
            let window = UIWindow(windowScene: scene)
            window.rootViewController = view
            window.frame = CGRect(x: 0, y: 0, width: width, height: 300)
            window.isHidden = false
            defer { window.isHidden = true; window.rootViewController = nil }
            view.view.frame = CGRect(x: 0, y: 0, width: width, height: 300)
            view.view.layoutIfNeeded()
            await Task.yield()
            view.view.layoutIfNeeded()
            func all(_ node: UIView) -> [UIView] { [node] + node.subviews.flatMap(all) }
            let text = all(view.view).compactMap { $0 as? UITextView }.first
            #expect(text?.text == code)
            #expect((text?.bounds.width ?? 0) > width, "The code must scroll, not be compressed to the viewport")
        }
    }
}
private final class ResourceProbe: NSObject {}
