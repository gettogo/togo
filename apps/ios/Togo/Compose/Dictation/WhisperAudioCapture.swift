import AVFoundation
import Foundation

/// The microphone through `AVAudioEngine`, converted to the 16 kHz mono samples Whisper
/// listens to. The samples stay in memory and never leave the device.
@MainActor final class WhisperAudioCapture: WhisperMicrophone {
    private var audio: AVAudioEngine?
    private var observers: [any NSObjectProtocol] = []
    private var ended: (@MainActor (DictationError) -> Void)?

    var isDenied: Bool {
        AVAudioApplication.shared.recordPermission == .denied
    }

    func start(
        samples: @escaping @MainActor ([Float]) -> Void, level: @escaping @MainActor (Float) -> Void,
        ended: @escaping @MainActor (DictationError) -> Void
    ) async throws {
        guard await AVAudioApplication.requestRecordPermission() else { throw DictationError.denied }
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.record, mode: .measurement)
        try session.setActive(true)
        let audio = AVAudioEngine()
        let node = audio.inputNode
        let natural = node.outputFormat(forBus: 0)
        // No input route, as just after AirPods drop: a tap in this format would raise.
        guard natural.sampleRate > 0, natural.channelCount > 0,
              let target = AVAudioFormat(
                commonFormat: .pcmFormatFloat32, sampleRate: Double(WhisperModel.sampleRate), channels: 1, interleaved: false
              ),
              let tap = WhisperTap(from: natural, to: target, samples: samples, level: level)
        else {
            try? session.setActive(false, options: .notifyOthersOnDeactivation)
            throw DictationError.routeLost
        }
        node.installTap(onBus: 0, bufferSize: 4_096, format: natural, block: tap.block)
        self.audio = audio
        self.ended = ended
        observe(session)
        audio.prepare()
        do {
            try audio.start()
        } catch {
            stop()
            throw DictationError.engine(error.localizedDescription)
        }
    }

    func stop() {
        for observer in observers { NotificationCenter.default.removeObserver(observer) }
        observers = []
        ended = nil
        guard let audio else { return }
        self.audio = nil
        audio.inputNode.removeTap(onBus: 0)
        audio.stop()
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    private func observe(_ session: AVAudioSession) {
        let center = NotificationCenter.default
        observers = [
            center.addObserver(forName: AVAudioSession.interruptionNotification, object: session, queue: .main) {
                [weak self] note in
                let raw = note.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt
                guard raw.flatMap(AVAudioSession.InterruptionType.init(rawValue:)) == .began else { return }
                Task { @MainActor in self?.interrupt(.interrupted) }
            },
            center.addObserver(forName: AVAudioSession.routeChangeNotification, object: session, queue: .main) {
                [weak self] note in
                let raw = note.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt
                guard raw.flatMap(AVAudioSession.RouteChangeReason.init(rawValue:)) == .oldDeviceUnavailable else { return }
                Task { @MainActor in self?.interrupt(.routeLost) }
            },
        ]
    }

    private func interrupt(_ error: DictationError) {
        let ended = ended
        stop()
        ended?(error)
    }
}

/// Runs on the audio thread: converts each microphone buffer to 16 kHz mono and hands a copy
/// to the main actor with its loudness. Holds nothing the main actor touches.
private final class WhisperTap: @unchecked Sendable {
    private let converter: AVAudioConverter
    private let format: AVAudioFormat
    private let samples: @MainActor ([Float]) -> Void
    private let level: @MainActor (Float) -> Void

    init?(
        from natural: AVAudioFormat, to format: AVAudioFormat,
        samples: @escaping @MainActor ([Float]) -> Void, level: @escaping @MainActor (Float) -> Void
    ) {
        guard let converter = AVAudioConverter(from: natural, to: format) else { return nil }
        self.converter = converter
        self.format = format
        self.samples = samples
        self.level = level
    }

    var block: AVAudioNodeTapBlock {
        { [self] buffer, _ in handle(buffer) }
    }

    private func handle(_ buffer: AVAudioPCMBuffer) {
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
        guard error == nil, converted.frameLength > 0, let channel = converted.floatChannelData?[0] else { return }
        let chunk = Array(UnsafeBufferPointer(start: channel, count: Int(converted.frameLength)))
        let loudness = Self.loudness(chunk)
        let samples = samples
        let level = level
        Task { @MainActor in
            samples(chunk)
            level(loudness)
        }
    }

    /// Root mean square, mapped from -50 dB..0 dB onto 0..1.
    private static func loudness(_ chunk: [Float]) -> Float {
        guard !chunk.isEmpty else { return 0 }
        let sum = chunk.reduce(Float(0)) { $0 + $1 * $1 }
        let rms = (sum / Float(chunk.count)).squareRoot()
        let decibels = 20 * log10(max(rms, 0.000_001))
        return min(max((decibels + 50) / 50, 0), 1)
    }
}
