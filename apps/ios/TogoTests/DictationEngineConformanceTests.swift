import Foundation
import Testing
import TogoKit
@testable import Togo

/// The dictation behaviours every engine must keep, run on Apple's (as its fake) and on
/// Whisper (with a fake model and microphone).
@MainActor
struct DictationEngineConformanceTests {
    enum Kind: String, CaseIterable, CustomTestStringConvertible {
        case apple, whisper
        var testDescription: String { rawValue }
    }

    /// One engine and the means to make it hear things.
    @MainActor final class Driver {
        let engine: any SpeechEngine
        private let fake: FakeSpeechEngine?
        private let loader: FakeWhisperLoader?
        private let microphone: FakeWhisperMicrophone?

        init(_ kind: Kind) async throws {
            switch kind {
            case .apple:
                let fake = FakeSpeechEngine()
                engine = fake
                self.fake = fake
                loader = nil
                microphone = nil
            case .whisper:
                let loader = FakeWhisperLoader()
                let microphone = FakeWhisperMicrophone()
                let root = FileManager.default.temporaryDirectory.appending(path: "whisper-conformance-\(UUID().uuidString)")
                let whisper = WhisperEngine(store: WhisperModelStore(root: root), loader: loader, microphone: { microphone })
                try await whisper.installModel(locale: Locale(identifier: "en-US")) { _ in }
                engine = whisper
                fake = nil
                self.loader = loader
                self.microphone = microphone
            }
        }

        /// What the engine hears as the speaker talks, and returns when listening ends.
        func say(_ text: String) {
            if let fake {
                fake.finalOnStop = text
            } else {
                loader?.transcriber.text = text
                microphone?.hear(seconds: 1)
            }
        }

        /// A call takes the audio after `text` was heard.
        func interrupt(after text: String) {
            if let fake {
                fake.emit(text, final: true)
                fake.fail(.interrupted)
            } else {
                say(text)
                microphone?.interrupt(.interrupted)
            }
        }

        func deny() {
            fake?.availabilityResult = .microphoneDenied
            microphone?.isDenied = true
        }

        /// Makes the next start wait, as a slow engine does, until `finishStarting`.
        func holdStart() {
            fake?.holdsStart = true
            loader?.holdsLoad = true
        }

        func finishStarting() {
            fake?.finishStarting()
            loader?.finishLoading()
        }

        var starts: Int { fake?.startCalls ?? loader?.loads ?? 0 }
        var microphoneOn: Bool { fake.map { $0.liveStreams > 0 } ?? microphone?.isRunning ?? false }
    }

    private let clock = TestDictationClock()
    private let history = DictationHistory(defaults: UserDefaults(suiteName: "conformance-\(UUID().uuidString)")!)

    private func model(_ driver: Driver, insert: @escaping @MainActor (String) -> Void) -> DictationModel {
        DictationModel(engine: driver.engine, locale: Locale(identifier: "en-US"), history: history, insert: insert, clock: clock.clock)
    }

    private func hold(_ dictation: DictationModel) async {
        await dictation.press()
        clock.advance(by: .milliseconds(400))
    }

    private func eventually(_ condition: () -> Bool) async {
        for _ in 0..<2_000 where !condition() {
            try? await Task.sleep(for: .milliseconds(1))
        }
    }

    @Test(arguments: Kind.allCases) func holdStartsReleaseInserts(_ kind: Kind) async throws {
        let driver = try await Driver(kind)
        var inserted: [String] = []
        let dictation = model(driver) { inserted.append($0) }
        await hold(dictation)
        #expect(dictation.isListening)
        driver.say("run the tests")
        await dictation.release()
        #expect(inserted == ["run the tests"])
        #expect(dictation.phase == .idle)
        #expect(!driver.microphoneOn)
    }

