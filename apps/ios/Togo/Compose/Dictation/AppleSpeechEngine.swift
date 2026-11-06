import AVFoundation
import Foundation
import Speech

/// Apple's on-device speech: a `SpeechAnalyzer` running a `SpeechTranscriber`, or Apple's
/// dictation model where the transcriber is not offered. Audio comes from the microphone
/// through `AVAudioEngine`, converted to the analyzer's format, and never leaves the device.
@MainActor final class AppleSpeechEngine: SpeechEngine {
    var onLevel: (@MainActor (Float) -> Void)?
    /// The one session allowed to use the microphone. A cancel or a newer start replaces it,
    /// and a start that was overtaken tears down what it built instead of going live.
    private var current: ListeningSession?

    var supportedLocales: [Locale] {
        get async {
            let speech = SpeechTranscriber.isAvailable ? await SpeechTranscriber.supportedLocales : []
            return speech.isEmpty ? await DictationTranscriber.supportedLocales : speech
        }
    }

    func availability(locale: Locale) async -> DictationAvailability {
        if AVAudioApplication.shared.recordPermission == .denied { return .microphoneDenied }
        guard let module = await module(for: locale) else { return .unsupportedLocale }
        switch await AssetInventory.status(forModules: [module.module]) {
        case .installed:
            return .ready
        case .supported, .downloading:
            let request = try? await AssetInventory.assetInstallationRequest(supporting: [module.module])
            let total = request?.progress.totalUnitCount ?? 0
            // The download reports its size in bytes; a bare step count is too small to be one.
            return .needsModel(bytes: total > 1_000_000 ? total : nil)
        case .unsupported:
            return .unsupportedLocale
        @unknown default:
            return .unsupportedLocale
        }
    }

    func installModel(locale: Locale, progress: @escaping @MainActor (Double) -> Void) async throws {
        guard let module = await module(for: locale) else { throw DictationError.engine(DictationCopy.unsupported) }
        guard let request = try await AssetInventory.assetInstallationRequest(supporting: [module.module]) else { return }
        let poll = Task { @MainActor in
            while !Task.isCancelled {
                progress(request.progress.fractionCompleted)
                try? await Task.sleep(for: .milliseconds(200))
            }
        }
        defer { poll.cancel() }
        try await request.downloadAndInstall()
        progress(1)
    }

    func start(locale: Locale) async throws -> AsyncThrowingStream<Transcript, any Error> {
        if let previous = current {
            current = nil
            finish(previous, error: nil)
        }
        let session = ListeningSession()
        current = session
        do {
            guard await AVAudioApplication.requestRecordPermission() else { throw DictationError.denied }
            try check(session)
            guard let module = await module(for: locale) else { throw DictationError.engine(DictationCopy.unsupported) }
            guard let format = await SpeechAnalyzer.bestAvailableAudioFormat(compatibleWith: [module.module]) else {
                throw DictationError.engine(DictationCopy.unsupported)
            }
            try check(session)
            let analyzer = SpeechAnalyzer(modules: [module.module])
            session.analyzer = analyzer
            try await analyzer.prepareToAnalyze(in: format)
            try check(session)

            let audioSession = AVAudioSession.sharedInstance()
            try audioSession.setCategory(.record, mode: .measurement)
            try audioSession.setActive(true)
            let audio = AVAudioEngine()
            let node = audio.inputNode
            let natural = node.outputFormat(forBus: 0)
            // No input route, as just after AirPods drop: a tap in this format would raise.
            guard natural.sampleRate > 0, natural.channelCount > 0 else { throw DictationError.routeLost }
            let (inputs, input) = AsyncStream.makeStream(of: AnalyzerInput.self)
            let level: @Sendable (Float) -> Void = { [weak self, weak session] level in
                Task { @MainActor in
                    guard let self, let session, self.current === session else { return }
                    self.onLevel?(level)
                }
            }
            guard let tap = MicrophoneTap(from: natural, to: format, input: input, level: level) else {
                input.finish()
                throw DictationError.engine(DictationCopy.unsupported)
            }
            node.installTap(onBus: 0, bufferSize: 4_096, format: natural, block: tap.block)
            session.audio = audio
            session.input = input
            let (stream, output) = AsyncThrowingStream.makeStream(of: Transcript.self)
            session.output = output
            session.reader = Task { await module.read(into: output) }
            observe(session)
            audio.prepare()
            try audio.start()
            try await analyzer.start(inputSequence: inputs)
            try check(session)
            return stream
        } catch {
            if current === session { current = nil }
            finish(session, error: nil)
            throw error
        }
    }

