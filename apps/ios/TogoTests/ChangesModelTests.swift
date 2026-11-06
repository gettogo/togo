import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct ChangesModelTests {
    private let write: Set = [Capability.git, Capability.gitWrite]

    private func subject(managed: Bool = true) -> ReviewSubject {
        ReviewSubject(
            hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
            managed: managed, capabilities: write
        )
    }

    private func loaded(
        _ status: GitStatus = ReviewFixtures.status, managed: Bool = true
    ) async -> (ChangesModel, FakeReviewServices) {
        let services = FakeReviewServices(status: status)
        let model = ChangesModel(subject: subject(managed: managed), services: services, machine: "Studio")
        await model.refresh()
        return (model, services)
    }

    private func status(_ files: [GitStatus.File], ahead: Int = 2) -> GitStatus {
        var status = ReviewFixtures.status
        status.files = files
        status.ahead = ahead
        return status
    }

    @Test func groupsStatusIntoSections() async {
        var fixture = ReviewFixtures.status
        fixture.files.append(ReviewFixtures.file("src/merge.ts", "U", "U"))
        let (model, _) = await loaded(fixture)

        #expect(model.sections.map(\.kind) == [.staged, .changed, .untracked, .conflicted])
        let paths = model.sections.map { $0.rows.map(\.path) }
        #expect(paths == [
            ["src/router.ts", "src/handlers/health.ts", "src/store.ts"],
            ["src/handlers/items.ts", "assets/logo.png", "generated/table.json"],
            ["newpkg/", "notes.txt"],
            ["src/merge.ts"],
        ])
        let words = model.sections.map { $0.rows.map(\.words) }
        #expect(words[0] == ["Modified", "Added", "Renamed from src/db.ts"])
        #expect(words[2] == ["Untracked, 2 files", "Untracked"])
        #expect(words[3] == ["Conflict"])
        #expect(model.sections[2].rows[0].paths == ["newpkg/main.go", "newpkg/util.go"])
        #expect(model.summary == "10 changed files, 3 staged")
        #expect(model.branchLine == "2 ahead of origin/main")
    }

    @Test func discardNeedsConfirmation() async {
        let (model, services) = await loaded()
        model.requestDiscard(["src/handlers/items.ts"])

        let confirmation = model.pendingConfirmation
        #expect(confirmation?.title == "Discard changes to src/handlers/items.ts?")
        #expect(confirmation?.message == "This puts it back as it was in the last commit.")
        #expect(confirmation?.files == ["src/handlers/items.ts"])
        #expect(services.writes.isEmpty)

        await model.confirm()
        #expect(services.writes == [.discard(["src/handlers/items.ts"])])
        #expect(model.pendingConfirmation == nil)
        #expect(!model.sections.flatMap(\.rows).contains { $0.path == "src/handlers/items.ts" })
    }

    /// The dialog closes, and clears the question, before its button's action runs: the answer
    /// carries the confirmation it was given.
    @Test func answerAfterTheDialogClosedStillDiscards() async throws {
        let (model, services) = await loaded()
        model.requestDiscard(["notes.txt"])
        let confirmation = try #require(model.pendingConfirmation)
        model.pendingConfirmation = nil

        await model.confirm(confirmation)
        #expect(services.writes == [.discard(["notes.txt"])])
        #expect(!model.sections.flatMap(\.rows).contains { $0.path == "notes.txt" })
    }

    @Test func untrackedFileConfirmationSaysItIsDeleted() async {
        let (model, _) = await loaded()
        model.requestDiscard(["notes.txt"])
        #expect(model.pendingConfirmation?.title == "Delete notes.txt?")
        #expect(model.pendingConfirmation?.message == "It was never committed, so it cannot be recovered.")
    }

    @Test func severalFilesConfirmationListsEveryName() async {
        let (model, services) = await loaded()
        let paths = ["src/handlers/items.ts", "assets/logo.png", "generated/table.json", "notes.txt"]
        model.requestDiscard(paths)
        let confirmation = model.pendingConfirmation
        #expect(confirmation?.title == "Discard changes to 4 files?")
        #expect(confirmation?.files == paths)
        for path in paths {
            #expect(confirmation?.message.contains(path) == true)
        }
        #expect(services.writes.isEmpty)
    }

    @Test func untrackedFolderOpensFromTheRepositoryRoot() {
        let row = ChangeRow(section: .untracked, path: "apps/tools/", paths: ["apps/tools/a.txt"], change: .untrackedFolder(1))
        #expect(row.route == .folder(path: "apps/tools", ref: nil, base: .repository))
    }

    @Test func untrackedFolderConfirmationCountsFiles() async {
        let (model, services) = await loaded()
        model.requestDiscard(["newpkg/main.go", "newpkg/util.go"])
        // The folder itself stays, so the question names only the files.
        #expect(model.pendingConfirmation?.title == "Delete the 2 files in newpkg/?")
        #expect(model.pendingConfirmation?.files == ["newpkg/main.go", "newpkg/util.go"])
        #expect(model.pendingConfirmation?.message.contains("newpkg/main.go\nnewpkg/util.go") == true)
        #expect(services.writes.isEmpty)
    }

    @Test func renamedDiscardConfirmationSaysWhatHappens() async {
        let (model, services) = await loaded()
        model.requestDiscard(["src/store.ts"])
        let confirmation = model.pendingConfirmation
        #expect(confirmation?.title == "Undo the rename?")
        #expect(confirmation?.message == "src/db.ts comes back and src/store.ts is removed.")
        #expect(confirmation?.button == "Undo rename")
        #expect(confirmation?.files == ["src/store.ts"])
        // The old path comes back, so it goes with what the user saw.
        #expect(confirmation?.selection.seen["src/db.ts"] == GitSeen(status: "R.", fingerprint: nil))
        #expect(services.writes.isEmpty)

        await model.confirm()
        #expect(services.writes == [.discard(["src/store.ts"])])
    }

    @Test func caseOnlyRenameNamesTheMachine() async {
        let (model, services) = await loaded()
        services.failures[.discard] = HelperError.conflict(GitConflict.caseRename.rawValue)
        model.requestDiscard(["src/store.ts"])
        await model.confirm()
        #expect(model.notice?.message == "Can't undo a rename that only changes letter case from this device. Use git on Studio.")
        #expect(model.notice?.canRetry == false)
    }

    @Test func unmanagedRefusalHidesActions() async {
        let (model, services) = await loaded()
        services.failures[.stage] = HelperError.unmanaged
        await model.stage(["src/handlers/items.ts"])
        #expect(!model.canWrite)
        #expect(model.readOnlyNote == "This session is not managed by Togo, so changes are read-only here.")
        #expect(model.notice == nil)
    }

    @Test func staleDiscardRefreshesList() async {
        let (model, services) = await loaded()
        model.requestDiscard(["src/handlers/items.ts"])
        services.editBehindBack("src/handlers/items.ts")
        let before = services.calls.filter { $0 == .status }.count

        await model.confirm()
        #expect(services.writes.isEmpty)
        #expect(model.notice?.message == "This file changed since you looked. Check it again.")
        #expect(services.calls.filter { $0 == .status }.count == before + 1)

        // The list now shows the edit, so a second try goes through.
        model.requestDiscard(["src/handlers/items.ts"])
        await model.confirm()
        #expect(services.writes == [.discard(["src/handlers/items.ts"])])
    }

    @Test func lockedShowsRetry() async {
        let (model, services) = await loaded()
        services.failures[.stage] = HelperError.locked
        await model.stage(["src/handlers/items.ts"])
        #expect(model.notice?.message == "Git is busy in this folder. Try again in a moment.")
        #expect(model.notice?.canRetry == true)
        #expect(services.writes.isEmpty)

        services.failures[.stage] = nil
        await model.retry()
        #expect(services.writes == [.stage(["src/handlers/items.ts"])])
        #expect(model.notice == nil)
    }

    @Test func filteredMessage() async {
        let (model, services) = await loaded()
        services.failures[.stage] = HelperError.conflict(GitConflict.filtered.rawValue)
        await model.stage(["assets/logo.png"])
        #expect(model.notice?.message == "This file uses a git filter such as LFS. Stage it at your desk.")
        #expect(model.notice?.canRetry == false)
    }

    @Test func identityNamesTheMachine() async {
        let (model, services) = await loaded()
        model.openCommitSheet()
        model.message = "Add the health route"
        services.failures[.commit] = HelperError.conflict(GitConflict.identity.rawValue)
        #expect(await model.commit(message: model.message, amend: false) == false)
        #expect(model.commitError == "Set your git name and email on Studio first.")
    }

    @Test func partialWriteSaysWhatChangedAndRefreshes() async {
        let (model, services) = await loaded()
        services.failures[.discard] = HelperError.partialWrite(changed: ["notes.txt"])
        model.requestDiscard(["notes.txt", "src/handlers/items.ts"])
        let before = services.calls.filter { $0 == .status }.count
        await model.confirm()
        #expect(model.notice?.message == "Only some changes were made: notes.txt. The list shows where things stand now.")
        #expect(services.calls.filter { $0 == .status }.count == before + 1)
    }

    @Test func racedCommitStillCloses() async {
        let (model, services) = await loaded()
        model.openCommitSheet()
        services.failures[.commit] = HelperError.racedCommit(hash: "1234567abcdef")
        #expect(await model.commit(message: "Add the health route", amend: false))
        #expect(!model.isCommitSheetOpen)
        #expect(model.notice?.message == "Someone else committed at the same time. Your commit is 1234567.")
    }

    @Test func truncatedListHasNoAllActions() async {
        let (model, services) = await loaded(ReviewFixtures.truncatedStatus)
        #expect(!model.canActOnAll)
        #expect(model.truncationNote != nil)
        await model.stageAll(.changed)
        #expect(services.writes.isEmpty)
        #expect(!services.calls.contains(.stage))
    }

    @Test func stageAllActsOnTheSectionShown() async {
        let (model, services) = await loaded()
        #expect(model.canActOnAll)
        await model.stageAll(.changed)
        #expect(services.writes == [.stage(["src/handlers/items.ts", "assets/logo.png", "generated/table.json"])])
    }

    @Test func amendOffEachTimeSheetOpens() async {
        let (model, _) = await loaded()
        model.openCommitSheet()
        #expect(!model.amend)
        model.amend = true
        model.isCommitSheetOpen = false
        model.openCommitSheet()
        #expect(!model.amend)
        #expect(model.isCommitSheetOpen)
    }

    @Test func amendPushedWarning() async {
        let (ahead, _) = await loaded()
        ahead.openCommitSheet()
        ahead.amend = true
        #expect(ahead.amendReplaces == "This replaces \(ReviewFixtures.log[0].short) Return 404 for unknown ids.")
        #expect(ahead.amendWarning == nil)

        var pushed = ReviewFixtures.status
        pushed.ahead = 0
        let (model, _) = await loaded(pushed)
        model.openCommitSheet()
        #expect(model.amendWarning == nil)
        model.amend = true
        #expect(model.amendWarning == "This commit is already pushed. Amending it rewrites shared history.")
    }

    @Test func amendSendsTheHeadSeen() async {
        let (model, services) = await loaded()
        model.openCommitSheet()
        #expect(await model.commit(message: "Return 404 for unknown ids", amend: true))
        #expect(services.writes == [.commit(message: "Return 404 for unknown ids", amend: true, head: ReviewFixtures.log[0].hash)])
    }

    @Test func commitDisabledWithNothingStaged() async {
        let (model, services) = await loaded(status([ReviewFixtures.file("notes.txt", "?", "?")]))
        model.openCommitSheet()
        model.message = "Add notes"
        #expect(!model.canCommit)
        model.amend = true
        #expect(model.canCommit)
        model.amend = false
        #expect(await model.commit(message: "Add notes", amend: false) == false)
        #expect(!services.calls.contains(.commit))
    }

    @Test func readOnlySubjectHidesActions() async {
        let (model, services) = await loaded(managed: false)
        #expect(!model.canWrite)
        #expect(model.readOnlyNote == "This session is not managed by Togo, so changes are read-only here.")
        await model.stage(["src/handlers/items.ts"])
        model.requestDiscard(["notes.txt"])
        #expect(model.pendingConfirmation == nil)
        #expect(!services.calls.contains(.stage))
        #expect(services.writes.isEmpty)
    }

    @Test func hostileNameConfirmationShowsTheWholeMarkedPath() async {
        let hostile = "src/inv\u{202E}gnp.exe\nand 3 more files"
        let (model, services) = await loaded(status([
            ReviewFixtures.file(hostile, ".", "M"), ReviewFixtures.file("README.md", ".", "M"),
        ]))
        let row = model.sections.flatMap(\.rows).first { $0.path == hostile }
        #expect(row?.displayName == "inv⟨U+202E⟩gnp.exe⟨U+000A⟩and 3 more files")
        #expect(row?.displayFolder == "src")
        #expect(row?.accessibilityLabel.contains("\u{202E}") == false)

        model.requestDiscard([hostile])
        let one = model.pendingConfirmation
        #expect(one?.title == "Discard changes to src/inv⟨U+202E⟩gnp.exe⟨U+000A⟩and 3 more files?")
        #expect(one?.files == [hostile])

        model.pendingConfirmation = nil
        model.requestDiscard([hostile, "README.md"])
        let several = model.pendingConfirmation
        // One line per file: the name cannot add a line of its own.
        let listed = several?.message.components(separatedBy: "\n\n").first?.components(separatedBy: "\n")
        #expect(listed == ["src/inv⟨U+202E⟩gnp.exe⟨U+000A⟩and 3 more files", "README.md"])
        #expect(several?.message.unicodeScalars.contains { $0.properties.isBidiControl } == false)
        #expect(services.writes.isEmpty)
    }

    @Test func voiceOverRowLabel() async {
        let (model, _) = await loaded(status([
            ReviewFixtures.file("Sources/App/App.swift", "M", "."),
            ReviewFixtures.file("README.md", ".", "M"),
        ]))
        let rows = model.sections.flatMap(\.rows)
        #expect(rows.map(\.accessibilityLabel) == [
            "App.swift, in Sources/App, Modified, staged",
            "README.md, Modified, not staged",
        ])
        #expect(model.summary == "2 changed files, 1 staged")
    }
}
