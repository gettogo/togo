#if DEBUG
import Foundation

/// Stands in for the network and WhisperKit: a download writes a small file, and the model it
/// loads answers with whatever text the test sets.
@MainActor final class FakeWhisperLoader: WhisperModelLoader {
    nonisolated let languages: Set<String> = ["en", "de", "fr", "ja"]
    var failure: (any Error)?
    /// Makes `load` throw, as a damaged model does.
    var loadFailure: (any Error)?
    /// Makes `load` wait until `finishLoading`.
    var holdsLoad = false
    let transcriber = FakeWhisperTranscriber()
    private(set) var downloads = 0
    private(set) var loads = 0
    /// Whether each load asked for the full check.
    private(set) var fullChecks: [Bool] = []
    private var gates: [CheckedContinuation<Void, Never>] = []

    func download(_ model: WhisperModel, into folder: URL, progress: @escaping @Sendable (Double) -> Void) async throws {
        downloads += 1
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        try Data("{}".utf8).write(to: folder.appending(path: "config.json"))
        progress(0.5)
        if let failure { throw failure }
        progress(1)
    }

    func load(_ model: WhisperModel, from folder: URL, fullCheck: Bool) async throws -> any WhisperTranscribing {
        loads += 1
        fullChecks.append(fullCheck)
        if holdsLoad { await withCheckedContinuation { gates.append($0) } }
        if let loadFailure { throw loadFailure }
        return transcriber
    }

    func finishLoading() {
        let waiting = gates
        gates = []
        for gate in waiting { gate.resume() }
    }
}

@MainActor final class FakeWhisperTranscriber: WhisperTranscribing {
    struct Call: Equatable {
        var samples: Int
        var language: String?
    }

    var text = ""
    private(set) var calls: [Call] = []

    func transcribe(_ samples: [Float], language: String?) async throws -> String {
        calls.append(Call(samples: samples.count, language: language))
        return text
    }
}

/// Counts every network fetch; it fetches nothing.
final class RecordingFetcher: WhisperFetching, @unchecked Sendable {
    private let lock = NSLock()
    private var fetched: [String] = []

    var calls: [String] {
        lock.withLock { fetched }
    }

    func fetch(_ file: WhisperModel.File, to destination: URL, written: @escaping @Sendable (Int64) -> Void) async throws {
        lock.withLock { fetched.append(file.path) }
        throw URLError(.notConnectedToInternet)
    }
}

/// Stands in for WhisperKit: loads only when the tokenizer's files are in the folder given.
final class RecordingRuntime: WhisperRuntime, @unchecked Sendable {
    private let lock = NSLock()
    private var loaded: [URL] = []
    /// Thrown after the tokenizer check, as a model that will not start does.
    var failure: (any Error)?

    var tokenizerFolders: [URL] {
        lock.withLock { loaded }
    }

    func load(models: URL, tokenizer: URL) async throws -> any WhisperTranscribing {
        lock.withLock { loaded.append(tokenizer) }
        guard FileManager.default.fileExists(atPath: tokenizer.appending(path: "tokenizer.json").path) else {
            throw WhisperLoadError.damaged
        }
        if let failure { throw failure }
        return SilentTranscriber()
    }
}

struct SilentTranscriber: WhisperTranscribing {
    func transcribe(_ samples: [Float], language: String?) async throws -> String { "" }
}

/// A microphone the test speaks into by the second.
@MainActor final class FakeWhisperMicrophone: WhisperMicrophone {
    var isDenied = false
    private(set) var isRunning = false
    private var samples: (@MainActor ([Float]) -> Void)?
    private var ended: (@MainActor (DictationError) -> Void)?

    func start(
        samples: @escaping @MainActor ([Float]) -> Void, level: @escaping @MainActor (Float) -> Void,
        ended: @escaping @MainActor (DictationError) -> Void
    ) async throws {
        if isDenied { throw DictationError.denied }
        isRunning = true
        self.samples = samples
        self.ended = ended
    }

    func stop() {
        isRunning = false
        samples = nil
        ended = nil
    }

    func hear(seconds: Int) {
        samples?([Float](repeating: 0.01, count: WhisperModel.sampleRate * seconds))
    }

    func interrupt(_ error: DictationError) {
        let ended = ended
        stop()
        ended?(error)
    }
}
#endif
