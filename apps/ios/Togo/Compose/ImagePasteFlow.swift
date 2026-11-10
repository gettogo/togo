import Foundation
import Observation
import TogoKit
import UIKit

/// Whether the session's machine can take images, from its helper's `hello` capabilities.
enum ImageSupport: Sendable {
    /// No helper on the machine, so there is nothing to offer.
    case none
    /// A helper without `upload.v1`.
    case unsupported
    case available
}

/// An image waiting in the annotate screen. Compared by identity: two pastes of the same
/// picture are still two attempts.
struct PendingImage: Identifiable, Equatable {
    let id = UUID()
    /// At most `ImageShrinker.annotateSide`, so marking up never holds a full-size bitmap.
    let preview: UIImage
    /// The bytes as picked; an image sent without marks is shrunk from these, not the preview.
    let original: Data

    static func == (lhs: PendingImage, rhs: PendingImage) -> Bool {
        lhs.id == rhs.id
    }
}

/// From a pasted or picked image to its path typed into the session: annotate, shrink,
/// upload through the helper, paste the returned path.
@Observable @MainActor final class ImagePasteFlow {
    enum Phase: Equatable {
        case idle
        case annotating(PendingImage)
        case uploading
        case failed(detail: String)
        case loadFailed
        case unreadable
        case tooLarge
        case unsupported
        /// The machine has no helper at all, so there is nothing to send to.
        case noHelper

        var message: String? {
            switch self {
            case .idle, .annotating, .uploading: nil
            case .failed: ComposeCopy.uploadFailed
            case .loadFailed: ComposeCopy.loadFailed
            case .unreadable: ComposeCopy.unreadable
            case .tooLarge: ComposeCopy.tooLarge
            case .unsupported: ComposeCopy.unsupported
            case .noHelper: ComposeCopy.noHelper
            }
        }
    }

    typealias Upload = @MainActor (_ name: String, _ data: Data) async throws -> String
    typealias Load = @MainActor () async throws -> Data?

    let support: ImageSupport
    private(set) var phase: Phase
    private let upload: Upload
    private let paste: @MainActor (String) -> Void
    private let now: () -> Date
    /// The encoded image of a failed upload, so Try again does not shrink it a second time.
    private var pending: ImageShrinker.Encoded?
    /// How the last picked photo was fetched, so Try again can fetch it again.
    private var loader: Load?

    init(
        support: ImageSupport, phase: Phase = .idle, upload: @escaping Upload,
        paste: @escaping @MainActor (String) -> Void, now: @escaping () -> Date = Date.init
    ) {
        self.support = support
        self.phase = phase
        self.upload = upload
        self.paste = paste
        self.now = now
    }

    /// The Attach key opens the photo picker only where the picture can go.
    var picksPhotos: Bool { support == .available }

    var isAnnotating: Bool {
        if case .annotating = phase { true } else { false }
    }

    var annotating: PendingImage? {
        if case .annotating(let image) = phase { image } else { nil }
    }

    /// Try again only does something after a failed upload or photo load; each keeps what it
    /// needs, the encoded bytes or the loader.
    var canRetry: Bool {
        switch phase {
        case .failed, .loadFailed: true
        default: false
        }
    }

    func begin(_ data: Data) async {
        guard canBegin else { return }
        guard let preview = await Self.preview(data) else {
            phase = .unreadable
            return
        }
        phase = .annotating(PendingImage(preview: preview, original: data))
    }

    /// A picked photo can fail to arrive, for example when its iCloud original cannot download.
    func load(_ fetch: @escaping Load) async {
        guard canBegin else { return }
        loader = fetch
        guard let data = try? await fetch() else {
            phase = .loadFailed
            return
        }
        loader = nil
        await begin(data)
    }

    /// `annotated` is the preview with marks drawn on it, or nil when nothing was drawn.
    func send(annotated: UIImage?) async {
        guard case .annotating(let image) = phase else { return }
        phase = .uploading
        let source: ShrinkSource = if let marked = annotated?.cgImage { .image(marked) } else { .data(image.original) }
        guard let encoded = await Self.shrink(source) else {
            phase = .tooLarge
            return
        }
        pending = encoded
        await deliver(encoded)
    }

    func retry() async {
        switch phase {
        case .failed:
            guard let pending else { return }
            phase = .uploading
            await deliver(pending)
        case .loadFailed:
            guard let loader else { return }
            phase = .idle
            await load(loader)
        default:
            return
        }
    }

    /// The Attach key stays on the bar without `upload.v1` or a helper, so tapping it can say
    /// why it does nothing.
    func explainUnsupported() {
        switch support {
        case .unsupported: phase = .unsupported
        case .none: phase = .noHelper
        case .available: break
        }
    }

    func dismiss() {
        pending = nil
        loader = nil
        phase = .idle
    }

    private var canBegin: Bool {
        switch support {
        // A pasted image can still arrive without a photo button, so it is told why.
        case .none:
            phase = .noHelper
            return false
        case .unsupported:
            phase = .unsupported
            return false
        case .available:
            return phase != .uploading
        }
    }

    private func deliver(_ encoded: ImageShrinker.Encoded) async {
        do {
            let path = try await upload(fileName(encoded.fileExtension), encoded.data)
            // Quoted so a path with spaces stays one word; the space lets the user keep typing.
            paste(shellQuote([path]) + " ")
            dismiss()
        } catch {
            phase = .failed(detail: ServiceErrors.message(error))
        }
    }

    private func fileName(_ fileExtension: String) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyyMMdd-HHmmss"
        return "image-\(formatter.string(from: now())).\(fileExtension)"
    }

    private enum ShrinkSource: Sendable {
        case data(Data)
        case image(CGImage)
    }

    // Decoding and re-encoding a 12 MP photo take long enough to stall frames, so both run
    // off the main actor.

    @concurrent private nonisolated static func shrink(_ source: ShrinkSource) async -> ImageShrinker.Encoded? {
        switch source {
        case .data(let data): ImageShrinker.fit(data)
        case .image(let image): ImageShrinker.fit(image)
        }
    }

    @concurrent private nonisolated static func preview(_ data: Data) async -> UIImage? {
        ImageShrinker.preview(data)
    }
}
