import Foundation
import Testing
import TogoKit
@testable import Togo

/// The microphone is on only while someone asked for it: one owner at a time, a cancelled
/// start never goes live, and a Dictate key press is used once or not at all.
@MainActor
struct DictationSafetyTests {
    private let engine = FakeSpeechEngine()
    private let clock = TestDictationClock()
    private let history = DictationHistory(defaults: UserDefaults(suiteName: "dictation-safety-\(UUID().uuidString)")!)

    private func model(insert: @escaping @MainActor (String) -> Void = { _ in }) -> DictationModel {
        DictationModel(
            engine: engine, locale: Locale(identifier: "en-US"), history: history, insert: insert, clock: clock.clock
        )
    }

    private func eventually(_ condition: () -> Bool) async {
        for _ in 0..<2_000 where !condition() {
            try? await Task.sleep(for: .milliseconds(1))
        }
    }

    @Test func newOwnerEndsTheOldOne() async {
        var first: [String] = []
        let a = model { first.append($0) }
        let b = model()
        await a.press()
        clock.advance(by: .milliseconds(400))
        engine.emit("from the first window", final: true)
        await b.press()
        await eventually { !a.isActive }
        #expect(first == ["from the first window"])
        #expect(!a.isActive)
        #expect(b.phase == .listening(partial: "", level: 0))
        #expect(engine.liveStreams == 1)
        // The old owner's finger lifting later must not touch the new owner's microphone.
        await a.release()
        a.relinquish()
        #expect(b.phase == .listening(partial: "", level: 0))
        #expect(engine.liveStreams == 1)
        #expect(engine.cancelCalls == 0)
    }

    @Test func cancelledStartNeverGoesLive() async {
        let dictation = model()
        engine.holdsStart = true
        let first = Task { await dictation.press() }
        await eventually { engine.startCalls == 1 }
        clock.advance(by: .milliseconds(400))
        await dictation.release()
        let second = Task { await dictation.press() }
        await eventually { engine.startCalls == 2 }
        engine.finishStarting()
        await first.value
        await second.value
        #expect(engine.streamsOpened == 1)
        #expect(engine.liveStreams == 1)
        #expect(engine.cancelCalls == 1)
        #expect(dictation.phase == .listening(partial: "", level: 0))
    }

    @Test func endingWithNoOwnerTurnsTheMicrophoneOff() async {
        let dictation = model()
        await dictation.toggle()
        #expect(engine.liveStreams == 1)
        dictation.relinquish()
        await eventually { engine.liveStreams == 0 }
        #expect(engine.liveStreams == 0)
        #expect(!dictation.isActive)
    }

    @Test func dictateRequestIsTakenOnce() {
        let store = AppStore(services: FakeAppServices())
        let id = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.open(hostID: id.hostID, sessionID: id.sessionID)
        store.dictate(id)
        #expect(store.takeDictationRequest(for: id, shown: true))
        #expect(!store.takeDictationRequest(for: id, shown: true))
    }

    @Test func requestForHiddenTabIsDropped() {
        let store = AppStore(services: FakeAppServices())
        let id = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.open(hostID: id.hostID, sessionID: id.sessionID)
        store.dictate(id)
        #expect(!store.takeDictationRequest(for: id, shown: false))
        #expect(store.dictationRequest == nil)
        // Showing the tab later starts nothing.
        #expect(!store.takeDictationRequest(for: id, shown: true))
    }

    @Test func staleDictateRequestExpires() {
        let store = AppStore(services: FakeAppServices())
        let id = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.open(hostID: id.hostID, sessionID: id.sessionID)
        store.dictate(id)
        let later = ContinuousClock.now + DictationRequests.lifetime + .seconds(1)
        #expect(!store.takeDictationRequest(for: id, shown: true, now: later))
        #expect(store.dictationRequest == nil)
    }

    @Test func partialIsSanitized() async {
        let dictation = model()
        await dictation.toggle()
        engine.emit("ls\u{1b}[2J \u{9b}x")
        await eventually { dictation.phase == .listening(partial: "ls[2J x", level: 0) }
        #expect(dictation.phase == .listening(partial: "ls[2J x", level: 0))
    }

    @Test func levelIgnoredWithoutMeter() async {
        let dictation = model()
        await dictation.toggle()
        dictation.showsLevel = false
        engine.onLevel?(0.8)
        #expect(dictation.phase == .listening(partial: "", level: 0))
        dictation.showsLevel = true
        engine.onLevel?(0.8)
        #expect(dictation.phase == .listening(partial: "", level: 0.8))
    }
}
