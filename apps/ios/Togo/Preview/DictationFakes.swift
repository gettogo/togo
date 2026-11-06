#if DEBUG
import Foundation

/// Speech for tests, previews and `-ui-fake-speech`: it hears whatever the caller emits.
@MainActor final class FakeSpeechEngine: SpeechEngine {
    static let sampleLocales = ["en-US", "en-GB", "de-DE", "fr-FR", "ja-JP"].map(Locale.init(identifier:))

    var availabilityResult: DictationAvailability = .ready
    var locales = FakeSpeechEngine.sampleLocales
    /// The fractions a model install reports, in order.
    var installSteps: [Double] = [0.5, 1]
    /// Runs after each install step is reported.
    var onInstallStep: () -> Void = {}
    /// Makes `start` wait until `finishStarting`. A start cancelled or overtaken meanwhile
    /// then throws, as the real engine's does.
    var holdsStart = false
    var startError: (any Error)?
    /// What `stop` yields as the last final result before the stream ends.
    var finalOnStop: String?
    /// Yielded, not final, as soon as listening starts.
    var heardOnStart: String?
    var onLevel: (@MainActor (Float) -> Void)?
    private(set) var startCalls = 0
    private(set) var stopCalls = 0
    private(set) var cancelCalls = 0
    private(set) var installCalls = 0
    /// Streams handed out, ever.
    private(set) var streamsOpened = 0
    /// Streams still open: the microphone is on while this is not zero.
    var liveStreams: Int { output == nil ? 0 : 1 }
    private var output: AsyncThrowingStream<Transcript, any Error>.Continuation?
    private var gates: [CheckedContinuation<Void, Never>] = []
    /// The start allowed to go live; a cancel or a newer start replaces it.
    private var currentStart: UUID?

    init() {}

    /// What the app runs with under `-ui-fake-speech`.
    static func scripted() -> FakeSpeechEngine {
        let engine = FakeSpeechEngine()
        engine.heardOnStart = "Run the tests"
        engine.finalOnStop = "Run the tests again"
        return engine
    }

    var supportedLocales: [Locale] {
        get async { locales }
    }

    func availability(locale: Locale) async -> DictationAvailability {
        availabilityResult
    }

    func installModel(locale: Locale, progress: @escaping @MainActor (Double) -> Void) async throws {
        installCalls += 1
        for step in installSteps {
            progress(step)
            onInstallStep()
        }
        availabilityResult = .ready
    }

    func start(locale: Locale) async throws -> AsyncThrowingStream<Transcript, any Error> {
        startCalls += 1
        let token = UUID()
        currentStart = token
        if holdsStart {
            await withCheckedContinuation { gates.append($0) }
        }
        guard currentStart == token else { throw CancellationError() }
        if let startError { throw startError }
        // One microphone: a new session ends any other.
        self.output?.finish()
        let (stream, output) = AsyncThrowingStream.makeStream(of: Transcript.self)
        self.output = output
        streamsOpened += 1
        if let heardOnStart { output.yield(Transcript(text: heardOnStart, isFinal: false)) }
        return stream
    }

    func stop() async {
        stopCalls += 1
        if let finalOnStop { output?.yield(Transcript(text: finalOnStop, isFinal: true)) }
        output?.finish()
        output = nil
    }

    func cancel() {
        cancelCalls += 1
        currentStart = nil
        output?.finish()
        output = nil
    }

    /// Lets a held `start` go on.
    func finishStarting() {
        let waiting = gates
        gates = []
        for gate in waiting { gate.resume() }
    }

    func emit(_ text: String, final: Bool = false) {
        output?.yield(Transcript(text: text, isFinal: final))
    }

    func fail(_ error: DictationError) {
        output?.finish(throwing: error)
        output = nil
    }
}
#endif