    @Test(arguments: Kind.allCases) func shortTapLatchesUntilSecondTap(_ kind: Kind) async throws {
        let driver = try await Driver(kind)
        var inserted: [String] = []
        let dictation = model(driver) { inserted.append($0) }
        await dictation.press()
        clock.advance(by: .milliseconds(100))
        await dictation.release()
        #expect(dictation.isListening)
        #expect(inserted.isEmpty)
        driver.say("check the logs")
        await dictation.press()
        await dictation.release()
        #expect(inserted == ["check the logs"])
        #expect(dictation.phase == .idle)
    }

    @Test(arguments: Kind.allCases) func transcriptIsSanitized(_ kind: Kind) async throws {
        let driver = try await Driver(kind)
        var inserted: [String] = []
        let dictation = model(driver) { inserted.append($0) }
        await hold(dictation)
        driver.say("ls\u{1b}[201~ x\u{9b}2J")
        await dictation.release()
        #expect(inserted == ["ls[201~ x2J"])
        #expect(history.load() == ["ls[201~ x2J"])
    }

    @Test(arguments: Kind.allCases) func neverPressesEnter(_ kind: Kind) async throws {
        let driver = try await Driver(kind)
        var events: [String] = []
        let tools = TerminalInputModel(
            images: ImagePasteFlow(support: .available, upload: { _, _ in "" }, paste: { _ in }),
            paste: { events.append("paste:\($0)") },
            speech: DictationSpeech(engine: driver.engine, locale: Locale(identifier: "en-US"), history: history, clock: clock.clock)
        )
        let dictation = try #require(tools.dictation)
        await hold(dictation)
        driver.say("deploy to staging\n")
        await dictation.release()
        // Typed at the prompt as one paste, the line break cleaned away: Enter is never pressed.
        #expect(events == ["paste:deploy to staging"])
    }

    @Test(arguments: Kind.allCases) func longTranscriptInserted(_ kind: Kind) async throws {
        let driver = try await Driver(kind)
        var inserted: [String] = []
        let dictation = model(driver) { inserted.append($0) }
        let long = String((0..<10_000).map { $0 % 7 == 6 ? " " : Character(UnicodeScalar(97 + $0 % 26)!) })
            .trimmingCharacters(in: .whitespaces) + "z"
        await hold(dictation)
        driver.say(long)
        await dictation.release()
        #expect(inserted == [long])
    }

    @Test(arguments: Kind.allCases) func releaseBeforeReadyCancelsCleanly(_ kind: Kind) async throws {
        let driver = try await Driver(kind)
        var inserted: [String] = []
        let dictation = model(driver) { inserted.append($0) }
        driver.holdStart()
        let pressing = Task { await dictation.press() }
        await eventually { driver.starts == 1 }
        clock.advance(by: .milliseconds(400))
        await dictation.release()
        driver.finishStarting()
        await pressing.value
        #expect(dictation.phase == .idle)
        #expect(inserted.isEmpty)
        #expect(!driver.microphoneOn)
    }

    @Test(arguments: Kind.allCases) func interruptionKeepsPartial(_ kind: Kind) async throws {
        let driver = try await Driver(kind)
        var inserted: [String] = []
        let dictation = model(driver) { inserted.append($0) }
        await hold(dictation)
        driver.interrupt(after: "add a retry around the upload")
        await eventually { !dictation.isActive }
        #expect(inserted == ["add a retry around the upload"])
        #expect(dictation.phase == .failed(DictationCopy.stoppedKept))
        #expect(!driver.microphoneOn)
    }

    @Test(arguments: Kind.allCases) func deniedShowsSettingsLink(_ kind: Kind) async throws {
        let driver = try await Driver(kind)
        driver.deny()
        let dictation = model(driver) { _ in }
        await dictation.press()
        #expect(dictation.phase == .failed(DictationCopy.denied))
        #expect(dictation.opensSettings)
        #expect(!driver.microphoneOn)
        dictation.dismiss()
        #expect(dictation.phase == .idle)
    }
}
