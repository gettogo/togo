import Foundation
import Testing
import TogoKit
import UIKit
@testable import Togo

/// The live working row's rules, and pictures attached in the composer.
@MainActor struct WorkingAndAttachmentTests {
    private let t0 = Date(timeIntervalSince1970: 1_793_000_000)

    private func item(_ id: String, _ body: HarnessItem.Body, at: Date? = nil, seq: Int = 1) -> HarnessItem {
        HarnessItem(id: id, seq: seq, turnId: "turn", at: WireDate(at ?? t0), body: body)
    }

    private func tool(_ id: String, _ category: HarnessItem.ToolCategory, title: String, subject: Subject?, state: HarnessState) -> HarnessItem {
        item(id, .tool(.init(callId: id, name: "T", title: title, category: category, subject: subject,
                             state: state, startedAt: WireDate(t0))))
    }

    private func prompt(_ text: String = "go") -> HarnessItem {
        item("u", .user(.init(text: text, queued: false, source: "phone")))
    }

    // MARK: Working row

    @Test func idleWithNothingSentShowsNothing() {
        #expect(WorkingLogic.state(items: [prompt()], working: false, awaitingSince: nil, localSince: nil, now: t0) == nil)
    }

    @Test func aSentPromptShowsTheRowBeforeTheHelperReportsWork() throws {
        let state = try #require(WorkingLogic.state(items: [], working: false, awaitingSince: t0, localSince: nil, now: t0 + 1))
        #expect(state.clock.start == t0)
        #expect(state.step == nil)
        #expect(WorkingCopy.line(state, now: t0 + 12) == "Working · 12s")
    }

    @Test func theRunningTurnSetsTheClock() throws {
        let turn = item("turn", .turn(.init(state: .running, startedAt: WireDate(t0 - 30), endedAt: nil, steps: 2, filesChanged: 0)))
        let state = try #require(WorkingLogic.state(items: [turn], working: true, awaitingSince: t0, localSince: t0, now: t0 + 5))
        #expect(state.clock.start == t0 - 30)
    }

    @Test func theNewestLiveStepNamesWhatHappens() {
        let read = tool("r", .read, title: "Read src/auth/sign-in.ts", subject: Subject(kind: .file, text: "src/auth/sign-in.ts"), state: .ok)
        let test = tool("s", .shell, title: "npm test", subject: Subject(kind: .command, text: "npm test\necho done"), state: .running)
        #expect(WorkingLogic.step([prompt(), read, test]) == "Running npm test")
        let reading = tool("r2", .read, title: "Read a", subject: Subject(kind: .file, text: "src/auth/sign-in.ts"), state: .running)
        #expect(WorkingLogic.step([prompt(), reading]) == "Reading src/auth/sign-in.ts")
        let mcp = tool("m", .mcp, title: "github: list issues", subject: nil, state: .running)
        #expect(WorkingLogic.step([prompt(), mcp]) == "github: list issues")
    }

    @Test func stepsBeforeThePromptAndStreamedRepliesSayNothing() {
        let old = tool("old", .shell, title: "make", subject: Subject(kind: .command, text: "make"), state: .running)
        #expect(WorkingLogic.step([old, prompt()]) == nil)
        let streaming = item("a", .assistant(.init(text: "Looking", partial: true)))
        let live = tool("r", .read, title: "Read a", subject: Subject(kind: .file, text: "a"), state: .running)
        #expect(WorkingLogic.step([prompt(), live, streaming]) == nil)
    }