    func stop() async {
        guard let session = current else { return }
        // No longer current: a new start from here on leaves this one to finish on its own.
        current = nil
        session.stopAudio()
        if let analyzer = session.analyzer {
            session.analyzer = nil
            try? await analyzer.finalizeAndFinishThroughEndOfInput()
        }
        await session.reader?.value
        finish(session, error: nil)
    }

    func cancel() {
        guard let session = current else { return }
        current = nil
        finish(session, error: nil)
    }

    // MARK: Parts

    private func check(_ session: ListeningSession) throws {
        if current !== session { throw CancellationError() }
    }

    /// Releases everything `session` holds, and the audio session once nothing is listening.
    private func finish(_ session: ListeningSession, error: DictationError?) {
        session.end(error)
        if current == nil {
            onLevel = nil
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        }
    }

    /// The transcriber for the closest supported locale. `SpeechTranscriber` where the device
    /// offers it, Apple's dictation model where it does not.
    private func module(for locale: Locale) async -> SpeechModuleChoice? {
        if SpeechTranscriber.isAvailable,
           let match = SpeechEngines.resolve(locale, in: await SpeechTranscriber.supportedLocales) {
            return .transcriber(SpeechTranscriber(locale: match, preset: .progressiveTranscription))
        }
        guard let match = SpeechEngines.resolve(locale, in: await DictationTranscriber.supportedLocales) else { return nil }
        return .dictation(DictationTranscriber(locale: match, preset: .progressiveShortDictation))
    }

    /// A call or Siri taking the audio, or the microphone in use going away, ends listening
    /// with what was heard so far.
    private func observe(_ session: ListeningSession) {
        let center = NotificationCenter.default
        let audioSession = AVAudioSession.sharedInstance()
        session.observers = [
            center.addObserver(forName: AVAudioSession.interruptionNotification, object: audioSession, queue: .main) {
                [weak self, weak session] note in
                let raw = note.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt
                guard raw.flatMap(AVAudioSession.InterruptionType.init(rawValue:)) == .began else { return }
                Task { @MainActor in
                    guard let self, let session else { return }
                    self.end(session, .interrupted)
                }
            },
            center.addObserver(forName: AVAudioSession.routeChangeNotification, object: audioSession, queue: .main) {
                [weak self, weak session] note in
                let raw = note.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt
                guard raw.flatMap(AVAudioSession.RouteChangeReason.init(rawValue:)) == .oldDeviceUnavailable else { return }
                Task { @MainActor in
                    guard let self, let session else { return }
                    self.end(session, .routeLost)
                }
            },
        ]
    }

    private func end(_ session: ListeningSession, _ error: DictationError) {
        guard current === session else { return }
        current = nil
        finish(session, error: error)
    }
}

/// What one listening session holds. Each start builds its own, so ending one never touches
/// another's microphone tap, analyzer or stream.
@MainActor private final class ListeningSession {
    var audio: AVAudioEngine?
    var analyzer: SpeechAnalyzer?
    var input: AsyncStream<AnalyzerInput>.Continuation?
    var output: AsyncThrowingStream<Transcript, any Error>.Continuation?
    var reader: Task<Void, Never>?
    var observers: [any NSObjectProtocol] = []

