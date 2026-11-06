import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct DictationSnapshotTests {
    private func bar(text: String = "", phase: DictationModel.Phase, target: String? = nil) -> some View {
        let defaults = UserDefaults(suiteName: "dictation-snapshots-\(UUID().uuidString)")!
        let model = ComposeModel(
            text: text, history: ComposeHistory(hostID: Samples.studio, sessionID: "s-04", defaults: defaults),
            images: ImagePasteFlow(support: .available, upload: { _, _ in "" }, paste: { _ in }),
            paste: { _ in }, pressEnter: {},
            speech: DictationSpeech(
                engine: FakeSpeechEngine(), locale: Locale(identifier: "en-US"),
                history: DictationHistory(defaults: defaults)
            )
        )
        model.dictation?.show(phase)
        return VStack(spacing: 0) {
            Spacer()
            ComposeBar(model: model, focusOnAppear: false, target: target)
        }
    }

    @Test func idle() {
        assertScreen(named: "dictation-idle") { bar(text: "Fix the flaky upload test", phase: .idle, target: "04") }
    }

    @Test func listeningWithPartial() {
        assertScreen(named: "dictation-listening") {
            bar(text: "Then", phase: .listening(partial: "run the whole suite again and tell me", level: 0.6))
        }
    }

    @Test func listeningEmpty() {
        assertScreen(named: "dictation-listening-empty") { bar(phase: .listening(partial: "", level: 0.2)) }
    }

    @Test func failedDenied() {
        assertScreen(named: "dictation-denied") { bar(phase: .failed(DictationCopy.denied)) }
    }

    @Test func settingsPage() {
        let defaults = UserDefaults(suiteName: "dictation-settings-\(UUID().uuidString)")!
        let history = DictationHistory(defaults: defaults)
        history.add("Run the migration on staging first")
        history.add("Explain why the cache misses after a deploy")
        let whisper = whisperEngine()
        assertScreen(named: "dictation-settings") {
            settings(history: history, availability: .needsModel(bytes: 64_000_000), engine: WhisperSettingsModel(whisper: whisper))
        }
    }

    @Test func whisperChosen() {
        let whisper = whisperEngine()
        assertScreen(named: "dictation-whisper") {
            settings(engine: WhisperSettingsModel(whisper: whisper, showsWhisper: true))
        }
    }

    @Test func whisperDownloading() {
        let whisper = whisperEngine()
        assertScreen(named: "dictation-whisper-downloading") {
            settings(engine: WhisperSettingsModel(whisper: whisper, showsWhisper: true, fraction: 0.42))
        }
    }

    @Test func whisperDownloaded() async throws {
        let whisper = whisperEngine()
        try await whisper.installModel(locale: Locale(identifier: "en-US")) { _ in }
        var chosen = AppSettings()
        chosen.dictationEngine = .whisper
        chosen.whisperModel = WhisperModel.base.id
        assertScreen(named: "dictation-whisper-downloaded") {
            settings(chosen, engine: WhisperSettingsModel(whisper: whisper))
        }
    }

    private func whisperEngine() -> WhisperEngine {
        let root = FileManager.default.temporaryDirectory.appending(path: "whisper-snapshots-\(UUID().uuidString)")
        return WhisperEngine(
            store: WhisperModelStore(root: root), loader: FakeWhisperLoader(), microphone: { FakeWhisperMicrophone() }
        )
    }

    private func settings(
        _ settings: AppSettings = AppSettings(), history: DictationHistory? = nil,
        availability: DictationAvailability = .ready, engine: WhisperSettingsModel
    ) -> some View {
        let history = history ?? DictationHistory(defaults: UserDefaults(suiteName: "dictation-settings-\(UUID().uuidString)")!)
        let speech = FakeSpeechEngine()
        speech.availabilityResult = availability
        return NavigationStack {
            DictationSettingsView(
                settings: settings, update: { _ in },
                model: DictationSettingsModel(
                    engine: speech, history: history,
                    locales: FakeSpeechEngine.sampleLocales, availability: availability,
                    displayLocale: Locale(identifier: "en-US")
                ),
                engine: engine,
                insert: { _ in }
            )
        }
        .togoControls()
    }
}
