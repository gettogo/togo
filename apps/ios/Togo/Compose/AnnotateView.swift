import Observation
import PencilKit
import SwiftUI
import UIKit

/// Marks on an image before it is sent: a pen and a highlighter, Undo, and Send.
struct AnnotateView: View {
    enum Tool: CaseIterable, Hashable {
        case pen, highlighter

        var title: String {
            switch self {
            case .pen: String(localized: "compose.annotate.pen", defaultValue: "Pen")
            case .highlighter: String(localized: "compose.annotate.highlighter", defaultValue: "Highlighter")
            }
        }

        var symbol: String {
            switch self {
            case .pen: "pencil.tip"
            case .highlighter: "highlighter"
            }
        }

        /// Ink colours belong to the picture, not the app's chrome, so they read on any screenshot.
        var ink: PKInkingTool {
            switch self {
            case .pen: PKInkingTool(.pen, color: .systemRed, width: 5)
            case .highlighter: PKInkingTool(.marker, color: .systemYellow, width: 24)
            }
        }
    }

    let image: UIImage
    let onCancel: () -> Void
    /// Called with the marked-up image, or nil when nothing was drawn.
    let onSend: (UIImage?) -> Void
    @State private var tool: Tool = .pen
    @State private var canvas = AnnotateCanvas()

    var body: some View {
        NavigationStack {
            GeometryReader { proxy in
                let frame = fittedFrame(in: proxy.size)
                ZStack(alignment: .topLeading) {
                    Image(uiImage: image)
                        .resizable()
                        .frame(width: frame.width, height: frame.height)
                        .accessibilityHidden(true)
                    CanvasRepresentable(canvas: canvas, tool: tool.ink)
                        .frame(width: frame.width, height: frame.height)
                        .accessibilityLabel(String(localized: "compose.annotate.canvas", defaultValue: "Image to mark up"))
                }
                .offset(x: frame.minX, y: frame.minY)
            }
            .padding(16)
            .background(Color.togoDeepest)
            .navigationTitle(String(localized: "compose.annotate.title", defaultValue: "Mark up"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(String(localized: "compose.annotate.cancel", defaultValue: "Cancel"), systemImage: "xmark", role: .cancel, action: onCancel)
                        .foregroundStyle(Color.togoSnow)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(ComposeCopy.send, systemImage: "arrow.up") { onSend(canvas.render(over: image)) }
                        .foregroundStyle(Color.togoSnow)
                }
                ToolbarItemGroup(placement: .bottomBar) {
                    ForEach(Tool.allCases, id: \.self) { option in
                        ToolButton(tool: option, selected: tool == option) { tool = option }
                    }
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    Button(String(localized: "compose.annotate.undo", defaultValue: "Undo"), systemImage: "arrow.uturn.backward") {
                        canvas.undo()
                    }
                    .foregroundStyle(canvas.canUndo ? Color.togoSnow : Color.togoMute)
                    .disabled(!canvas.canUndo)
                }
            }
        }
    }

    private func fittedFrame(in bounds: CGSize) -> CGRect {
        guard image.size.width > 0, image.size.height > 0 else { return .zero }
        let scale = min(bounds.width / image.size.width, bounds.height / image.size.height)
        let size = CGSize(width: image.size.width * scale, height: image.size.height * scale)
        return CGRect(
            x: (bounds.width - size.width) / 2, y: (bounds.height - size.height) / 2,
            width: size.width, height: size.height
        )
    }
}

/// Selected is a filled disc, unselected an outline, so the active tool reads without colour.
private struct ToolButton: View {
    let tool: AnnotateView.Tool
    let selected: Bool
    let choose: () -> Void

    var body: some View {
        Button(action: choose) {
            Image(systemName: tool.symbol)
                .font(.body.weight(.medium))
                .foregroundStyle(selected ? Color.togoInk : Color.togoSnow)
                .frame(width: 36, height: 36)
                .background {
                    if selected {
                        Circle().fill(Color.togoSnow)
                    } else {
                        Circle().strokeBorder(Color.togoLine, lineWidth: 1)
                    }
                }
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        // The fill moves to the chosen tool in place.
        .motion(.respond, value: selected)
        .accessibilityLabel(tool.title)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// The drawing and its undo steps, kept outside the canvas view so Undo works without the
/// responder chain's undo manager, which a full-screen cover does not reliably reach.
@Observable @MainActor final class AnnotateCanvas {
    private(set) var drawing = PKDrawing()
    private(set) var steps: [PKDrawing] = []
    /// Bumped when the model changes the drawing, so the canvas view knows to take it.
    private(set) var revision = 0
    @ObservationIgnored var size: CGSize = .zero

    var canUndo: Bool { !steps.isEmpty }

    func drew(_ next: PKDrawing) {
        steps.append(drawing)
        drawing = next
    }

    func undo() {
        guard let previous = steps.popLast() else { return }
        drawing = previous
        revision += 1
    }

    /// The preview at its own resolution, at most `ImageShrinker.annotateSide`, with the marks
    /// scaled up from the on-screen canvas.
    func render(over image: UIImage) -> UIImage? {
        guard !drawing.strokes.isEmpty, size.width > 0 else { return nil }
        let pixels = CGSize(width: image.size.width * image.scale, height: image.size.height * image.scale)
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        // PencilKit adapts ink to the current appearance; light keeps the colours as chosen.
        var marks = UIImage()
        UITraitCollection(userInterfaceStyle: .light).performAsCurrent {
            marks = drawing.image(from: CGRect(origin: .zero, size: size), scale: pixels.width / size.width)
        }
        return UIGraphicsImageRenderer(size: pixels, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: pixels))
            marks.draw(in: CGRect(origin: .zero, size: pixels))
        }
    }
}

private struct CanvasRepresentable: UIViewRepresentable {
    let canvas: AnnotateCanvas
    let tool: PKInkingTool

    func makeUIView(context: Context) -> PKCanvasView {
        let view = PKCanvasView()
        view.backgroundColor = .clear
        view.isOpaque = false
        // A finger draws too; this is a quick mark-up, not a Pencil-only sketch pad.
        view.drawingPolicy = .anyInput
        view.isScrollEnabled = false
        view.delegate = context.coordinator
        view.tool = tool
        return view
    }

    func updateUIView(_ view: PKCanvasView, context: Context) {
        view.tool = tool
        context.coordinator.canvas = canvas
        if context.coordinator.revision != canvas.revision {
            context.coordinator.revision = canvas.revision
            context.coordinator.applying = true
            view.drawing = canvas.drawing
            context.coordinator.applying = false
        }
        canvas.size = view.bounds.size
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(canvas: canvas)
    }

    @MainActor final class Coordinator: NSObject, PKCanvasViewDelegate {
        var canvas: AnnotateCanvas
        var revision = 0
        var applying = false

        init(canvas: AnnotateCanvas) {
            self.canvas = canvas
        }

        func canvasViewDrawingDidChange(_ view: PKCanvasView) {
            guard !applying else { return }
            canvas.size = view.bounds.size
            canvas.drew(view.drawing)
        }
    }
}
