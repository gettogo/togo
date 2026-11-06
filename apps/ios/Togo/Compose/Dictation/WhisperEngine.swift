import Foundation

/// Whisper, on this device. While listening the microphone's audio stays in memory; it is
/// transcribed now and then for the partial line, and once more in full when listening ends.
/// Audio and text never leave the device. The only network use is the model download, and
/// only when the user asks for it.
@MainActor final class WhisperEngine: SpeechEngine {
    let model: WhisperModel
    var onLevel: (@MainActor (Float) -> Void)?
    private let store: WhisperModelStore
    private let loader: any WhisperModelLoader
    private let makeMicrophone: @MainActor () -> any WhisperMicrophone
    /// How often the partial line is brought up to date.
    private let interval: Duration
    private var transcriber: (any WhisperTranscribing)?
    /// The one session allowed to use the microphone, as in `AppleSpeechEngine`.
    private var current: WhisperSession?
    private var installing = false

    /// The partial line follows the last this-many samples, so it shows the latest words; the
    /// final pass hears everything.
    private static let partialWindow = 30 * WhisperModel.sampleRate

    init(
        model: WhisperModel = .base, store: WhisperModelStore = .standard,
        loader: any WhisperModelLoader = WhisperKitLoader(),
        microphone: @escaping @MainActor () -> any WhisperMicrophone = { WhisperAudioCapture() },
        interval: Duration = .milliseconds(1_500)
    ) {
        self.model = model
        self.store = store
        self.loader = loader
        makeMicrophone = microphone
        self.interval = interval
    }

    var isInstalled: Bool {
        store.isInstalled(model)
    }

    var supportedLocales: [Locale] {
        get async { loader.languages.sorted().map(Locale.init(identifier:)) }
    }

    func availability(locale: Locale) async -> DictationAvailability {
        if makeMicrophone().isDenied { return .microphoneDenied }
        guard language(for: locale) != nil else { return .unsupportedLocale }
        return isInstalled ? .ready : .needsModel(bytes: model.bytes)
    }

    func installModel(locale: Locale, progress: @escaping @MainActor (Double) -> Void) async throws {
        let report = ProgressRelay(progress)
        installing = true
        defer {
            report.finish()
            installing = false
        }
        try await store.install(model, with: loader) { fraction in
            Task { @MainActor in report.send(fraction) }
        }
        progress(1)
    }

    /// Frees the space the model takes. Dictation goes back to Apple's speech.
    func deleteModel() throws {
        cancel()
        transcriber = nil
        try store.delete(model)
    }

    /// Frees what a download stopped by closing the app left behind.
    func removeLeftovers() {
        guard !installing else { return }
        store.removeLeftovers(of: model)
    }

    func start(locale: Locale) async throws -> AsyncThrowingStream<Transcript, any Error> {
        if let previous = current {
            current = nil
            previous.close()
        }
        let session = WhisperSession(language: language(for: locale))
        current = session
        do {
            guard session.language != nil else { throw DictationError.engine(DictationCopy.unsupported) }
            guard isInstalled else { throw DictationError.engine(DictationCopy.whisperMissing) }
            let transcriber = try await loaded()
            try check(session)
            let (stream, output) = AsyncThrowingStream.makeStream(of: Transcript.self)
            session.output = output
            let microphone = makeMicrophone()
            session.microphone = microphone
            try await microphone.start(
                samples: { [weak session] in session?.samples += $0 },
                level: { [weak self, weak session] level in
                    guard let self, let session, self.current === session else { return }
                    self.onLevel?(level)
                },
                ended: { [weak self, weak session] error in
                    guard let self, let session else { return }
                    Task { await self.end(session, error) }
                }
            )
            try check(session)
            session.partials = Task { [weak self, weak session] in
                guard let self, let session else { return }
                await self.followAlong(session, with: transcriber)
            }
            return stream
        } catch {
            if current === session { current = nil }
            session.close()
            throw error
        }
    }

    func stop() async {
        guard let session = current else { return }
        current = nil
        await finish(session, error: nil)
    }

    func cancel() {
        guard let session = current else { return }
        current = nil
        session.close()
        onLevel = nil
    }

