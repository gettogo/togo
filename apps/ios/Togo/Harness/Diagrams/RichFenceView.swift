import SwiftUI
import TogoKit
import UIKit

/// Shared by every markdown surface. Before completion, or on any failure, source stays usable.
struct RichFenceView: View {
    let fence: MarkdownFence
    var streaming = false
    @Environment(\.colorScheme) private var scheme
    @State private var width: CGFloat = 320
    @State private var visible = false
    @State private var image: UIImage?
    @State private var renderedKey: MermaidRenderer.Key?
    @State private var failed = false
    @State private var sourceShown = false
    @State private var selection: DiagramSelection?
    private var key: MermaidRenderer.Key { .init(fence.source, dark: scheme == .dark, width: width) }

    private var currentImage: UIImage? {
        guard visible else { return nil }
        if renderedKey == key { return image }
        switch fence.renderer { case .mermaid: return MermaidRenderer.shared.cached(key) }
    }
    private func renderedImage() async -> UIImage? {
        switch fence.renderer {
        case .mermaid: return await MermaidRenderer.shared.image(fence, dark: scheme == .dark, width: width)
        }
    }

    /// A stable slot while the diagram renders and when rows are reused or width changes.
    static func previewHeight(width: CGFloat) -> CGFloat { min(280, max(180, width * 0.62)) }

    var body: some View {
        Group {
            if streaming || failed {
                CodeWell(text: fence.source)
            } else {
                VStack(spacing: 0) {
                    HStack {
                        Text(DiagramCopy.title).font(.footnote.weight(.semibold))
                        Spacer(minLength: 8)
                        Button(sourceShown ? DiagramCopy.showDiagram : DiagramCopy.showSource) { sourceShown.toggle() }
                            .disabled(currentImage == nil)
                            .frame(minWidth: 44, minHeight: 44)
                        Button(DiagramCopy.copy, systemImage: "doc.on.doc") { copy() }
                            .labelStyle(.iconOnly)
                            .frame(minWidth: 44, minHeight: 44)
                    }
                    .font(.footnote)
                    .foregroundStyle(Color.togoSnow)
                    .buttonStyle(.plain)
                    .frame(minHeight: 44)
                    .padding(.horizontal, 12)
                    Group {
                        if let image = currentImage, !sourceShown {
                            Button { selection = DiagramSelection(image: image, source: fence.source) } label: {
                                Image(uiImage: image).resizable().scaledToFit().padding(8)
                                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                                    .contentShape(.rect)
                            }
                            .buttonStyle(.plain)
                            .accessibilityLabel(DiagramCopy.title)
                            .accessibilityHint(DiagramCopy.openHint)
                            .accessibilityValue(fence.source)
                        } else {
                            ScrollView(.vertical) { CodeWell(text: fence.source) }
                        }
                    }
                    .frame(height: Self.previewHeight(width: width))
                }
                .background(Color.togoDeepest, in: .rect(cornerRadius: 12))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { if $0.isFinite && $0 > 0 { width = $0 } }
        .onAppear { visible = true }
        .onDisappear { visible = false; image = nil; renderedKey = nil }
        .task(id: RenderID(key: key, streaming: streaming, renderer: fence.renderer, visible: visible)) {
            guard visible, !streaming else { return }
            let requested = key
            failed = false
            if let cached = MermaidRenderer.shared.cached(requested) { image = cached; renderedKey = requested }
            let rendered = await renderedImage()
            guard !Task.isCancelled else { return }
            image = rendered
            renderedKey = requested
            failed = rendered == nil
        }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didReceiveMemoryWarningNotification)) { _ in
            MermaidRenderer.shared.clearCache()
        }
        .fullScreenCover(item: $selection) { selected in
            DiagramViewer(image: selected.image, source: selected.source)
        }
        .transaction { $0.animation = nil }
    }

    private struct RenderID: Hashable { let key: MermaidRenderer.Key; let streaming: Bool; let renderer: MarkdownFence.Renderer; let visible: Bool }
    private struct DiagramSelection: Identifiable { let id = UUID(); let image: UIImage; let source: String }
    private func copy() { ChatPasteboard.copy(exact: fence.source); Haptic.done.play() }
}

enum DiagramCopy {
    static let title = String(localized: "diagram.title", defaultValue: "Diagram")
    static let showSource = String(localized: "diagram.source", defaultValue: "Show Source")
    static let showDiagram = String(localized: "diagram.image", defaultValue: "Show Diagram")
    static let copy = String(localized: "diagram.copy", defaultValue: "Copy Source")
    static let openHint = String(localized: "diagram.open", defaultValue: "Opens the diagram for zooming and panning.")
}
