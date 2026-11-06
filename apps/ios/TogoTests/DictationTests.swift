import Foundation
import Testing
import TogoKit
@testable import Togo

/// Dictation into the compose field, driven by the fake engine and a clock the test moves.
@MainActor
struct DictationTests {
    private let engine = FakeSpeechEngine()
    private let clock = TestDictationClock()
    private let history = DictationHistory(defaults: UserDefaults(suiteName: "dictation-tests-\(UUID().uuidString)")!)

    private func model(insert: @escaping @MainActor (String) -> Void) -> DictationModel {
        DictationModel(
            engine: engine, locale: Locale(identifier: "en-US"), history: history, insert: insert, clock: clock.clock
        )
    }

    private func compose(log: Log) -> ComposeModel {
        ComposeModel(
            history: ComposeHistory(hostID: Samples.studio, sessionID: "s-04", defaults: UserDefaults(suiteName: "dictation-compose-\(UUID().uuidString)")!),
            images: ImagePasteFlow(support: .available, upload: { _, _ in "" }, paste: { _ in }),
            paste: { log.events.append("paste:\($0)") },
            pressEnter: { log.events.append("enter") },
            speech: DictationSpeech(engine: engine, locale: Locale(identifier: "en-US"), history: history, clock: clock.clock)
        )
    }

    private func eventually(_ condition: () -> Bool) async {
        for _ in 0..<2_000 where !condition() {
            try? await Task.sleep(for: .milliseconds(1))
        }
    }

    /// Holds past the tap threshold, so the release ends listening.
    private func hold(_ dictation: DictationModel) async {
        await dictation.press()
        clock.advance(by: .milliseconds(400))
    }

    @Test func holdStartsReleaseInserts() async {
        var inserted: [String] = []
        let dictation = model { inserted.append($0) }
        await hold(dictation)
        #expect(dictation.phase == .listening(partial: "", level: 0))
        engine.emit("run the")
        await eventually { dictation.phase == .listening(partial: "run the", level: 0) }
        #expect(dictation.phase == .listening(partial: "run the", level: 0))
        engine.finalOnStop = "run the tests"
        await dictation.release()
        #expect(inserted == ["run the tests"])
        #expect(dictation.phase == .idle)
        #expect(engine.stopCalls == 1)
    }

    @Test func shortTapLatchesUntilSecondTap() async {
        var inserted: [String] = []
        let dictation = model { inserted.append($0) }
        await dictation.press()
        clock.advance(by: .milliseconds(100))
        await dictation.release()
        #expect(dictation.phase == .listening(partial: "", level: 0))
        #expect(inserted.isEmpty)
        engine.emit("check the logs", final: true)
        await dictation.press()
        await dictation.release()
        #expect(inserted == ["check the logs"])
        #expect(dictation.phase == .idle)
        #expect(engine.stopCalls == 1)
    }

    @Test func transcriptIsSanitized() async {
        var inserted: [String] = []
        let dictation = model { inserted.append($0) }
        await hold(dictation)
        engine.finalOnStop = "ls\u{1b}[201~ x\u{9b}2J"
        await dictation.release()
        #expect(inserted == ["ls[201~ x2J"])
        #expect(history.load() == ["ls[201~ x2J"])
    }

    @Test func neverPressesEnter() async throws {
        let log = Log()
        let compose = compose(log: log)
        let dictation = try #require(compose.dictation)
        await hold(dictation)
        engine.finalOnStop = "deploy to staging\n"
        await dictation.release()
        #expect(compose.text == "deploy to staging")
        #expect(log.events.isEmpty)
    }

    @Test func longTranscriptInserted() async {
        var inserted: [String] = []
        let dictation = model { inserted.append($0) }
        let long = String((0..<10_000).map { $0 % 7 == 6 ? " " : Character(UnicodeScalar(97 + $0 % 26)!) })
            .trimmingCharacters(in: .whitespaces) + "z"
        #expect(long.count >= 10_000)
        await hold(dictation)
        engine.finalOnStop = long
        await dictation.release()
        #expect(inserted == [long])
    }

    @Test func releaseBeforeReadyCancelsCleanly() async {
        var inserted: [String] = []
        let dictation = model { inserted.append($0) }
        engine.holdsStart = true
        let pressing = Task { await dictation.press() }
        await eventually { engine.startCalls == 1 }
        clock.advance(by: .milliseconds(400))
        await dictation.release()
        engine.finishStarting()
        await pressing.value
        #expect(dictation.phase == .idle)
        #expect(inserted.isEmpty)
        #expect(engine.cancelCalls == 1)
        #expect(engine.stopCalls == 0)
        #expect(engine.streamsOpened == 0)
    }

    @Test func interruptionKeepsPartial() async {
        var inserted: [String] = []
        let dictation = model { inserted.append($0) }
        await hold(dictation)
        engine.emit("add a retry", final: true)
        engine.emit(" around the upload")
        engine.fail(.interrupted)
        await eventually { !dictation.isActive }
        #expect(inserted == ["add a retry around the upload"])
        // Listening stopped without being asked to, so the bar says so in words.
        #expect(dictation.phase == .failed(DictationCopy.stoppedKept))
    }

