import Foundation
import TogoKit

/// Whether dictation can listen in a language right now, and if not, what stands in the way.
enum DictationAvailability: Equatable, Sendable {
    case ready
    /// The speech model for the language is not on the device yet; `bytes` is its download
    /// size when the system says.
    case needsModel(bytes: Int64?)
    case unsupportedLocale
    case microphoneDenied
    case unavailable(String)
}

/// Text heard so far. A result that is not final may still change; a final one never does,
/// and the next result continues after it.
struct Transcript: Equatable, Sendable {
    var text: String
    var isFinal: Bool
}

enum DictationError: Error, Equatable {
    /// A call, Siri or another app took the audio.
    case interrupted
    /// The microphone in use went away, as when AirPods come out.
    case routeLost
    case denied
    case engine(String)
}

/// Turns speech into text on this device. Audio and text never leave it.
@MainActor protocol SpeechEngine: AnyObject {
    func availability(locale: Locale) async -> DictationAvailability
    func installModel(locale: Locale, progress: @escaping @MainActor (Double) -> Void) async throws
    func start(locale: Locale) async throws -> AsyncThrowingStream<Transcript, any Error>
    /// The stream yields its last final result, then ends.
    func stop() async
    /// Ends listening, or a start still under way, without a last result.
    func cancel()
    var supportedLocales: [Locale] { get async }
    /// How loud the microphone is while listening, from 0 to 1, a few times a second.
    var onLevel: (@MainActor (Float) -> Void)? { get set }
}

/// The engine the app dictates with. One microphone, so one engine for every compose bar: it
/// runs Apple's speech or Whisper, as this device's settings choose.
@MainActor enum SpeechEngines {
    static let selected = SelectedSpeechEngine(choice: .shared)
    static var shared: any SpeechEngine { selected }

    /// Applies the engine choice in `settings` from the next dictation on.
    static func select(_ settings: AppSettings) {
        selected.select(settings)
    }

    /// The language to listen for: the stored choice, or the system's when there is none.
    static func locale(_ identifier: String?) -> Locale {
        identifier.map(Locale.init(identifier:)) ?? .current
    }

    /// The supported locale for `locale`: the same one, or else the first with its language.
    static func resolve(_ locale: Locale, in supported: [Locale]) -> Locale? {
        let wanted = locale.identifier(.bcp47)
        if let exact = supported.first(where: { $0.identifier(.bcp47) == wanted }) { return exact }
        guard let language = locale.language.languageCode else { return nil }
        return supported.first { $0.language.languageCode == language }
    }
}

/// Times dictation by. Tests pass one they move by hand.
struct DictationClock: Sendable {
    var now: @MainActor @Sendable () -> ContinuousClock.Instant
    /// Returns at the instant given, or throws when the task is cancelled.
    var sleep: @Sendable (ContinuousClock.Instant) async throws -> Void

    static let system = DictationClock(now: { .now }, sleep: { try await Task.sleep(until: $0, clock: .continuous) })
}
