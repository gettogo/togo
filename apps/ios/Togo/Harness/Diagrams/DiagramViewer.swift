import SwiftUI
import UIKit

/// System navigation in every size class, native scroll-view zoom and pan in the content.
struct DiagramViewer: View {
    let image: UIImage
    let source: String
    @Environment(\.dismiss) private var dismiss
    @State private var sourceShown = false

    var body: some View {
        NavigationStack {
            Group {
                if sourceShown { ScrollView { CodeWell(text: source).padding(16) } }
                else { DiagramZoom(image: image).accessibilityLabel(DiagramCopy.title).accessibilityValue(source) }
            }
            .background(Color.togoDeepest)
            .navigationTitle(DiagramCopy.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(PromptCopy.done, systemImage: "checkmark", role: .confirm) { dismiss() }
                }
                ToolbarItemGroup(placement: .primaryAction) {
                    Button(sourceShown ? DiagramCopy.showDiagram : DiagramCopy.showSource) { sourceShown.toggle() }
                    Button(DiagramCopy.copy, systemImage: "doc.on.doc") { ChatPasteboard.copy(exact: source) }
                }
            }
        }
    }
}

struct DiagramZoom: UIViewRepresentable {
    let image: UIImage
    func makeUIView(context: Context) -> DiagramScrollView {
        let view = DiagramScrollView()
        view.delegate = context.coordinator
        view.picture.contentMode = .scaleAspectFit
        view.addSubview(view.picture)
        return view
    }
    func updateUIView(_ view: DiagramScrollView, context: Context) {
        guard view.picture.image !== image else { return }
        view.picture.image = image
        view.picture.frame = CGRect(origin: .zero, size: image.size)
        view.contentSize = image.size
        view.fittedSize = .zero
        view.setNeedsLayout()
    }
    func makeCoordinator() -> Coordinator { Coordinator() }
    final class Coordinator: NSObject, UIScrollViewDelegate {
        func viewForZooming(in scrollView: UIScrollView) -> UIView? { (scrollView as? DiagramScrollView)?.picture }
        func scrollViewDidZoom(_ scrollView: UIScrollView) { (scrollView as? DiagramScrollView)?.centerPicture() }
    }
}

final class DiagramScrollView: UIScrollView {
    let picture = UIImageView()
    var fittedSize = CGSize.zero
    override func layoutSubviews() {
        super.layoutSubviews()
        guard bounds.width > 0, bounds.height > 0, let image = picture.image else { return }
        if fittedSize != bounds.size {
            let oldMin = minimumZoomScale
            let relative = oldMin > 0 ? zoomScale / oldMin : 1
            let fit = min(bounds.width / image.size.width, bounds.height / image.size.height, 1)
            minimumZoomScale = fit
            maximumZoomScale = max(8, fit * 8)
            setZoomScale(min(maximumZoomScale, max(fit, fit * relative)), animated: false)
            fittedSize = bounds.size
        }
        centerPicture()
    }
    func centerPicture() {
        contentInset = UIEdgeInsets(top: max(0, (bounds.height - contentSize.height) / 2), left: max(0, (bounds.width - contentSize.width) / 2), bottom: 0, right: 0)
    }
}
