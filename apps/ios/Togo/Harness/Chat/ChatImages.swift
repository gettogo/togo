import SwiftUI
import TogoKit
import UIKit

/// How the chat fetches a picture it only knows by path (`fs.image`); nil where the machine's
/// helper cannot serve one, so the thumbnail says what it is instead.
struct ChatImageFetch {
    let fetch: @MainActor (String) async throws -> Data
}

extension EnvironmentValues {
    @Entry var chatImageFetch: ChatImageFetch?
}

/// Decoded chat pictures kept in memory: thumbnails at the size they are drawn, and a few
/// screen-sized ones for the viewer. A picture this phone uploaded is put here under its path
/// on the machine, so it shows without a round trip. Both caches are bounded by decoded bytes.
@MainActor final class ChatImageLoader {
    static let shared = ChatImageLoader()
    /// The longest side a chat thumbnail is decoded at, in pixels.
    static let thumbnailLimit = 600

    private let thumbs: NSCache<NSString, UIImage> = {
        let cache = NSCache<NSString, UIImage>()
        cache.countLimit = 200
        cache.totalCostLimit = 48 << 20
        return cache
    }()

    private let full: NSCache<NSString, UIImage> = {
        let cache = NSCache<NSString, UIImage>()
        cache.countLimit = 12
        cache.totalCostLimit = 96 << 20
        return cache
    }()

    private var observer: NSObjectProtocol?

    init() {
        observer = NotificationCenter.default.addObserver(
            forName: UIApplication.didReceiveMemoryWarningNotification, object: nil, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.thumbs.removeAllObjects()
                self?.full.removeAllObjects()
            }
        }
    }

    /// A thumbnail when one is held, else the screen-sized picture; `maxSide` nil asks for the
    /// screen-sized one only.
    func cached(_ ref: HarnessImage, maxSide: Int? = nil) -> UIImage? {
        guard let key = Self.key(ref) else { return nil }
        if maxSide != nil, let thumb = thumbs.object(forKey: key as NSString) { return thumb }
        return full.object(forKey: key as NSString)
    }

    func put(_ image: UIImage, path: String) {
        full.setObject(image, forKey: path as NSString, cost: Self.cost(image))
    }

    /// The picture from the cache, its inline bytes, or the machine; nil when none can say.
    /// With `maxSide` it is decoded no larger than that, for a thumbnail.
    func load(_ ref: HarnessImage, fetch: ChatImageFetch?, maxSide: Int? = nil) async -> UIImage? {
        if let hit = cached(ref, maxSide: maxSide) { return hit }
        var bytes = ref.data
        if bytes == nil, let path = ref.path, let fetch {
            bytes = try? await fetch.fetch(path)
        }
        guard let bytes, let image = await Self.decode(bytes, maxSide: maxSide) else { return nil }
        if let key = Self.key(ref) {
            (maxSide == nil ? full : thumbs).setObject(image, forKey: key as NSString, cost: Self.cost(image))
        }
        return image
    }

    static func key(_ ref: HarnessImage) -> String? {
        if let path = ref.path { return path }
        return ref.data.map { "inline:\($0.count):\($0.hashValue)" }
    }

    /// The pixels to decode for a thumbnail `points` long on its longer side.
    static func thumbnailPixels(points: CGFloat, scale: CGFloat) -> Int {
        min(thumbnailLimit, Int((points * scale).rounded(.up)))
    }

    private static func cost(_ image: UIImage) -> Int {
        guard let cg = image.cgImage else { return 1 }
        return cg.bytesPerRow * cg.height
    }

    @concurrent private nonisolated static func decode(_ data: Data, maxSide: Int?) async -> UIImage? {
        if let maxSide { return ImageShrinker.preview(data, maxSide: maxSide) }
        return ImageShrinker.preview(data)
    }
}

/// A prompt's or a step's pictures: one shown large, several as a grid of squares. Tapping one
/// opens them full screen, where they zoom and share.
struct ChatImageGrid: View {
    let images: [HarnessImage]
    var alignment: HorizontalAlignment = .leading
    @State private var opened: ImageViewerRequest?

