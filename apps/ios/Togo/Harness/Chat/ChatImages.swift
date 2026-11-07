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

/// Decoded chat pictures, screen-sized, kept in memory. A picture this phone uploaded is put
/// here under its path on the machine, so it shows without a round trip.
@MainActor final class ChatImageLoader {
    static let shared = ChatImageLoader()

    private let cache: NSCache<NSString, UIImage> = {
        let cache = NSCache<NSString, UIImage>()
        cache.countLimit = 80
        return cache
    }()

    func cached(_ ref: HarnessImage) -> UIImage? {
        Self.key(ref).flatMap { cache.object(forKey: $0 as NSString) }
    }

    func put(_ image: UIImage, path: String) {
        cache.setObject(image, forKey: path as NSString)
    }

    /// The picture from the cache, its inline bytes, or the machine; nil when none can say.
    func load(_ ref: HarnessImage, fetch: ChatImageFetch?) async -> UIImage? {
        if let hit = cached(ref) { return hit }
        var bytes = ref.data
        if bytes == nil, let path = ref.path, let fetch {
            bytes = try? await fetch.fetch(path)
        }
        guard let bytes, let image = await Self.decode(bytes) else { return nil }
        if let key = Self.key(ref) { cache.setObject(image, forKey: key as NSString) }
        return image
    }

    static func key(_ ref: HarnessImage) -> String? {
        if let path = ref.path { return path }
        return ref.data.map { "inline:\($0.count):\($0.hashValue)" }
    }

    @concurrent private nonisolated static func decode(_ data: Data) async -> UIImage? {
        ImageShrinker.preview(data)
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
        .fullScreenCover(item: $opened) { request in
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
    @Environment(\.chatImageFetch) private var fetch
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
        .clipShape(.rect(cornerRadius: 10, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Color.togoLine, lineWidth: 0.5)
        }
        .task(id: ChatImageLoader.key(ref)) {
            image = ChatImageLoader.shared.cached(ref)
            guard image == nil else { return }
            image = await ChatImageLoader.shared.load(ref, fetch: fetch)
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
