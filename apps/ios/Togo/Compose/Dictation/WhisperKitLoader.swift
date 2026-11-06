import CryptoKit
import Foundation
import WhisperKit

/// Downloads a model's pinned files from Hugging Face and runs it with WhisperKit.
///
/// Only `download` uses the network, through `fetcher`. Loading checks the files on disk and
/// hands them to `runtime`, which reads the folder and nothing else.
struct WhisperKitLoader: WhisperModelLoader {
    var fetcher: any WhisperFetching = HubFetcher()
    var runtime: any WhisperRuntime = WhisperKitRuntime()

    var languages: Set<String> { Constants.languageCodes }

    func download(_ model: WhisperModel, into folder: URL, progress: @escaping @Sendable (Double) -> Void) async throws {
        let total = Double(max(model.bytes, 1))
        var done: Int64 = 0
        for file in model.files {
            try Task.checkCancellation()
            let destination = folder.appending(path: file.local)
            try FileManager.default.createDirectory(
                at: destination.deletingLastPathComponent(), withIntermediateDirectories: true
            )
            let before = done
            try await fetcher.fetch(file, to: destination) { written in
                progress(Double(before + written) / total)
            }
            try Self.verify(file, at: destination)
            done += file.bytes
            progress(Double(done) / total)
        }
    }

    /// Throws `WhisperLoadError.damaged` when a file is missing or changed, and `.failed` when
    /// the files are fine but the model would not load. Every file's size is checked each
    /// time; with `fullCheck` its SHA-256 too.
    func load(_ model: WhisperModel, from folder: URL, fullCheck: Bool) async throws -> any WhisperTranscribing {
        for file in model.files {
            let url = folder.appending(path: file.local)
            let size = (try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize).flatMap { $0 }
            guard size.map(Int64.init) == file.bytes else { throw WhisperLoadError.damaged }
            if fullCheck {
                do { try Self.verify(file, at: url) } catch { throw WhisperLoadError.damaged }
            }
        }
        do {
            return try await runtime.load(models: folder, tokenizer: folder.appending(path: WhisperModel.tokenizerFolder))
        } catch let error as WhisperLoadError {
            throw error
        } catch {
            throw WhisperLoadError.failed
        }
    }

    /// Throws unless the file has exactly the pinned size and SHA-256, and removes it if not.
    static func verify(_ file: WhisperModel.File, at url: URL) throws {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        var hasher = SHA256()
        var size: Int64 = 0
        while let chunk = try handle.read(upToCount: 1 << 20), !chunk.isEmpty {
            hasher.update(data: chunk)
            size += Int64(chunk.count)
        }
        let digest = hasher.finalize().map { String(format: "%02x", $0) }.joined()
        guard size == file.bytes, digest == file.sha256 else {
            try? FileManager.default.removeItem(at: url)
            throw WhisperLoadError.damaged
        }
    }
}

enum WhisperLoadError: Error, Equatable {
    /// A file is missing or differs from the pinned one, or the tokenizer would not load.
    case damaged
    /// The files are as downloaded but the model would not start, as under memory pressure.
    case failed
}

/// The network side of a download: one pinned file to a path on this device.
protocol WhisperFetching: Sendable {
    func fetch(_ file: WhisperModel.File, to destination: URL, written: @escaping @Sendable (Int64) -> Void) async throws
}

/// Fetches from Hugging Face in an ephemeral session: no cache, no cookies, no credentials.
struct HubFetcher: WhisperFetching {
    private let session: URLSession = {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil
        configuration.urlCredentialStorage = nil
        configuration.waitsForConnectivity = false
        return URLSession(configuration: configuration)
    }()

    func fetch(_ file: WhisperModel.File, to destination: URL, written: @escaping @Sendable (Int64) -> Void) async throws {
        let box = TaskBox()
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
                let task = session.downloadTask(with: file.url) { location, response, error in
                    // The temporary file is gone once this returns, so it is moved here.
                    let status = (response as? HTTPURLResponse)?.statusCode ?? 0
                    if let error {
                        continuation.resume(throwing: error)
                    } else if let location, status == 200 {
                        do {
                            try? FileManager.default.removeItem(at: destination)
                            try FileManager.default.moveItem(at: location, to: destination)
                            continuation.resume()
                        } catch {
                            continuation.resume(throwing: error)
                        }
                    } else {
                        continuation.resume(throwing: URLError(.badServerResponse))
                    }
                }
                box.start(task, written: written)
            }
        } onCancel: {
            box.cancel()
        }
    }
}

/// The running download task, so cancelling the Swift task cancels it, and its progress.
private final class TaskBox: @unchecked Sendable {
    private let lock = NSLock()
    private var task: URLSessionDownloadTask?
    private var observation: NSKeyValueObservation?
    private var cancelled = false

    func start(_ task: URLSessionDownloadTask, written: @escaping @Sendable (Int64) -> Void) {
        lock.lock()
        defer { lock.unlock() }
        self.task = task
        observation = task.observe(\.countOfBytesReceived) { task, _ in written(task.countOfBytesReceived) }
        if cancelled { task.cancel() } else { task.resume() }
    }

    func cancel() {
        lock.lock()
        defer { lock.unlock() }
        cancelled = true
        task?.cancel()
    }
}
