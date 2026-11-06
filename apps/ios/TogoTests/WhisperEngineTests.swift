import Foundation
import Testing
import TogoKit
@testable import Togo

/// The Whisper engine and the choice between it and Apple's speech, on a fake model loader,
/// a fake microphone and a folder of the test's own.
@MainActor
struct WhisperEngineTests {
    private let root = FileManager.default.temporaryDirectory
        .appending(path: "whisper-tests-\(UUID().uuidString)", directoryHint: .isDirectory)
    private let loader = FakeWhisperLoader()
    private let microphone = FakeWhisperMicrophone()
    private let apple = FakeSpeechEngine()
    private let english = Locale(identifier: "en-US")

    private var store: WhisperModelStore { WhisperModelStore(root: root) }

    private func whisper() -> WhisperEngine {
        WhisperEngine(model: .base, store: store, loader: loader, microphone: { [microphone] in microphone })
    }

    private func choice(_ whisper: WhisperEngine) -> EngineChoice {
        EngineChoice(apple: apple, whisper: whisper)
    }

    private func eventually(_ condition: () -> Bool) async {
        for _ in 0..<2_000 where !condition() {
            try? await Task.sleep(for: .milliseconds(1))
        }
    }

    @Test func defaultIsApple() async throws {
        let settings = AppSettings()
        #expect(settings.dictationEngine == .apple)
        #expect(settings.whisperModel == nil)
        let whisper = whisper()
        let choice = choice(whisper)
        #expect(choice.engine(for: settings) === apple)

        // A downloaded model alone changes nothing: Apple stays until Whisper is chosen.
        try await whisper.installModel(locale: english) { _ in }
        var downloaded = settings
        downloaded.whisperModel = WhisperModel.base.id
        #expect(choice.engine(for: downloaded) === apple)
        downloaded.dictationEngine = .whisper
        #expect(choice.engine(for: downloaded) === whisper)
    }

    @Test func whisperNeedsModelUntilDownloaded() async throws {
        let whisper = whisper()
        var settings = AppSettings()
        settings.dictationEngine = .whisper
        settings.whisperModel = WhisperModel.base.id
        #expect(await whisper.availability(locale: english) == .needsModel(bytes: WhisperModel.base.bytes))
        #expect(!whisper.isInstalled)
        // Chosen but not on the device: dictation keeps using Apple's speech.
        #expect(choice(whisper).engine(for: settings) === apple)
        #expect(loader.downloads == 0)

        var fractions: [Double] = []
        try await whisper.installModel(locale: english) { fractions.append($0) }
        #expect(loader.downloads == 1)
        #expect(fractions.last == 1)
        #expect(whisper.isInstalled)
        #expect(await whisper.availability(locale: english) == .ready)
        #expect(await whisper.availability(locale: Locale(identifier: "tlh")) == .unsupportedLocale)
        #expect(choice(whisper).engine(for: settings) === whisper)
    }

    @Test func modelExcludedFromBackup() async throws {
        let whisper = whisper()
        try await whisper.installModel(locale: english) { _ in }
        let folder = store.folder(for: .base)
        #expect(folder.path.hasPrefix(root.path))
        for url in [root, folder] {
            let values = try url.resourceValues(forKeys: [.isExcludedFromBackupKey])
            #expect(values.isExcludedFromBackup == true, "\(url.lastPathComponent) would be backed up")
        }
        #expect(FileManager.default.fileExists(atPath: folder.appending(path: "config.json").path))
        // The app's own models live in Application Support, never in Documents.
        let standard = WhisperModelStore.standard.root.path
        #expect(standard.contains("/Library/Application Support/"))
    }

    @Test func deleteFallsBackToApple() async throws {
        let whisper = whisper()
        var settings = AppSettings()
        let section = WhisperSettingsModel(whisper: whisper)
        section.settings = settings
        await section.download { settings = $0 }
        #expect(settings.dictationEngine == .whisper)
        #expect(settings.whisperModel == WhisperModel.base.id)
        #expect(choice(whisper).engine(for: settings) === whisper)

        // A change made meanwhile, here or synced from another device, is kept.
        settings.themeID = "harbor"
        section.settings = settings

        section.delete { settings = $0 }
        #expect(settings.dictationEngine == .apple)
        #expect(settings.whisperModel == nil)
        #expect(settings.themeID == "harbor")
        #expect(!whisper.isInstalled)
        #expect(!FileManager.default.fileExists(atPath: store.folder(for: .base).path))
        #expect(choice(whisper).engine(for: settings) === apple)
        #expect(await whisper.availability(locale: english) == .needsModel(bytes: WhisperModel.base.bytes))
    }