    var body: some View {
        let shown = Array(images.prefix(9))
        Group {
            if shown.count == 1 {
                thumbnail(0, side: 200)
            } else {
                LazyVGrid(columns: Array(repeating: GridItem(.fixed(88), spacing: 6), count: min(3, shown.count)), spacing: 6) {
                    ForEach(shown.indices, id: \.self) { index in
                        thumbnail(index, side: 88)
                    }
                }
                .fixedSize()
            }
        }
        .frame(maxWidth: .infinity, alignment: Alignment(horizontal: alignment, vertical: .center))
        .presentCover("chat.image", item: $opened) { request in
            ChatImageViewer(images: images, index: request.index)
        }
    }

    private func thumbnail(_ index: Int, side: CGFloat) -> some View {
        Button { opened = ImageViewerRequest(index: index) } label: {
            ChatThumbnail(ref: images[index], side: side, square: images.count > 1)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(ChatImageCopy.label(images[index], index: index, of: images.count))
        .accessibilityHint(ChatImageCopy.openHint)
    }
}

struct ImageViewerRequest: Identifiable {
    var index: Int
    var id: Int { index }
}

/// One picture at thumbnail size: the image once loaded, else a quiet tile saying what it is.
struct ChatThumbnail: View {
    /// One driver for the crossfade: loaded or failed, whichever changed.
    private struct ThumbKey: Equatable {
        var loaded: Bool
        var failed: Bool
    }

    @Environment(\.chatImageFetch) private var fetch
    @Environment(\.displayScale) private var displayScale
    let ref: HarnessImage
    let side: CGFloat
    var square = true
    @State private var image: UIImage?
    @State private var failed = false

    var body: some View {
        Group {
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .aspectRatio(contentMode: square ? .fill : .fit)
                    .frame(width: square ? side : nil, height: square ? side : nil)
                    .frame(maxWidth: square ? side : side * 1.4, maxHeight: side)
            } else {
                tile
            }
        }
        // motion: state change — the picture crossfades over its placeholder in the same frame, nothing moves.
        .motion(.respond, value: ThumbKey(loaded: image != nil, failed: failed))
        .clipShape(.rect(cornerRadius: 10, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Color.togoLine, lineWidth: 0.5)
        }
        .task(id: ChatImageLoader.key(ref)) {
            // A square crop fills its side with the picture's short side (up to 2:1 stays sharp);
            // a single picture shows up to 1.4 × its side across.
            let pixels = ChatImageLoader.thumbnailPixels(points: square ? side * 2 : side * 1.4, scale: displayScale)
            image = ChatImageLoader.shared.cached(ref, maxSide: pixels)
            guard image == nil else { return }
            image = await ChatImageLoader.shared.load(ref, fetch: fetch, maxSide: pixels)
            failed = image == nil
        }
    }

    private var tile: some View {
        VStack(spacing: 4) {
            if failed {
                Image(systemName: "photo")
                    .font(.title3)
                if let size = ChatImageCopy.size(ref) {
                    Text(verbatim: size).font(.caption2).monospacedDigit()
                }
            } else {
                ProgressView().controlSize(.small)
            }
        }
        .foregroundStyle(Color.togoMute)
        .frame(width: side, height: square ? side : side * 0.75)
        .background(Color.togoRaised)
    }
}

enum ChatImageCopy {
    static let image = String(localized: "chat.image", defaultValue: "Image")
    static let openHint = String(localized: "chat.image.open", defaultValue: "Opens full screen")
    static let done = String(localized: "chat.image.done", defaultValue: "Done")
    static let share = String(localized: "chat.image.share", defaultValue: "Share")
    static let unavailable = String(localized: "chat.image.unavailable", defaultValue: "This image cannot be shown here")

    static func label(_ ref: HarnessImage, index: Int, of count: Int) -> String {
        let name = ref.path.map { ($0 as NSString).lastPathComponent }
        let base = name.map { "\(image), \($0)" } ?? image
        return count > 1 ? "\(base), \(index + 1) of \(count)" : base
    }

    static func size(_ ref: HarnessImage) -> String? {
        ref.bytes.map { ByteCountFormatter.string(fromByteCount: Int64($0), countStyle: .file) }
    }
}
