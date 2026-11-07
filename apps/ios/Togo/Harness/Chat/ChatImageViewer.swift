import SwiftUI
import TogoKit
import UIKit

/// The chat's pictures full screen: swipe between them, pinch or double-tap to zoom, share.
struct ChatImageViewer: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.chatImageFetch) private var fetch
    let images: [HarnessImage]
    @State var index: Int
    @State private var loaded: [Int: UIImage]
    @State private var failed: Set<Int> = []

    /// `preloaded` are pictures already on the phone, such as attachments not sent yet.
    init(images: [HarnessImage], index: Int, preloaded: [Int: UIImage] = [:]) {
        self.images = images
        _index = State(initialValue: index)
        _loaded = State(initialValue: preloaded)
    }

    var body: some View {
        NavigationStack {
            TabView(selection: $index) {
                ForEach(images.indices, id: \.self) { at in
                    page(at).tag(at)
                }
            }
            .tabViewStyle(.page(indexDisplayMode: images.count > 1 ? .automatic : .never))
            .background(Color.black.ignoresSafeArea())
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(ChatImageCopy.done, systemImage: "xmark") { dismiss() }
                }
                if let image = loaded[index] {
                    ToolbarItem(placement: .primaryAction) {
                        let shared = Image(uiImage: image)
                        ShareLink(item: shared, preview: SharePreview(ChatImageCopy.label(images[index], index: index, of: images.count), image: shared)) {
                            Label(ChatImageCopy.share, systemImage: "square.and.arrow.up")
                        }
                    }
                }
            }
            .toolbarBackgroundVisibility(.hidden, for: .navigationBar)
        }
        .preferredColorScheme(.dark)
    }

    @ViewBuilder private func page(_ at: Int) -> some View {
        Group {
            if let image = loaded[at] {
                ZoomableImage(image: image)
                    .accessibilityLabel(ChatImageCopy.label(images[at], index: at, of: images.count))
            } else if failed.contains(at) {
                ContentUnavailableView(ChatImageCopy.unavailable, systemImage: "photo")
                    .foregroundStyle(Color.togoMute)
            } else {
                ProgressView()
            }
        }
        // motion: state change — the spinner gives way to the picture (or why it can't show) in place.
        .motion(.fade, value: PageKey(loaded: loaded[at] != nil, failed: failed.contains(at)))
        .task {
            guard loaded[at] == nil else { return }
            if let image = await ChatImageLoader.shared.load(images[at], fetch: fetch) {
                loaded[at] = image
            } else {
                failed.insert(at)
            }
        }
    }
}

/// A picture in a scroll view that pinches from 1x to 4x and double-taps between the two.
private struct ZoomableImage: UIViewRepresentable {
    let image: UIImage

    func makeUIView(context: Context) -> UIScrollView {
        let scroll = UIScrollView()
        scroll.minimumZoomScale = 1
        scroll.maximumZoomScale = 4
        scroll.showsVerticalScrollIndicator = false
        scroll.showsHorizontalScrollIndicator = false
        scroll.contentInsetAdjustmentBehavior = .never
        scroll.delegate = context.coordinator
        let view = UIImageView(image: image)
        view.contentMode = .scaleAspectFit
        view.isAccessibilityElement = false
        view.frame = scroll.bounds
        view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        scroll.addSubview(view)
        context.coordinator.imageView = view
        let tap = UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.doubleTap(_:)))
        tap.numberOfTapsRequired = 2
        scroll.addGestureRecognizer(tap)
        return scroll
    }

    func updateUIView(_ scroll: UIScrollView, context: Context) {
        context.coordinator.imageView?.image = image
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    final class Coordinator: NSObject, UIScrollViewDelegate {
        weak var imageView: UIImageView?

        func viewForZooming(in scrollView: UIScrollView) -> UIView? { imageView }

        @objc func doubleTap(_ gesture: UITapGestureRecognizer) {
            guard let scroll = gesture.view as? UIScrollView else { return }
            if scroll.zoomScale > 1 {
                scroll.setZoomScale(1, animated: !UIAccessibility.isReduceMotionEnabled)
            } else {
                let point = gesture.location(in: imageView)
                let size = CGSize(width: scroll.bounds.width / 2.5, height: scroll.bounds.height / 2.5)
                let rect = CGRect(origin: CGPoint(x: point.x - size.width / 2, y: point.y - size.height / 2), size: size)
                scroll.zoom(to: rect, animated: !UIAccessibility.isReduceMotionEnabled)
            }
        }
    }
}

/// One driver for a page's crossfade: loaded or failed, whichever changed.
private struct PageKey: Equatable {
    var loaded: Bool
    var failed: Bool
}
