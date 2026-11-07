import Foundation
import Observation
import SwiftUI
import TogoKit
import UIKit

/// Pictures waiting above the composer's field, like screenshots attached in a chat app. Each
/// one is shrunk with its metadata (location included) removed, uploaded to the session's
/// folder on the machine at once, and sent with the next prompt as its path. Send waits until
/// every upload is through.
@Observable @MainActor final class ComposerAttachments {
    /// The most pictures one prompt carries, as many as the helper takes.
    static let limit = 10

    struct Item: Identifiable, Equatable {
        enum State: Equatable {
            case uploading
            /// On the machine at `path`.
            case ready(path: String)
            case failed(String)
        }

        let id: UUID
        let preview: UIImage
        var state: State
        /// The bytes as sent, kept so Try again does not shrink twice.
        var encoded: ImageShrinker.Encoded?

        var path: String? { if case .ready(let path) = state { path } else { nil } }

        static func == (lhs: Item, rhs: Item) -> Bool { lhs.id == rhs.id && lhs.state == rhs.state }
    }

    /// A picked or pasted picture, shrunk: a screen-sized preview and the bytes to upload.
    struct Prepared {
        var preview: UIImage
        var encoded: ImageShrinker.Encoded
    }

    typealias Upload = @MainActor (_ name: String, _ data: Data) async throws -> String
    typealias Prepare = @MainActor (Data) async -> Prepared?

    private(set) var items: [Item] = []
    /// One quiet line about a picture that could not be read or did not fit.
    private(set) var notice: String?

    @ObservationIgnored private let upload: Upload?
    @ObservationIgnored private let prepare: Prepare
    @ObservationIgnored private let now: () -> Date

    init(upload: Upload?, prepare: @escaping Prepare = ComposerAttachments.shrink, now: @escaping () -> Date = Date.init) {
        self.upload = upload
        self.prepare = prepare
        self.now = now
    }

    /// The machine takes pictures: its helper serves `fs.upload`.
    var available: Bool { upload != nil }

    var isEmpty: Bool { items.isEmpty }
    var isFull: Bool { items.count >= Self.limit }
    var room: Int { max(0, Self.limit - items.count) }

    /// Still uploading, so Send waits.
    var isUploading: Bool { items.contains { $0.state == .uploading } }

    /// Every picture is on the machine and none failed: the prompt can go.
    var isReady: Bool { !items.isEmpty && items.allSatisfy { $0.path != nil } }

    /// The machine paths, in the order the pictures were added.
    var paths: [String] { items.compactMap(\.path) }

    /// Adds pictures in order, as many as there is room for, and starts their uploads.
    func add(_ images: [Data]) async {
        notice = nil
        let take = images.prefix(room)
        if take.count < images.count { notice = ComposerCopy.attachLimit(Self.limit) }
        var uploads: [Task<Void, Never>] = []
        // Order is fixed as each is prepared, before any upload ends.
        for data in take {
            guard let prepared = await prepare(data) else {
                notice = ComposerCopy.attachUnreadable
                continue
            }
            let id = UUID()
            let item = Item(id: id, preview: prepared.preview, state: .uploading, encoded: prepared.encoded)
            // motion: cause → effect — the picked picture's chip grows in place as the glass makes room.
            withAnimation(Motion.animation(.resize, reduce: Self.reduceMotion)) { items.append(item) }
            uploads.append(Task { await self.deliver(id) })
        }
        for upload in uploads { await upload.value }
    }

    func remove(_ id: UUID) {
        // motion: preventing a jump — the removed chip conceals and the strip closes its room.
        withAnimation(Motion.animation(.resize, reduce: Self.reduceMotion)) { items.removeAll { $0.id == id } }
    }

    private static var reduceMotion: Bool { UIAccessibility.isReduceMotionEnabled }

    /// A chip's state (uploading, ready, failed) crossfades in place.
    private func setState(_ state: Item.State, at index: Int) {
        // motion: state change — the chip's veil crossfades in place as the upload moves on.
        withAnimation(Motion.animation(.respond, reduce: Self.reduceMotion)) { items[index].state = state }
    }

    func retry(_ id: UUID) async {
        guard let at = items.firstIndex(where: { $0.id == id }), case .failed = items[at].state else { return }
        setState(.uploading, at: at)
        await deliver(id)
    }

    /// The prompt went: the pictures are part of it now.
    func clear() {
        items = []
        notice = nil
    }

    func clearNotice() { notice = nil }

    private func deliver(_ id: UUID) async {
        guard let upload, let item = items.first(where: { $0.id == id }), let encoded = item.encoded else { return }
        do {
            let path = try await upload(fileName(encoded.fileExtension, index: items.firstIndex { $0.id == id } ?? 0), encoded.data)
            guard let at = items.firstIndex(where: { $0.id == id }) else { return }
            setState(.ready(path: path), at: at)
            // The chat shows the sent picture from here, without fetching it back.
            ChatImageLoader.shared.put(item.preview, path: path)
        } catch {
            guard let at = items.firstIndex(where: { $0.id == id }) else { return }
            setState(.failed(ServiceErrors.message(error)), at: at)
        }
    }

    private func fileName(_ fileExtension: String, index: Int) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyyMMdd-HHmmss"
        return "image-\(formatter.string(from: now()))-\(index + 1).\(fileExtension)"
    }

    /// Shrinks under the upload limit with metadata removed, off the main actor.
    static func shrink(_ data: Data) async -> Prepared? {
        await prepareOff(data)
    }

    @concurrent private nonisolated static func prepareOff(_ data: Data) async -> Prepared? {
        guard let preview = ImageShrinker.preview(data), let encoded = ImageShrinker.fit(data) else { return nil }
        return Prepared(preview: preview, encoded: encoded)
    }
}
