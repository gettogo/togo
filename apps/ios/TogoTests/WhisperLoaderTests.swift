import Foundation
import Testing
import TogoKit
@testable import Togo

/// Loading a downloaded model reads its folder and nothing else; anything wrong with the files
/// fails in words and leaves dictation on Apple's speech.
@MainActor
struct WhisperLoaderTests {
    private let root = FileManager.default.temporaryDirectory
        .appending(path: "whisper-loader-\(UUID().uuidString)", directoryHint: .isDirectory)
    private let fetcher = RecordingFetcher()
    private let runtime = RecordingRuntime()

    private var loader: WhisperKitLoader { WhisperKitLoader(fetcher: fetcher, runtime: runtime) }
    private var folder: URL { root.appending(path: WhisperModel.base.id) }

    /// Every pinned file at its pinned size, as sparse files: right sizes, wrong contents.
    private func lay(_ model: WhisperModel = .base, except missing: String? = nil) throws {
        for file in model.files where file.local != missing {
            let url = folder.appending(path: file.local)
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            FileManager.default.createFile(atPath: url.path, contents: nil)
            let handle = try FileHandle(forWritingTo: url)
            try handle.truncate(atOffset: UInt64(file.bytes))
            try handle.close()
        }
    }

    @Test func loadNeverTouchesTheNetwork() async throws {
        try lay()
        _ = try await loader.load(.base, from: folder, fullCheck: false)
        #expect(fetcher.calls.isEmpty)
        // The tokenizer comes from the checked folder inside the model, never from elsewhere.
        #expect(runtime.tokenizerFolders == [folder.appending(path: WhisperModel.tokenizerFolder)])
    }

    @Test func missingTokenizerFailsWithoutFetching() async throws {
        try lay(except: "\(WhisperModel.tokenizerFolder)/tokenizer.json")
        await #expect(throws: WhisperLoadError.damaged) {
            _ = try await loader.load(.base, from: folder, fullCheck: false)
        }
        #expect(fetcher.calls.isEmpty)
        #expect(runtime.tokenizerFolders.isEmpty)