    // MARK: Parts

    /// Whisper's code for the locale's language, when the model understands it.
    private func language(for locale: Locale) -> String? {
        guard let code = locale.language.languageCode?.identifier, loader.languages.contains(code) else { return nil }
        return code
    }

    /// The model, loaded from its folder only. A missing or changed file removes the model,
    /// so dictation goes back to Apple's speech until it is downloaded again. A model that is
    /// intact but will not start stays, and the next dictation tries again.
    private func loaded() async throws -> any WhisperTranscribing {
        if let transcriber { return transcriber }
        let fullCheck = store.needsFullCheck(model)
        let loaded: any WhisperTranscribing
        do {
            loaded = try await loader.load(model, from: store.folder(for: model), fullCheck: fullCheck)
        } catch WhisperLoadError.damaged {
            try? store.delete(model)
            throw DictationError.engine(DictationCopy.whisperDamaged)
        } catch {
            throw DictationError.engine(DictationCopy.whisperFailedToLoad)
        }
        if fullCheck { try? store.markChecked(model) }
        transcriber = loaded
        return loaded
    }

    private func check(_ session: WhisperSession) throws {
        if current !== session { throw CancellationError() }
    }

    /// Keeps the partial line close to what is being said.
    private func followAlong(_ session: WhisperSession, with transcriber: any WhisperTranscribing) async {
        var heard = 0
        while !Task.isCancelled {
            do { try await Task.sleep(for: interval) } catch { return }
            let count = session.samples.count
            guard current === session, count > heard else { continue }
            heard = count
            let recent = Array(session.samples.suffix(Self.partialWindow))
            guard let text = try? await transcriber.transcribe(recent, language: session.language),
                  current === session, !Task.isCancelled else { continue }
            session.output?.yield(Transcript(text: Self.clean(text), isFinal: false))
        }
    }

    /// A call, Siri or a lost microphone stopped listening: what was heard still counts.
    private func end(_ session: WhisperSession, _ error: DictationError) async {
        guard current === session else { return }
        current = nil
        await finish(session, error: error)
    }

    /// Stops the microphone, transcribes everything heard and ends the stream with it.
    private func finish(_ session: WhisperSession, error: DictationError?) async {
        session.microphone?.stop()
        session.microphone = nil
        session.partials?.cancel()
        await session.partials?.value
        let samples = session.samples
        session.samples = []
        var failure = error
        if !samples.isEmpty, let transcriber {
            do {
                let text = Self.clean(try await transcriber.transcribe(samples, language: session.language))
                if !text.isEmpty { session.output?.yield(Transcript(text: text, isFinal: true)) }
            } catch {
                failure = failure ?? .engine(DictationCopy.whisperFailedToHear)
            }
        }
        session.close(failure)
        if current == nil { onLevel = nil }
    }

    /// Whisper marks silence and noises as "[BLANK_AUDIO]" or "[MUSIC]"; none of that is words.
    static func clean(_ text: String) -> String {
        text.replacing(/\[[A-Z_ ]+\]/, with: "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

/// What one listening session holds.
@MainActor private final class WhisperSession {
    let language: String?
    var microphone: (any WhisperMicrophone)?
    var output: AsyncThrowingStream<Transcript, any Error>.Continuation?
    var partials: Task<Void, Never>?
    var samples: [Float] = []

    init(language: String?) {
        self.language = language
    }

    /// Safe to call more than once.
    func close(_ error: DictationError? = nil) {
        microphone?.stop()
        microphone = nil
        partials?.cancel()
        partials = nil
        samples = []
        if let error { output?.finish(throwing: error) } else { output?.finish() }
        output = nil
    }
}

/// Download progress arrives from another thread, possibly after the download is done; this
/// keeps it in order and quiet once it is over.
@MainActor private final class ProgressRelay {
    private let progress: @MainActor (Double) -> Void
    private var done = false
    private var last = 0.0

    init(_ progress: @escaping @MainActor (Double) -> Void) {
        self.progress = progress
    }

    func send(_ fraction: Double) {
        guard !done, fraction >= last else { return }
        last = fraction
        progress(min(fraction, 0.99))
    }

    func finish() {
        done = true
    }
}
