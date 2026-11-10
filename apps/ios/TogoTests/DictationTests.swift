import Foundation
import Testing
import TogoKit
@testable import Togo

/// Dictation typed into the terminal, driven by the fake engine and a clock the test moves.
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

    private func tools(log: Log) -> TerminalInputModel {
        TerminalInputModel(
            images: ImagePasteFlow(support: .available, upload: { _, _ in "" }, paste: { _ in }),
            paste: { log.events.append("paste:\($0)") },
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

    /// The words are typed at the session's prompt as a paste; the line break said with them
    /// is cleaned away and nothing presses Enter.
    @Test func neverPressesEnter() async throws {
        let log = Log()
        let tools = tools(log: log)
        let dictation = try #require(tools.dictation)
        await hold(dictation)
        engine.finalOnStop = "deploy to staging\n"
        await dictation.release()
        #expect(log.events == ["paste:deploy to staging"])
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

    @Test func spaceOnlyBetweenWords() {
        #expect(DictationModel.needsSpace(between: "git", and: "status"))
        #expect(!DictationModel.needsSpace(between: "git ", and: "log"))
        #expect(!DictationModel.needsSpace(between: "done", and: "."))
        #expect(!DictationModel.needsSpace(between: "", and: "start"))
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

    @Test func dictateKeyListensThenTypesIntoTheTerminal() async {
        let store = AppStore(services: FakeAppServices())
        store.open(hostID: Samples.studio, sessionID: "s-04")
        let id = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.dictate(id)
        #expect(store.dictationRequest == id)
        let log = Log()
        let tools = tools(log: log)
        await tools.takeDictation(for: id, from: store, shown: true)
        #expect(store.dictationRequest == nil)
        #expect(tools.dictation?.phase == .listening(partial: "", level: 0))
        // Latched: lifting a finger ends nothing; the next tap does.
        await tools.dictation?.release()
        #expect(tools.dictation?.phase == .listening(partial: "", level: 0))
        engine.emit("run the tests", final: true)
        await tools.dictation?.toggle()
        #expect(log.events == ["paste:run the tests"])
    }

    @Test func zenKeepsTheDictateKeyAway() {
        let store = AppStore(services: FakeAppServices())
        store.open(hostID: Samples.studio, sessionID: "s-04")
        let id = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.zen = true
        store.dictate(id)
        #expect(store.dictationRequest == nil)
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

    /// Insert from Settings waits for the tab on screen, after anything already waiting, and
    /// brings the workspace forward; the workspace types it where that tab takes words.
    @Test func historyInsertWaitsForTheTabOnScreen() {
        let store = AppStore(services: FakeAppServices())
        store.open(hostID: Samples.studio, sessionID: "s-04")
        let id = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.pendingInput[id] = "then"
        store.workspaceOpen = false
        store.insertDictation("run\u{1b} it")
        #expect(store.pendingInput[id] == "then run it")
        #expect(store.workspaceOpen)
        #expect(store.path.isEmpty)
        store.insertDictation("\u{1b}")
        #expect(store.pendingInput[id] == "then run it", "nothing left after cleaning adds nothing")
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
