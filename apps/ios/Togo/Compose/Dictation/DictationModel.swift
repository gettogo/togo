import Foundation
import Observation
import TogoTerminal

/// Hold-to-talk dictation for one compose bar.
///
/// A press starts listening; letting go after `tapThreshold` ends it and hands the words to
/// `insert`. A shorter press latches, and the next press ends it. The words only ever reach
/// `insert`, cleaned by `PasteText`; nothing here sends a key or touches a terminal.
@MainActor @Observable final class DictationModel {
    enum Phase: Equatable {
        case idle
        /// Getting the microphone and model ready; the fraction while a model downloads.
        case preparing(Double?)
        case listening(partial: String, level: Float)
        case finishing
        case failed(String)
    }

    static let tapThreshold: Duration = .milliseconds(250)
    static let maxListen: Duration = .seconds(120)

    private(set) var phase: Phase
    /// The failure is the microphone being off, so the bar offers the Settings app.
    private(set) var opensSettings = false
    /// The language's speech model is missing; the bar shows its size and offers Download.
    private(set) var offersDownload = false
    /// Whether anything shows the microphone level. Without a meter, as under Reduce Motion,
    /// level updates would only redraw the bar for nothing.
    @ObservationIgnored var showsLevel = true
    /// The language listened for. Changing it applies from the next press.
    var locale: Locale

    @ObservationIgnored private let engine: any SpeechEngine
    @ObservationIgnored private let history: DictationHistory
    @ObservationIgnored private let insert: @MainActor (String) -> Void
    @ObservationIgnored private let clock: DictationClock
    @ObservationIgnored private var pressedAt: ContinuousClock.Instant?
    @ObservationIgnored private var latched = false
    /// Bumped whenever a dictation begins or is abandoned, so late answers from an engine call
    /// made for an earlier one are ignored.
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var heard = ""
    @ObservationIgnored private var pending = ""
    @ObservationIgnored private var session: Task<Void, Never>?
    @ObservationIgnored private var limit: Task<Void, Never>?

    init(
        engine: any SpeechEngine, locale: Locale, history: DictationHistory,
        insert: @escaping @MainActor (String) -> Void, clock: DictationClock = .system
    ) {
        self.engine = engine
        self.locale = locale
        self.history = history
        self.insert = insert
        self.clock = clock
        phase = .idle
    }

    var isActive: Bool {
        switch phase {
        case .preparing, .listening, .finishing: true
        case .idle, .failed: false
        }
    }

    var isListening: Bool {
        if case .listening = phase { true } else { false }
    }

    /// A finger down on the microphone. During a latched dictation it is the tap that ends it.
    func press() async {
        switch phase {
        case .idle, .failed:
            pressedAt = clock.now()
            await begin(latched: false, install: false)
        case .preparing, .listening:
            if latched { await end() }
        case .finishing:
            break
        }
    }

    /// The finger lifted. A short press latches; a hold ends listening and inserts the words.
    func release() async {
        guard let pressedAt else { return }
        self.pressedAt = nil
        guard !latched else { return }
        if clock.now() - pressedAt < Self.tapThreshold, isActive {
            latched = true
            return
        }
        await finish()
    }

    /// Starts a latched dictation, or ends the current one. The Dictate key and VoiceOver use it.
    func toggle() async {
        if isActive {
            await end()
        } else {
            pressedAt = nil
            await begin(latched: true, install: false)
        }
    }

    /// Downloads the missing speech model the bar offered, then listens until the next tap.
    func download() async {
        guard offersDownload else { return }
        pressedAt = nil
        await begin(latched: true, install: true)
    }

    /// Ends this dictation, keeping what was heard, whether or not it is latched.
    func end() async {
        pressedAt = nil
        await finish()
    }

    /// Lets go of the microphone at once, as when the bar leaves the screen. What was heard
    /// still lands in the draft.
    func relinquish() {
        pressedAt = nil
        switch phase {
        case .preparing: abandon()
        case .listening: Task { await finish() }
        case .idle, .failed, .finishing: break
        }
    }

    func dismiss() {
        guard case .failed = phase else { return }
        phase = .idle
        opensSettings = false
        offersDownload = false
    }

    // MARK: Listening

