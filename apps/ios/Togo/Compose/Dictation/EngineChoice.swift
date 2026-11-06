import Foundation
import TogoKit

/// Which engine dictation runs on. Apple's speech unless Whisper is chosen and its model is on
/// this device, so a missing or deleted model always falls back to Apple.
@MainActor struct EngineChoice {
    let apple: any SpeechEngine
    let whisper: WhisperEngine

    func engine(for settings: AppSettings) -> any SpeechEngine {
        guard settings.dictationEngine == .whisper, settings.whisperModel == whisper.model.id, whisper.isInstalled else {
            return apple
        }
        return whisper
    }

    static let shared = EngineChoice(apple: makeApple(), whisper: WhisperEngine())

    /// The engine for `choice`, with the downloaded model `settings` names.
    static func make(_ choice: DictationEngineChoice, settings: AppSettings) -> any SpeechEngine {
        var chosen = settings
        chosen.dictationEngine = choice
        return shared.engine(for: chosen)
    }

    private static func makeApple() -> any SpeechEngine {
        #if DEBUG
        if ProcessInfo.processInfo.arguments.contains("-ui-fake-speech") {
            return FakeSpeechEngine.scripted()
        }
        #endif
        return AppleSpeechEngine()
    }
}

/// The engine every compose bar dictates with: whichever this device's settings choose. One
/// object for all of them, so there is one owner of the microphone whichever engine runs.
@MainActor final class SelectedSpeechEngine: SpeechEngine {
    private let choice: EngineChoice
    private var settings = AppSettings()
    /// The engine the last start went to; its stop and cancel go there too.
    private var active: (any SpeechEngine)?

    var onLevel: (@MainActor (Float) -> Void)?

    init(choice: EngineChoice) {
        self.choice = choice
    }

    var current: any SpeechEngine {
        choice.engine(for: settings)
    }

    /// Follows the settings from the next start on; a dictation under way keeps its engine.
    func select(_ settings: AppSettings) {
        self.settings = settings
    }

    var supportedLocales: [Locale] {
        get async { await current.supportedLocales }
    }

    func availability(locale: Locale) async -> DictationAvailability {
        await current.availability(locale: locale)
    }

    func installModel(locale: Locale, progress: @escaping @MainActor (Double) -> Void) async throws {
        try await current.installModel(locale: locale, progress: progress)
    }

    func start(locale: Locale) async throws -> AsyncThrowingStream<Transcript, any Error> {
        let engine = current
        if let previous = active, previous !== engine { previous.cancel() }
        active = engine
        engine.onLevel = { [weak self] level in self?.onLevel?(level) }
        return try await engine.start(locale: locale)
    }

    func stop() async {
        await (active ?? current).stop()
    }

    func cancel() {
        (active ?? current).cancel()
    }
}