        // WhisperKit's own path, given an empty tokenizer folder, fails rather than looking
        // anywhere else.
        let empty = root.appending(path: "empty", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: empty, withIntermediateDirectories: true)
        await #expect(throws: WhisperLoadError.damaged) {
            _ = try await VerifiedTokenizer(folder: empty)
        }
        await #expect(throws: WhisperLoadError.damaged) {
            _ = try await WhisperKitRuntime().load(models: empty, tokenizer: empty)
        }
    }

    @Test func fullCheckCatchesChangedFiles() async throws {
        try lay()
        await #expect(throws: WhisperLoadError.damaged) {
            _ = try await loader.load(.base, from: folder, fullCheck: true)
        }
        #expect(fetcher.calls.isEmpty)
        #expect(runtime.tokenizerFolders.isEmpty)
    }

    @Test func rechecksAfterAppUpdate() async throws {
        let fake = FakeWhisperLoader()
        var store = WhisperModelStore(root: root)
        store.appVersion = "1.0 (1)"
        try await store.install(.base, with: fake) { _ in }
        #expect(!store.needsFullCheck(.base))
        let first = WhisperEngine(store: store, loader: fake, microphone: { FakeWhisperMicrophone() })
        _ = try await first.start(locale: Locale(identifier: "en-US"))
        first.cancel()

        store.appVersion = "1.1 (2)"
        #expect(store.needsFullCheck(.base))
        let updated = WhisperEngine(store: store, loader: fake, microphone: { FakeWhisperMicrophone() })
        _ = try await updated.start(locale: Locale(identifier: "en-US"))
        updated.cancel()
        #expect(fake.fullChecks == [false, true])
        #expect(!store.needsFullCheck(.base))
    }

    @Test func damagedModelFallsBackToApple() async throws {
        let fake = FakeWhisperLoader()
        let store = WhisperModelStore(root: root)
        let whisper = WhisperEngine(store: store, loader: fake, microphone: { FakeWhisperMicrophone() })
        try await whisper.installModel(locale: Locale(identifier: "en-US")) { _ in }
        fake.loadFailure = WhisperLoadError.damaged
        let history = DictationHistory(defaults: UserDefaults(suiteName: "whisper-loader-\(UUID().uuidString)")!)
        let dictation = DictationModel(
            engine: whisper, locale: Locale(identifier: "en-US"), history: history, insert: { _ in }
        )
        await dictation.toggle()
        #expect(dictation.phase == .failed(DictationCopy.whisperDamaged))
        #expect(!whisper.isInstalled)
        var settings = AppSettings()
        settings.dictationEngine = .whisper
        settings.whisperModel = WhisperModel.base.id
        let apple = FakeSpeechEngine()
        #expect(EngineChoice(apple: apple, whisper: whisper).engine(for: settings) === apple)
        #expect(fake.downloads == 1, "a damaged model is never fetched again on its own")
    }

    /// A decoder that reports a logits size and takes the mark.
    private struct FakeDecoder: LanguageMarked {
        var logitsSize: Int?
        var isModelMultilingual = false
    }

    @Test func multilingualModelIsToldTheLanguage() async throws {
        var base = FakeDecoder(logitsSize: 51_865)
        WhisperKitRuntime.markLanguages(&base)
        #expect(base.isModelMultilingual)
        var english = FakeDecoder(logitsSize: 51_864, isModelMultilingual: true)
        WhisperKitRuntime.markLanguages(&english)
        #expect(!english.isModelMultilingual)

        // The chosen language goes into the prefill; without one the model detects it.
        let german = WhisperKitRuntime.options(language: "de")
        #expect(german.language == "de")
        #expect(german.usePrefillPrompt)
        #expect(!german.detectLanguage)
        #expect(german.task == .transcribe)
        let automatic = WhisperKitRuntime.options(language: nil)
        #expect(automatic.language == nil)
        #expect(automatic.detectLanguage)

        // And the language reaches the model from the locale dictated in.
        let fake = FakeWhisperLoader()
        let microphone = FakeWhisperMicrophone()
        let whisper = WhisperEngine(store: WhisperModelStore(root: root), loader: fake, microphone: { microphone })
        try await whisper.installModel(locale: Locale(identifier: "de-DE")) { _ in }
        fake.transcriber.text = "Tests starten"
        _ = try await whisper.start(locale: Locale(identifier: "de-DE"))
        microphone.hear(seconds: 1)
        await whisper.stop()
        #expect(fake.transcriber.calls.last?.language == "de")
    }

    @Test func intactModelThatFailsToStartIsKept() async throws {
        try lay()
        runtime.failure = CocoaError(.featureUnsupported)
        await #expect(throws: WhisperLoadError.failed) {
            _ = try await loader.load(.base, from: folder, fullCheck: false)
        }

        let fake = FakeWhisperLoader()
        let whisper = WhisperEngine(
            store: WhisperModelStore(root: root.appending(path: "engine")), loader: fake,
            microphone: { FakeWhisperMicrophone() }
        )
        try await whisper.installModel(locale: Locale(identifier: "en-US")) { _ in }
        fake.loadFailure = WhisperLoadError.failed
        let history = DictationHistory(defaults: UserDefaults(suiteName: "whisper-loader-\(UUID().uuidString)")!)
        let dictation = DictationModel(
            engine: whisper, locale: Locale(identifier: "en-US"), history: history, insert: { _ in }
        )
        await dictation.toggle()
        #expect(dictation.phase == .failed(DictationCopy.whisperFailedToLoad))
        #expect(whisper.isInstalled)
        // The next try loads it again.
        fake.loadFailure = nil
        dictation.dismiss()
        await dictation.toggle()
        #expect(dictation.isListening)
        #expect(fake.loads == 2)
        dictation.relinquish()
    }

    @Test func failedDeleteSaysSo() async throws {
        let whisper = WhisperEngine(
            store: WhisperModelStore(root: root), loader: FakeWhisperLoader(), microphone: { FakeWhisperMicrophone() }
        )
        let section = WhisperSettingsModel(whisper: whisper)
        var settings = AppSettings()
        section.settings = settings
        await section.download { settings = $0 }
        // A folder whose entries cannot be removed.
        try FileManager.default.setAttributes([.posixPermissions: 0o555], ofItemAtPath: folder.path)
        defer { try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: folder.path) }
        section.delete { settings = $0 }
        #expect(section.failure == DictationCopy.whisperDeleteFailed)
        #expect(section.isInstalled)
        #expect(section.showsWhisper)
        #expect(settings.dictationEngine == .apple)
    }

    /// Cancelled while the microphone waits to start, as behind the permission alert: once it
    /// does start, it is stopped again rather than left listening.
    @Test func cancelWhileTheMicrophoneStartsStopsIt() async throws {
        let microphone = FakeWhisperMicrophone()
        let whisper = WhisperEngine(store: WhisperModelStore(root: root), loader: FakeWhisperLoader(), microphone: { microphone })
        try await whisper.installModel(locale: Locale(identifier: "en-US")) { _ in }
        microphone.whileStarting = { whisper.cancel() }
        await #expect(throws: CancellationError.self) { _ = try await whisper.start(locale: Locale(identifier: "en-US")) }
        #expect(!microphone.isRunning)
    }

    /// A second dictation while the model still loads waits for that load: one copy in memory.
    @Test func secondStartJoinsTheLoad() async throws {
        let fake = FakeWhisperLoader()
        let whisper = WhisperEngine(store: WhisperModelStore(root: root), loader: fake, microphone: { FakeWhisperMicrophone() })
        try await whisper.installModel(locale: Locale(identifier: "en-US")) { _ in }
        fake.holdsLoad = true
        let first = Task { _ = try await whisper.start(locale: Locale(identifier: "en-US")) }
        while fake.loads < 1 { await Task.yield() }
        fake.holdsLoad = false
        let second = Task { _ = try await whisper.start(locale: Locale(identifier: "en-US")) }
        for _ in 0..<20 { await Task.yield() }
        fake.finishLoading()
        try? await first.value
        try await second.value
        whisper.cancel()
        #expect(fake.loads == 1)
    }

    /// The model let go for memory while listening ends still hears the final pass.
    @Test func finalPassSurvivesTheModelBeingReleased() async throws {
        let fake = FakeWhisperLoader()
        let microphone = FakeWhisperMicrophone()
        let whisper = WhisperEngine(store: WhisperModelStore(root: root), loader: fake, microphone: { microphone })
        try await whisper.installModel(locale: Locale(identifier: "en-US")) { _ in }
        _ = try await whisper.start(locale: Locale(identifier: "en-US"))
        microphone.hear(seconds: 1)
        let stopping = Task { await whisper.stop() }
        await Task.yield()
        whisper.releaseModelIfIdle()
        await stopping.value
        #expect(fake.transcriber.calls.last?.samples == WhisperModel.sampleRate)
    }

    @Test func leftoverDownloadIsCleared() throws {
        let staging = root.appending(path: "\(WhisperModel.base.id).download", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: staging, withIntermediateDirectories: true)
        try Data(count: 1_024).write(to: staging.appending(path: "weight.bin"))
        let whisper = WhisperEngine(
            store: WhisperModelStore(root: root), loader: FakeWhisperLoader(), microphone: { FakeWhisperMicrophone() }
        )
        _ = WhisperSettingsModel(whisper: whisper)
        #expect(!FileManager.default.fileExists(atPath: staging.path))
    }
}