    @Test func theSentHoldEndsWhenWorkStopsAfterItBegan() {
        #expect(!WorkingLogic.awaitingEnded(awaitingSince: t0, wasWorking: false, working: false, items: [], now: t0 + 1))
        #expect(!WorkingLogic.awaitingEnded(awaitingSince: t0, wasWorking: true, working: true, items: [], now: t0 + 60))
        #expect(WorkingLogic.awaitingEnded(awaitingSince: t0, wasWorking: true, working: false, items: [], now: t0 + 3))
        // The helper never reported work: the hold lapses.
        #expect(WorkingLogic.awaitingEnded(awaitingSince: t0, wasWorking: false, working: false, items: [],
                                           now: t0 + WorkingLogic.sendGrace + 1))
        // A quick turn began and ended between two notifications.
        let turn = item("turn", .turn(.init(state: .ok, startedAt: WireDate(t0 + 0.5), endedAt: WireDate(t0 + 2), steps: 0, filesChanged: 0)))
        #expect(WorkingLogic.awaitingEnded(awaitingSince: t0, wasWorking: false, working: false, items: [turn], now: t0 + 2))
    }

    @Test func storeShowsTheRowFromSendUntilTheTurnEnds() async throws {
        let services = FakeHarnessServices(descriptor: HarnessDescriptor())
        let store = HarnessSessionStore(subject: HarnessSubject(hostID: UUID(), sessionID: "s_x", capabilities: [Capability.harness]),
                                        services: services)
        #expect(store.liveWork == nil)
        store.markSent(at: .now)
        #expect(store.liveWork != nil)
        store.refreshWorking(now: .now + WorkingLogic.sendGrace + 1)
        #expect(store.liveWork == nil)
    }

    // MARK: Attachments

    private func prepared(_ data: Data) async -> ComposerAttachments.Prepared? {
        guard data != Data("bad".utf8) else { return nil }
        return .init(preview: UIImage(), encoded: .init(data: data, fileExtension: "jpg"))
    }

    @Test func preparationBlocksTheActualComposerSend() async throws {
        var waiting: CheckedContinuation<Void, Never>?
        let attachments = ComposerAttachments(upload: { name, _ in "/u/\(name)" }, prepare: { data in
            await withCheckedContinuation { waiting = $0 }
            return await prepared(data)
        })
        let model = CommandLineModel(store: .preview(), attachments: attachments)
        model.draft = "include this picture"
        let add = Task { await attachments.add([Data("a".utf8)]) }
        for _ in 0..<1000 where waiting == nil { try? await Task.sleep(for: .milliseconds(1)) }
        let release = try #require(waiting)
        #expect(!model.canSend, "Preparing a selected image must hold Send even before upload starts")
        #expect(attachments.isUploading)
        release.resume()
        await add.value
        #expect(model.canSend)
        #expect(attachments.paths.count == 1)
    }

    @Test func anUnreadableSelectionRemainsVisibleForRetryOrRemoval() async {
        let attachments = ComposerAttachments(upload: { name, _ in "/u/\(name)" }, prepare: prepared)
        await attachments.add([Data("bad".utf8)])
        #expect(attachments.items.count == 1)
        #expect(!attachments.isReady)
        if case .failed? = attachments.items.first?.state {} else { Issue.record("The selected picture vanished") }
    }

    @Test func librarySelectionReservesItsChipBeforeReturningToTheUI() async throws {
        var waiting: CheckedContinuation<Data?, Never>?
        let attachments = ComposerAttachments(upload: { name, _ in "/u/\(name)" }, prepare: prepared)
        let model = CommandLineModel(store: .preview(), attachments: attachments)
        model.draft = "include this photo"
        let work = attachments.enqueue([{ await withCheckedContinuation { waiting = $0 } }])
        #expect(attachments.items.count == 1 && attachments.isUploading)
        #expect(!model.canSend)
        for _ in 0..<1000 where waiting == nil { try? await Task.sleep(for: .milliseconds(1)) }
        let release = try #require(waiting)
        release.resume(returning: Data("a".utf8))
        await work.value
        #expect(attachments.isReady && model.canSend)
    }

    @Test func failedLibraryLoadCanBeRetriedOnTheSameChip() async throws {
        var calls = 0
        let attachments = ComposerAttachments(upload: { name, _ in "/u/\(name)" }, prepare: prepared)
        await attachments.enqueue([{
            calls += 1
            if calls == 1 { throw CocoaError(.fileReadUnknown) }
            return Data("a".utf8)
        }]).value
        let id = try #require(attachments.items.first?.id)
        #expect(attachments.items.count == 1 && !attachments.isReady)
        if case .failed(let message) = attachments.items[0].state { #expect(message == ComposeCopy.loadFailed) }
        else { Issue.record("Failed photo lost its retry state") }
        await attachments.retry(id)
        #expect(calls == 2 && attachments.isReady)
        #expect(attachments.items.first?.id == id)
    }

    @Test(arguments: [true, false])
    func removedLibraryLoadCannotRestoreAnAttachment(_ clearAll: Bool) async throws {
        var waiting: CheckedContinuation<Data?, Never>?
        var uploads = 0
        let attachments = ComposerAttachments(upload: { name, _ in uploads += 1; return "/u/\(name)" }, prepare: prepared)
        let work = attachments.enqueue([{ await withCheckedContinuation { waiting = $0 } }])
        let id = try #require(attachments.items.first?.id)
        for _ in 0..<1000 where waiting == nil { try? await Task.sleep(for: .milliseconds(1)) }
        let release = try #require(waiting)
        if clearAll { attachments.clear() } else { attachments.remove(id) }
        release.resume(returning: Data("a".utf8))
        await work.value
        #expect(attachments.isEmpty && uploads == 0)
        #expect(!attachments.isUploading)
    }

    @Test func photoWorkIsBoundedWhileSelectionOrderStaysStable() async {
        var waiting: [CheckedContinuation<Void, Never>] = []
        var opened = false
        var active = 0
        var maximum = 0
        let attachments = ComposerAttachments(upload: { name, _ in "/u/\(name)" }, prepare: prepared)
        let loads: [ComposerAttachments.Load] = (0..<10).map { n in {
            active += 1
            maximum = max(maximum, active)
            defer { active -= 1 }
            if !opened { await withCheckedContinuation { waiting.append($0) } }
            return Data("photo-\(n)".utf8)
        } }
        let work = attachments.enqueue(loads)
        for _ in 0..<1000 where waiting.count < 2 { try? await Task.sleep(for: .milliseconds(1)) }
        #expect(waiting.count == 2 && active == 2)
        opened = true
        for release in waiting { release.resume() }
        await work.value
        #expect(maximum == 2 && attachments.items.count == 10 && attachments.isReady)
        #expect(attachments.items.map { String(decoding: $0.encoded!.data, as: UTF8.self) } == (0..<10).map { "photo-\($0)" })
    }

    @Test func picturesUploadInOrderAndSendTheirPaths() async {
        @MainActor final class Log { var names: [String] = [] }
        let log = Log()
        let attachments = ComposerAttachments(upload: { name, _ in
            log.names.append(name)
            return "/u/\(name)"
        }, prepare: prepared, now: { self.t0 })
        await attachments.add([Data("a".utf8), Data("b".utf8)])
        #expect(attachments.items.count == 2)
        #expect(attachments.isReady)
        #expect(!attachments.isUploading)
        #expect(Set(attachments.paths) == Set(log.names.map { "/u/\($0)" }))
        #expect(attachments.paths.count == 2)
        #expect(attachments.paths[0].hasSuffix("-1.jpg") && attachments.paths[1].hasSuffix("-2.jpg"))
    }

    @Test func unreadablePicturesAndOverflowSaySoAndKeepTheRest() async {
        let attachments = ComposerAttachments(upload: { name, _ in "/u/\(name)" }, prepare: prepared)
        await attachments.add([Data("bad".utf8), Data("ok".utf8)])
        #expect(attachments.items.count == 2, "The unreadable selection keeps its retry/remove chip")
        #expect(attachments.notice == ComposerCopy.attachUnreadable)
        await attachments.add((0..<12).map { Data("\($0)".utf8) })
        #expect(attachments.items.count == ComposerAttachments.limit)
        #expect(attachments.notice == ComposerCopy.attachLimit(ComposerAttachments.limit))
        #expect(attachments.isFull)
    }

    @Test func aFailedUploadBlocksSendUntilRetriedOrRemoved() async throws {
        @MainActor final class Switch { var fail = true }
        let line = Switch()
        struct Down: Error {}
        let attachments = ComposerAttachments(upload: { name, _ in
            if line.fail { throw Down() }
            return "/u/\(name)"
        }, prepare: prepared)
        await attachments.add([Data("a".utf8)])
        #expect(!attachments.isReady)
        let id = try #require(attachments.items.first?.id)
        line.fail = false
        await attachments.retry(id)
        #expect(attachments.isReady)
        attachments.remove(id)
        #expect(attachments.isEmpty)
        #expect(!attachments.isReady)
    }

    @Test func oneSendCarriesEveryPictureAndTheText() async {
        @MainActor final class Sent { var all: [(String, [String])] = [] }
        let box = Sent()
        let attachments = ComposerAttachments(upload: { name, _ in "/u/\(name)" }, prepare: prepared)
        var routes = CommandLineModel.Routes(
            has: { _ in true }, working: { false }, mode: { "" }, modes: { [] }, queued: { [] }, scope: { "t" },
            complete: { _, _ in [] },
            send: { text, _ in box.all.append((text, [])); return nil },
            interrupt: { HarnessActionResult(result: .applied) }, run: { _, _, _ in HarnessActionResult(result: .applied) }
        )
        routes.sendImages = { text, _, images in box.all.append((text, images)); return nil }
        let model = CommandLineModel(
            store: .preview(), memory: CommandMemory(defaults: UserDefaults(suiteName: "attach-\(UUID().uuidString)")!),
            routes: routes, attachments: attachments
        )
        await attachments.add([Data("a".utf8), Data("b".utf8), Data("c".utf8)])
        model.draft = "what changed?"
        #expect(model.canSend)
        await model.submit()
        #expect(box.all.count == 1)
        #expect(box.all.first?.0 == "what changed?")
        #expect(box.all.first?.1.count == 3)
        #expect(attachments.isEmpty)
        // Pictures alone are a prompt too.
        await attachments.add([Data("d".utf8)])
        await model.submit()
        #expect(box.all.count == 2)
        #expect(box.all.last?.0 == "")
    }
}
