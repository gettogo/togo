import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct FileCompletionFailureTests {
    private var rootsError: HelperError {
        .rpc(.init(code: RPCErrorCode.refused, message: "refused", data: .object([
            "reason": .string("fallback"), "detail": .string("roots"),
            "message": .string("Private folder text must not be displayed")
        ])))
    }

    private func make(_ session: CommandLineModelTests.Session) -> CommandLineModel {
        let defaults = UserDefaults(suiteName: "file-completion-\(UUID().uuidString)")!
        return CommandLineModel(store: .preview(), memory: CommandMemory(defaults: defaults), routes: session.routes())
    }

    @Test func incompleteRootsRemoveCachedMatchesAndOfferTerminal() async {
        let session = CommandLineModelTests.Session()
        let model = make(session)
        model.draft = "@"
        await model.draftChanged()
        #expect(!model.menu.items.isEmpty)

        session.completeError = rootsError
        model.draft = "@s"
        await model.draftChanged()
        #expect(model.menu == .none)
        #expect(model.notice == ComposerCopy.filesUnavailable)
        #expect(model.noticeAction == .openTerminal)
        #expect(model.draft == "@s")

        // A later ordinary read failure cannot revive the invalid cache.
        session.completeError = CommandLineModelTests.Offline()
        model.draft = "@sr"
        await model.draftChanged()
        #expect(model.menu == .none)
    }

    @Test func successfulFileReadClearsOnlyTheWorkspaceNotice() async {
        let session = CommandLineModelTests.Session()
        let model = make(session)
        session.completeError = rootsError
        model.draft = "@"
        await model.draftChanged()
        #expect(model.notice == ComposerCopy.filesUnavailable)

        session.completeError = nil
        await model.draftChanged()
        #expect(!model.menu.items.isEmpty)
        #expect(model.notice == nil)
        #expect(model.noticeAction == nil)

        model.notice = ComposerCopy.notSent
        model.noticeAction = .retry
        await model.draftChanged()
        #expect(model.notice == ComposerCopy.notSent)
        #expect(model.noticeAction == .retry)
    }

    @Test func commandAndUnrelatedFailuresDoNotBecomeWorkspaceFailures() async {
        let session = CommandLineModelTests.Session()
        let model = make(session)
        session.completeError = rootsError
        model.draft = "/"
        await model.draftChanged()
        #expect(model.notice == nil)

        session.completeError = CommandLineModelTests.Offline()
        model.draft = "@"
        model.notice = ComposerCopy.notSent
        model.noticeAction = .retry
        await model.draftChanged()
        #expect(model.notice == ComposerCopy.notSent)
        #expect(model.noticeAction == .retry)
    }

    @Test func anOlderFailureCannotHideANewerQuery() async {
        let session = CommandLineModelTests.Session()
        let model = make(session)
        let error = rootsError
        model.routes.complete = { [weak model] _, _ in
            guard let model else { return [] }
            // A newer keystroke finishes while the old request is outstanding.
            model.draft = "@"
            model.routes.complete = { _, _ in HarnessSamples.files }
            await model.draftChanged()
            throw error
        }
        model.draft = "@old"
        await model.draftChanged()
        #expect(model.menu.items == HarnessSamples.files)
        #expect(model.notice == nil)
        #expect(model.draft == "@")
    }

    @Test func aCancelledReadCannotShowAWorkspaceFailure() async {
        let session = CommandLineModelTests.Session()
        let model = make(session)
        let error = rootsError
        model.routes.complete = { _, _ in
            withUnsafeCurrentTask { $0?.cancel() }
            throw error
        }
        model.draft = "@"
        let task = Task { await model.draftChanged() }
        await task.value
        #expect(model.menu == .none)
        #expect(model.notice == nil)
    }

    @Test func endingOrClosingFileSearchClearsItsNoticeWithoutEditingDraft() async {
        let session = CommandLineModelTests.Session()
        let model = make(session)
        session.completeError = rootsError
        model.draft = "@"
        await model.draftChanged()
        model.draft = "ordinary prompt"
        await model.draftChanged()
        #expect(model.notice == nil)

        model.draft = "@"
        await model.draftChanged()
        model.closeMenu()
        #expect(model.notice == nil)
        #expect(model.draft == "@")

        model.notice = ComposerCopy.notSent
        model.noticeAction = .retry
        model.closeMenu()
        #expect(model.notice == ComposerCopy.notSent)
        #expect(model.noticeAction == .retry)
    }

    @Test func cancellingAnOldNoticeTimerPreservesItsReplacement() async {
        let model = make(CommandLineModelTests.Session())
        model.notice = ComposerCopy.notSent
        let task = Task { await model.expireNotice(ComposerCopy.notSent, after: .seconds(3600)) }
        model.notice = ComposerCopy.filesUnavailable
        model.noticeAction = .openTerminal
        task.cancel()
        await task.value
        #expect(model.notice == ComposerCopy.filesUnavailable)
        #expect(model.noticeAction == .openTerminal)
    }

    @Test func expiryClearsOnlyTheNoticeThatFinishedItsHold() async {
        let model = make(CommandLineModelTests.Session())
        model.notice = ComposerCopy.filesUnavailable
        model.noticeAction = .openTerminal
        await model.expireNotice(ComposerCopy.notSent, after: .zero)
        #expect(model.notice == ComposerCopy.filesUnavailable)
        await model.expireNotice(ComposerCopy.filesUnavailable, after: .zero)
        #expect(model.notice == nil)
        #expect(model.noticeAction == nil)
    }
}