    private func begin(latched: Bool, install: Bool) async {
        generation += 1
        let current = generation
        self.latched = latched
        heard = ""
        pending = ""
        opensSettings = false
        offersDownload = false
        phase = .preparing(nil)
        // One microphone: whoever held it, in another window or tab, finishes first.
        if let owner = MicrophoneOwners.owner(of: engine), owner !== self {
            await owner.end()
        }
        guard current == generation else { return }
        MicrophoneOwners.set(self, for: engine)
        let locale = self.locale
        switch await engine.availability(locale: locale) {
        case .ready:
            break
        case .needsModel(let bytes) where !install:
            return fail(
                DictationCopy.needsModel(language: DictationCopy.language(for: locale), bytes: bytes),
                generation: current, download: true
            )
        case .needsModel:
            guard current == generation else { return }
            do {
                try await engine.installModel(locale: locale) { [weak self] fraction in
                    guard let self, current == self.generation else { return }
                    self.phase = .preparing(fraction)
                }
            } catch {
                return fail(Self.message(error), generation: current)
            }
        case .unsupportedLocale:
            return fail(DictationCopy.unsupported, generation: current)
        case .microphoneDenied:
            return fail(DictationCopy.denied, generation: current, settings: true)
        case .unavailable(let reason):
            return fail(reason, generation: current)
        }
        guard current == generation else { return }
        engine.onLevel = { [weak self] level in
            guard let self, self.showsLevel, current == self.generation,
                  case .listening(let partial, _) = self.phase else { return }
            self.phase = .listening(partial: partial, level: level)
        }
        let stream: AsyncThrowingStream<Transcript, any Error>
        do {
            stream = try await engine.start(locale: locale)
        } catch {
            return fail(Self.message(error), generation: current)
        }
        guard current == generation else {
            // Abandoned while the engine was starting; it must not keep listening for no one.
            if MicrophoneOwners.owner(of: engine) == nil { engine.cancel() }
            return
        }
        phase = .listening(partial: "", level: 0)
        session = Task { [weak self] in await self?.consume(stream, generation: current) }
        let deadline = clock.now() + Self.maxListen
        limit = Task { [weak self, clock] in
            do { try await clock.sleep(deadline) } catch { return }
            guard let self, current == self.generation else { return }
            await self.finish()
        }
    }

    private func consume(_ stream: AsyncThrowingStream<Transcript, any Error>, generation current: Int) async {
        var stopped = false
        do {
            for try await transcript in stream {
                guard current == generation else { return }
                if transcript.isFinal {
                    heard = Self.join(heard, transcript.text)
                    pending = ""
                } else {
                    pending = transcript.text
                }
                if case .listening(_, let level) = phase {
                    phase = .listening(partial: PasteText.sanitize(Self.join(heard, pending)), level: level)
                }
            }
        } catch {
            // Interrupted, the microphone went away, or the engine failed: keep what was heard
            // and say that listening stopped.
            stopped = true
        }
        guard current == generation else { return }
        complete(stopped: stopped)
    }

    /// Ends listening: waits for the last final result, or abandons a start still under way.
    private func finish() async {
        switch phase {
        case .preparing:
            abandon()
        case .listening:
            phase = .finishing
            limit?.cancel()
            await engine.stop()
            await session?.value
        case .idle, .failed, .finishing:
            break
        }
    }

    private func abandon() {
        generation += 1
        limit?.cancel()
        latched = false
        phase = .idle
        if MicrophoneOwners.owner(of: engine) === self {
            MicrophoneOwners.set(nil, for: engine)
            engine.onLevel = nil
        }
        engine.cancel()
    }

    private func complete(stopped: Bool = false) {
        limit?.cancel()
        limit = nil
        session = nil
        latched = false
        pressedAt = nil
        if MicrophoneOwners.owner(of: engine) === self {
            MicrophoneOwners.set(nil, for: engine)
            engine.onLevel = nil
        }
        let text = PasteText.sanitize(Self.join(heard, pending)).trimmingCharacters(in: .whitespacesAndNewlines)
        heard = ""
        pending = ""
        phase = stopped ? .failed(text.isEmpty ? DictationCopy.stopped : DictationCopy.stoppedKept) : .idle
        guard !text.isEmpty else { return }
        insert(text)
        history.add(text)
    }

    private func fail(_ message: String, generation current: Int, settings: Bool = false, download: Bool = false) {
        guard current == generation else { return }
        generation += 1
        latched = false
        pressedAt = nil
        if MicrophoneOwners.owner(of: engine) === self {
            MicrophoneOwners.set(nil, for: engine)
            engine.onLevel = nil
        }
        opensSettings = settings || message == DictationCopy.denied
        offersDownload = download
        phase = .failed(message)
    }

    private static func message(_ error: any Error) -> String {
        switch error as? DictationError {
        case .denied: DictationCopy.denied
        case .engine(let reason): reason
        case .interrupted, .routeLost: DictationCopy.stopped
        case nil: ServiceErrors.message(error)
        }
    }

    /// Finished pieces arrive one after another; a space goes between two words that would
    /// otherwise run together.
    static func join(_ head: String, _ tail: String) -> String {
        needsSpace(between: head, and: tail) ? head + " " + tail : head + tail
    }

    static func needsSpace(between head: String, and tail: String) -> Bool {
        guard let last = head.last, let first = tail.first else { return false }
        return !last.isWhitespace && !first.isWhitespace && !first.isPunctuation
    }

    #if DEBUG
    /// Puts the model in a phase for previews and snapshots.
    func show(_ phase: Phase) {
        self.phase = phase
        opensSettings = phase == .failed(DictationCopy.denied)
    }
    #endif
}

/// Which dictation holds each engine's microphone. Weak, so a bar that is gone holds nothing.
@MainActor private enum MicrophoneOwners {
    private final class Weak {
        weak var model: DictationModel?
        init(_ model: DictationModel) { self.model = model }
    }

    private static var owners: [ObjectIdentifier: Weak] = [:]

    static func owner(of engine: any SpeechEngine) -> DictationModel? {
        owners[ObjectIdentifier(engine)]?.model
    }

    static func set(_ model: DictationModel?, for engine: any SpeechEngine) {
        owners[ObjectIdentifier(engine)] = model.map(Weak.init)
    }
}