    func stopAudio() {
        audio?.inputNode.removeTap(onBus: 0)
        audio?.stop()
        audio = nil
        input?.finish()
        input = nil
    }

    /// Safe to call more than once.
    func end(_ error: DictationError?) {
        stopAudio()
        if let analyzer {
            self.analyzer = nil
            Task { await analyzer.cancelAndFinishNow() }
        }
        reader?.cancel()
        reader = nil
        if let error { output?.finish(throwing: error) } else { output?.finish() }
        output = nil
        for observer in observers { NotificationCenter.default.removeObserver(observer) }
        observers = []
    }
}

/// Either transcriber, read the same way.
private enum SpeechModuleChoice: Sendable {
    case transcriber(SpeechTranscriber)
    case dictation(DictationTranscriber)

    var module: any SpeechModule {
        switch self {
        case .transcriber(let module): module
        case .dictation(let module): module
        }
    }

    /// Volatile results become the partial line; finalized ones are what gets inserted.
    func read(into output: AsyncThrowingStream<Transcript, any Error>.Continuation) async {
        do {
            switch self {
            case .transcriber(let module):
                for try await result in module.results {
                    output.yield(Transcript(text: String(result.text.characters), isFinal: result.isFinal))
                }
            case .dictation(let module):
                for try await result in module.results {
                    output.yield(Transcript(text: String(result.text.characters), isFinal: result.isFinal))
                }
            }
        } catch {
            output.finish(throwing: DictationError.engine(error.localizedDescription))
        }
    }
}

/// Runs on the audio thread: converts each microphone buffer to the analyzer's format and
/// measures how loud it is. Holds nothing the main actor touches.
private final class MicrophoneTap: @unchecked Sendable {
    private let converter: AVAudioConverter
    private let format: AVAudioFormat
    private let input: AsyncStream<AnalyzerInput>.Continuation
    private let level: @Sendable (Float) -> Void

    /// Nil when the microphone's format cannot be converted to the analyzer's.
    init?(
        from natural: AVAudioFormat, to format: AVAudioFormat, input: AsyncStream<AnalyzerInput>.Continuation,
        level: @escaping @Sendable (Float) -> Void
    ) {
        // Converted even when the formats match: the copy is ours, while the engine reuses
        // the tap's own buffer once the block returns.
        guard let converter = AVAudioConverter(from: natural, to: format) else { return nil }
        self.converter = converter
        self.format = format
        self.input = input
        self.level = level
    }

    var block: AVAudioNodeTapBlock {
        { [self] buffer, _ in handle(buffer) }
    }

    private func handle(_ buffer: AVAudioPCMBuffer) {
        level(Self.loudness(buffer))
        let ratio = format.sampleRate / buffer.format.sampleRate
        let capacity = AVAudioFrameCount((Double(buffer.frameLength) * ratio).rounded(.up)) + 1
        guard let converted = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: capacity) else { return }
        var supplied = false
        var error: NSError?
        converter.convert(to: converted, error: &error) { _, status in
            if supplied {
                status.pointee = .noDataNow
                return nil
            }
            supplied = true
            status.pointee = .haveData
            return buffer
        }
        if error == nil, converted.frameLength > 0 {
            input.yield(AnalyzerInput(buffer: converted))
        }
    }

    /// Root mean square of the first channel, mapped from -50 dB..0 dB onto 0..1.
    private static func loudness(_ buffer: AVAudioPCMBuffer) -> Float {
        guard let samples = buffer.floatChannelData?[0], buffer.frameLength > 0 else { return 0 }
        let count = Int(buffer.frameLength)
        var sum: Float = 0
        for index in 0..<count {
            sum += samples[index] * samples[index]
        }
        let rms = (sum / Float(count)).squareRoot()
        let decibels = 20 * log10(max(rms, 0.000_001))
        return min(max((decibels + 50) / 50, 0), 1)
    }
}
