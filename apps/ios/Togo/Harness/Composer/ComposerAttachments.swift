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
            case loading
            case uploading
            /// On the machine at `path`.
            case ready(path: String)
            case failed(String)
        }

        let id: UUID
        var preview: UIImage?
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
    typealias Load = @MainActor () async throws -> Data?

    private(set) var items: [Item] = []
    /// One quiet line about a picture that could not be read or did not fit.
    private(set) var notice: String?

    @ObservationIgnored private let upload: Upload?
    @ObservationIgnored private let prepare: Prepare
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private var sources: [UUID: Load] = [:]
    @ObservationIgnored private var tasks: [UUID: Task<Void, Never>] = [:]
    /// Two lanes bound photo acquisition, decoding and upload, even across clear/re-add.
    @ObservationIgnored private var tails: [Task<Void, Never>?] = [nil, nil]
    @ObservationIgnored private var nextLane = 0

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

    /// Selected media is still loading, preparing or uploading, so Send waits.
    var isUploading: Bool { items.contains { $0.state == .loading || $0.state == .uploading } }

    /// Every picture is on the machine and none failed: the prompt can go.
    var isReady: Bool { !items.isEmpty && items.allSatisfy { $0.path != nil } }

    /// The machine paths, in the order the pictures were added.
    var paths: [String] { items.compactMap(\.path) }

    /// Adds pictures in order, as many as there is room for, and starts their uploads.
    func add(_ images: [Data]) async {
        guard !Task.isCancelled else { return }
        let sources: [Load] = images.map { data in { data } }
        await enqueue(sources).value
    }

    /// Reserves the chips before returning to the UI. Send cannot slip between selection
    /// and the first asynchronous photo-library read.
    @discardableResult
    func enqueue(_ loads: [Load]) -> Task<Void, Never> {
        notice = nil
        guard available else { refuse(); return Task {} }
        let take = loads.prefix(room)
        if take.count < loads.count { notice = ComposerCopy.attachLimit(Self.limit) }
        var work: [Task<Void, Never>] = []
        for load in take {
            let id = UUID()
            sources[id] = load
            let item = Item(id: id, preview: nil, state: .loading, encoded: nil)
            // motion: cause → effect — the picked picture's chip grows in place as the glass makes room.
            withAnimation(Motion.animation(.resize, reduce: Self.reduceMotion)) { items.append(item) }
            work.append(start(id))
        }
        return Task { for task in work { await task.value } }
    }

    private func start(_ id: UUID) -> Task<Void, Never> {
        let lane = nextLane
        nextLane = (lane + 1) % tails.count
        let previous = tails[lane]
        let task = Task { [weak self] in
            await previous?.value
            guard let self else { return }
            defer { self.tasks[id] = nil }
            guard !Task.isCancelled, self.items.contains(where: { $0.id == id }) else { return }
            await self.process(id)
        }
        tasks[id] = task
        tails[lane] = task
        return task
    }

    private func process(_ id: UUID) async {
        if items.first(where: { $0.id == id })?.encoded != nil {
            await deliver(id)
            return
        }
        guard let load = sources[id] else { return }
        let data = try? await load()
        guard !Task.isCancelled, let at = items.firstIndex(where: { $0.id == id }) else { return }
        guard let data else {
            setState(.failed(ComposeCopy.loadFailed), at: at)
            return
        }
        let prepared = await prepare(data)
        guard !Task.isCancelled, let at = items.firstIndex(where: { $0.id == id }) else { return }
        guard let prepared else {
            notice = ComposerCopy.attachUnreadable
            setState(.failed(ComposerCopy.attachUnreadable), at: at)
            return
        }
        items[at].preview = prepared.preview
        items[at].encoded = prepared.encoded
        sources[id] = nil
        setState(.uploading, at: at)
        await deliver(id)
    }

    func remove(_ id: UUID) {
        tasks.removeValue(forKey: id)?.cancel()
        sources[id] = nil
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
        setState(items[at].encoded == nil ? .loading : .uploading, at: at)
        await start(id).value
    }

    /// A pasted file that is not a picture: uploaded as it is, its machine path returned for
    /// the prompt. Nil, with a line saying why, when it is too large or does not arrive.
    func upload(file name: String, data: Data) async -> String? {
        notice = nil
        guard let upload else {
            refuse()
            return nil
        }
        guard data.count <= Self.fileLimit else {
            notice = ComposerCopy.fileTooLarge
            return nil
        }
        do {
            return try await upload(name, data)
        } catch {
            notice = ComposerCopy.fileFailed(ServiceErrors.message(error))
            return nil
        }
    }

    /// A pasted picture or file with nowhere to go: the machine takes no uploads.
    func refuse() {
        notice = ComposerCopy.attachUnavailable
    }

    /// The prompt went: the pictures are part of it now.
    func clear() {
        for task in tasks.values { task.cancel() }
        tasks = [:]
        sources = [:]
        items = []
        notice = nil
    }

    func clearNotice() { notice = nil }

    private func deliver(_ id: UUID) async {
        guard let upload, let item = items.first(where: { $0.id == id }), let encoded = item.encoded else { return }
        do {
            let path = try await upload(fileName(encoded.fileExtension, index: items.firstIndex { $0.id == id } ?? 0), encoded.data)
            guard !Task.isCancelled, let at = items.firstIndex(where: { $0.id == id }) else { return }
            setState(.ready(path: path), at: at)
            // The chat shows the sent picture from here, without fetching it back.
            if let preview = item.preview { ChatImageLoader.shared.put(preview, path: path) }
        } catch {
            guard !Task.isCancelled, let at = items.firstIndex(where: { $0.id == id }) else { return }
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
