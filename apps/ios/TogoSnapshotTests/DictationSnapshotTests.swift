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
        assertScreen(named: "dictation-settings") {
            let engine = FakeSpeechEngine()
            engine.availabilityResult = .needsModel(bytes: 64_000_000)
            return NavigationStack {
                DictationSettingsView(
                    settings: AppSettings(), update: { _ in },
                    model: DictationSettingsModel(
                        engine: engine, history: history,
                        locales: FakeSpeechEngine.sampleLocales, availability: .needsModel(bytes: 64_000_000),
                        displayLocale: Locale(identifier: "en-US")
                    ),
                    insert: { _ in }
                )
            }
            .togoControls()
        }
    }
}