    @Test func failedDownloadKeepsApple() async throws {
        loader.failure = URLError(.notConnectedToInternet)
        let whisper = whisper()
        var settings = AppSettings()
        var updates = 0
        let section = WhisperSettingsModel(whisper: whisper)
        section.settings = settings
        section.choose(.whisper) { settings = $0; updates += 1 }
        #expect(section.showsWhisper)
        #expect(updates == 0, "choosing Whisper before it is downloaded must not switch engines")

        await section.download { settings = $0; updates += 1 }
        #expect(updates == 0)
        #expect(settings.dictationEngine == .apple)
        #expect(settings.whisperModel == nil)
        #expect(section.failure == DictationCopy.whisperFailed)
        #expect(section.fraction == nil)
        #expect(!whisper.isInstalled)
        // Nothing half downloaded is left behind.
        let left = (try? FileManager.default.contentsOfDirectory(atPath: root.path)) ?? []
        #expect(left.allSatisfy { !$0.hasPrefix(WhisperModel.base.id) }, "left behind: \(left)")
        #expect(choice(whisper).engine(for: settings) === apple)
    }

    @Test func conformsToSanitisedInsert() async throws {
        let whisper = whisper()
        try await whisper.installModel(locale: english) { _ in }
        loader.transcriber.text = "ls\u{1b}[201~ x\u{9b}2J\r\n"
        var inserted: [String] = []
        let history = DictationHistory(defaults: UserDefaults(suiteName: "whisper-tests-\(UUID().uuidString)")!)
        let dictation = DictationModel(engine: whisper, locale: english, history: history, insert: { inserted.append($0) })

        await dictation.toggle()
        #expect(dictation.isListening)
        #expect(microphone.isRunning)
        microphone.hear(seconds: 1)
        await dictation.toggle()
        #expect(!microphone.isRunning)
        #expect(inserted == ["ls[201~ x2J"])
        #expect(dictation.phase == .idle)
        // The words reached the model as audio from this device's microphone only.
        #expect(loader.transcriber.calls.last?.language == "en")
        #expect(loader.transcriber.calls.last?.samples == 16_000)

        // A cancelled start leaves the microphone off and inserts nothing.
        inserted = []
        await dictation.toggle()
        dictation.relinquish()
        await eventually { !microphone.isRunning }
        #expect(!microphone.isRunning)
        #expect(inserted.isEmpty)
    }

    @Test func interruptionKeepsWhatWasHeard() async throws {
        let whisper = whisper()
        try await whisper.installModel(locale: english) { _ in }
        loader.transcriber.text = "deploy to staging"
        var inserted: [String] = []
        let history = DictationHistory(defaults: UserDefaults(suiteName: "whisper-tests-\(UUID().uuidString)")!)
        let dictation = DictationModel(engine: whisper, locale: english, history: history, insert: { inserted.append($0) })
        await dictation.toggle()
        microphone.hear(seconds: 2)
        microphone.interrupt(.interrupted)
        await eventually { !dictation.isActive }
        #expect(inserted == ["deploy to staging"])
        #expect(dictation.phase == .failed(DictationCopy.stoppedKept))
        #expect(!microphone.isRunning)
    }

    @Test func routerFollowsTheSettings() async throws {
        let whisper = whisper()
        try await whisper.installModel(locale: english) { _ in }
        let router = SelectedSpeechEngine(choice: choice(whisper))
        #expect(router.current === apple)
        var settings = AppSettings()
        settings.dictationEngine = .whisper
        settings.whisperModel = WhisperModel.base.id
        router.select(settings)
        #expect(router.current === whisper)
        // A start goes to the engine chosen then; its stop follows it even if the choice moves.
        _ = try await router.start(locale: english)
        #expect(microphone.isRunning)
        router.select(AppSettings())
        await router.stop()
        #expect(!microphone.isRunning)
        #expect(apple.stopCalls == 0)
    }
}