    @Test func deniedShowsSettingsLink() async {
        engine.availabilityResult = .microphoneDenied
        let dictation = model { _ in }
        await dictation.press()
        #expect(dictation.phase == .failed(DictationCopy.denied))
        #expect(dictation.opensSettings)
        #expect(DictationCopy.denied == "Togo can't use the microphone. Turn it on in Settings.")
        #expect(engine.startCalls == 0)
        dictation.dismiss()
        #expect(dictation.phase == .idle)
        #expect(!dictation.opensSettings)
    }

    @Test func needsModelInstallsThenListens() async {
        engine.availabilityResult = .needsModel(bytes: 64_000_000)
        var progress: [DictationModel.Phase] = []
        let dictation = model { _ in }
        engine.onInstallStep = { progress.append(dictation.phase) }
        await dictation.press()
        // Nothing downloads until the user asks, with the size in front of them.
        let offer = DictationCopy.needsModel(language: "English (US)", bytes: 64_000_000)
        #expect(dictation.phase == .failed(offer))
        #expect(dictation.offersDownload)
        #expect(engine.installCalls == 0)
        #expect(engine.startCalls == 0)
        await dictation.download()
        #expect(engine.installCalls == 1)
        #expect(progress == [.preparing(0.5), .preparing(1)])
        #expect(dictation.phase == .listening(partial: "", level: 0))
        #expect(
            DictationCopy.needsModel(language: "English (US)", bytes: 64_000_000)
                == "Download speech for English (US), 64 MB"
        )
        #expect(DictationCopy.language(for: Locale(identifier: "en-US"), in: Locale(identifier: "en-US")) == "English (US)")
        #expect(DictationCopy.unsupported == "Dictation isn't available for this language.")
    }

    @Test func unsupportedLocaleSaysSo() async {
        engine.availabilityResult = .unsupportedLocale
        let dictation = model { _ in }
        await dictation.press()
        #expect(dictation.phase == .failed(DictationCopy.unsupported))
        #expect(!dictation.opensSettings)
    }

    @Test func stopsAfterMaxListen() async {
        var inserted: [String] = []
        let dictation = model { inserted.append($0) }
        await dictation.toggle()
        engine.emit("a very long thought", final: true)
        clock.advance(by: DictationModel.maxListen + .seconds(1))
        await eventually { dictation.phase == .idle }
        #expect(inserted == ["a very long thought"])
        #expect(engine.stopCalls == 1)
    }

    @Test func spaceAddedAfterWord() async {
        let compose = compose(log: Log())
        compose.text = "git"
        compose.insertDictated("status")
        #expect(compose.text == "git status")
        compose.insertDictated("now")
        #expect(compose.text == "git status now")
        compose.text = "git "
        compose.selection = nil
        compose.insertDictated("log")
        #expect(compose.text == "git log")
        compose.text = "one three"
        compose.selection = NSRange(location: 3, length: 0)
        compose.insertDictated("two")
        #expect(compose.text == "one two three")
        #expect(compose.selection == NSRange(location: 7, length: 0))
    }

    @Test func historyKeepsTwentyNewestFirst() {
        for n in 1...25 {
            history.add("note \(n)")
        }
        history.add("note 24")
        let entries = history.load()
        #expect(entries.count == DictationHistory.limit)
        #expect(DictationHistory.limit == 20)
        #expect(entries.first == "note 24")
        #expect(entries.filter { $0 == "note 24" }.count == 1)
        #expect(entries.last == "note 6")
        history.clear()
        #expect(history.load().isEmpty)
    }

    @Test func dictateKeyOpensComposeAndListens() async {
        let store = AppStore(services: FakeAppServices())
        store.open(hostID: Samples.studio, sessionID: "s-04")
        let id = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.dictate(id)
        #expect(store.composing == id)
        #expect(store.dictationRequest == id)
        let compose = compose(log: Log())
        await compose.takeDictation(for: id, from: store, shown: true)
        #expect(store.dictationRequest == nil)
        #expect(compose.dictation?.phase == .listening(partial: "", level: 0))
        // Latched: lifting a finger ends nothing; the next tap does.
        await compose.dictation?.release()
        #expect(compose.dictation?.phase == .listening(partial: "", level: 0))
    }

    @Test func composeTargetsFocusedTab() {
        let store = AppStore(services: FakeAppServices())
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        let first = TabID(hostID: Samples.studio, sessionID: "s-01")
        let second = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.workspace.selected = first
        store.workspace.secondary = second
        store.workspace.focused = .secondary
        store.panesVisible = false
        #expect(store.composeTab?.id == first)
        store.panesVisible = true
        #expect(store.composeTab?.id == second)
    }

    @Test func historyInsertGoesToTheDraft() {
        let store = AppStore(services: FakeAppServices())
        store.open(hostID: Samples.studio, sessionID: "s-04")
        let id = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.drafts[id] = "then"
        store.insertDictation("run\u{1b} it")
        #expect(store.drafts[id] == "then run it")
        #expect(store.composing == id)
    }
}

/// Time for dictation that moves only when the test says so.
@MainActor final class TestDictationClock {
    private let start = ContinuousClock.now
    private var offset: Duration = .zero

    var now: ContinuousClock.Instant { start + offset }

    func advance(by duration: Duration) {
        offset += duration
    }

    var clock: DictationClock {
        DictationClock(
            now: { [self] in now },
            sleep: { [self] deadline in
                while await now < deadline {
                    try await Task.sleep(for: .milliseconds(1))
                }
            }
        )
    }
}
