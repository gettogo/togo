import Foundation
import Testing
import TogoKit
import UIKit
@testable import Togo

/// The fake services, with diffs the test supplies in place of the fixtures'.
@MainActor private final class ScriptedDiffServices: ReviewServices {
    let fake: FakeReviewServices
    var diffs: [String: GitDiff] = [:]

    init(_ fake: FakeReviewServices = FakeReviewServices()) {
        self.fake = fake
    }

    func status(_ s: ReviewSubject) async throws -> GitStatus { try await fake.status(s) }
    func diff(_ s: ReviewSubject, _ source: DiffSource) async throws -> GitDiff {
        let answer = try await fake.diff(s, source)
        switch source {
        case .worktree(let path), .staged(let path), .untracked(let path), .commit(_, let path?):
            return diffs[path] ?? answer
        case .commit(_, nil):
            return diffs["*"] ?? answer
        }
    }
    func read(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> FsReadResult {
        try await fake.read(s, path: path, ref: ref, base: base)
    }
    func listDir(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> ListDirResult {
        try await fake.listDir(s, path: path, ref: ref, base: base)
    }
    func log(_ s: ReviewSubject, ref: String?, limit: Int) async throws -> [GitCommit] {
        try await fake.log(s, ref: ref, limit: limit)
    }
    func branches(_ s: ReviewSubject) async throws -> GitBranches { try await fake.branches(s) }
    func stage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await fake.stage(s, paths: paths, seen: seen)
    }
    func unstage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await fake.unstage(s, paths: paths, seen: seen)
    }
    func discard(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await fake.discard(s, paths: paths, seen: seen)
    }
    func commit(_ s: ReviewSubject, message: String, amend: Bool, head: String?) async throws -> GitCommitResult {
        try await fake.commit(s, message: message, amend: amend, head: head)
    }
    func ports(_ s: ReviewSubject) async throws -> [ListeningPort] { try await fake.ports(s) }
    func forward(_ s: ReviewSubject, port: ListeningPort) async throws -> ForwardInfo {
        try await fake.forward(s, port: port)
    }
    func stopForward(localPort: Int) async { await fake.stopForward(localPort: localPort) }
    func forwardEvents() -> AsyncStream<(localPort: Int, message: String)> { fake.forwardEvents() }
}

@MainActor struct DiffModelTests {
    private func subject(managed: Bool = true) -> ReviewSubject {
        ReviewSubject(
            hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
            managed: managed, capabilities: [Capability.git, Capability.gitWrite]
        )
    }

    private func model(
        _ source: DiffSource, services: any ReviewServices = FakeReviewServices(), managed: Bool = true,
        send: ((String) -> Void)? = nil
    ) -> DiffModel {
        DiffModel(subject: subject(managed: managed), services: services, source: source, sendToSession: send)
    }

    private func lines(_ rows: [CodeRow]) -> [(text: String, kind: DiffLineKind?, tokens: [TokenSpan])] {
        rows.compactMap { row in
            if case let .line(_, _, text, kind, tokens, _, _) = row { (text, kind, tokens) } else { nil }
        }
    }

    // MARK: Rendering

    @Test func untrackedRendersAllAdded() async {
        let fake = FakeReviewServices()
        let model = model(.untracked(path: "notes.txt"), services: fake)
        await model.load()
        #expect(model.state == .ready)
        let shown = lines(model.rows(split: false))
        #expect(shown.map(\.text) == ["check the 404 path", "then the pagination test"])
        #expect(shown.allSatisfy { $0.kind == .added })
        #expect(!fake.calls.contains(.diff))
        // Status names files from the repository root, also for a session in a subfolder.
        #expect(fake.bases["notes.txt"] == .repository)
    }

    @Test func binaryShowsNotice() async {
        let model = model(.worktree(path: "assets/logo.png"))
        await model.load()
        #expect(model.state == .binary)
        #expect(model.rows(split: false) == [.notice("Binary file, not shown")])
        #expect(model.openRoute == .file(path: "assets/logo.png", ref: nil, base: .repository))
    }

    @Test func binaryOpenOnlyForImages() async {
        let services = ScriptedDiffServices()
        services.diffs["tool.bin"] = GitDiff(diff: "", truncated: false, binary: ["tool.bin"])
        let model = model(.worktree(path: "tool.bin"), services: services)
        await model.load()
        #expect(model.state == .binary)
        #expect(model.openRoute == nil)
    }

    @Test func truncatedShowsNotice() async {
        let model = model(.worktree(path: ReviewFixtures.bigDiffPath))
        await model.load()
        #expect(model.rows(split: false).first == .notice("Showing the first 512 KB of this diff."))
        #expect(model.rows(split: true).first == .notice("Showing the first 512 KB of this diff."))
    }

    @Test func modeOnlyChangeSaysSo() async {
        let services = ScriptedDiffServices()
        services.diffs["run.sh"] = GitDiff(
            diff: "diff --git a/run.sh b/run.sh\nold mode 100644\nnew mode 100755\n", truncated: false, binary: []
        )
        let model = model(.worktree(path: "run.sh"), services: services)
        await model.load()
        #expect(model.rows(split: false) == [.notice("Only the file mode changed (644 to 755).")])
    }

    @Test func emptyDiffIsEmpty() async {
        let model = model(.worktree(path: "src/unchanged.ts"))
        await model.load()
        #expect(model.state == .empty)
    }

    @Test func failedLoadSaysWhy() async {
        let fake = FakeReviewServices()
        fake.failures[.diff] = ReviewFixtureError.offline
        let model = model(.worktree(path: "src/router.ts"), services: fake)
        await model.load()
        #expect(model.state == .failed("The machine didn't answer."))
    }

    @Test func oldSideTokensForRemovedLines() {
        // The removed lines sit inside a block comment on the old side only.
        let diff = GitDiff(diff: """
        diff --git a/a.ts b/a.ts
        --- a/a.ts
        +++ b/a.ts
        @@ -1,3 +1,3 @@
        -/* start
        -let x = 1
        +// start
        +let y = 2
         end */

        """, truncated: false, binary: [])
        let prepared = DiffPreparation.prepare(diff)
        let shown = lines(prepared[0].unified)
        let removed = shown.first { $0.text == "let x = 1" }
        let added = shown.first { $0.text == "let y = 2" }
        #expect(removed?.tokens.map(\.kind) == [.comment])
        #expect(added?.tokens.contains { $0.kind == .keyword } == true)
        // Context reads from the new side in one column and from each side in two.
        #expect(shown.last?.tokens.contains { $0.kind == .comment } == false)
        let pair = prepared[0].split.compactMap { row -> (CodeRowSide?, CodeRowSide?)? in
            if case let .split(left, right) = row, left?.kind == .context { (left, right) } else { nil }
        }.first
        #expect(pair?.0?.tokens.map(\.kind) == [.comment])
        #expect(pair?.1?.tokens.contains { $0.kind == .comment } == false)
    }

    @Test func splitOnlyWhenWide() {
        #expect(DiffModel.splitAvailable(horizontalSizeClass: .regular, width: 700))
        #expect(DiffModel.splitAvailable(horizontalSizeClass: .regular, width: 1_032))
        #expect(!DiffModel.splitAvailable(horizontalSizeClass: .regular, width: 699))
        #expect(!DiffModel.splitAvailable(horizontalSizeClass: .compact, width: 900))
        #expect(!DiffModel.splitAvailable(horizontalSizeClass: nil, width: 900))
    }

    // MARK: Comments

    @Test func commentFormatsPathAndLine() async {
        var sent: [String] = []
        let model = model(.worktree(path: "src/router.ts"), send: { sent.append($0) })
        await model.load()
        let rows = model.rows(split: false)
        let added = rows.firstIndex { $0.copyText == "  app.get(\"/health\", health)" }!
        let removed = rows.firstIndex { $0.copyText == "  app.get(\"/items/:id\", find)" }!
        #expect(model.commentDraft(row: added)?.quote == "  app.get(\"/health\", health)")
        model.comment(row: added, text: "  Add a test for this  ")
        model.comment(row: removed, text: "Why drop this?")
        model.comment(row: added, text: "   ")
        #expect(sent == ["src/router.ts:12: Add a test for this", "src/router.ts:11: Why drop this?"])
        #expect(model.commentDraft(row: 0) == nil)
    }

    @Test func hostileFileNameIsCleanedBeforeTheSession() async {
        var sent: [String] = []
        let services = ScriptedDiffServices()
        // Git quotes names with control characters; this one tries to end a bracketed paste,
        // start a new line and reverse the text that follows.
        let name = #"x\033[201~\nrm -rf ~\342\200\256.ts"#
        services.diffs["evil"] = GitDiff(diff: """
        diff --git "a/\(name)" "b/\(name)"
        --- "a/\(name)"
        +++ "b/\(name)"
        @@ -1,1 +1,1 @@
        -old
        +new

        """, truncated: false, binary: [])
        let model = model(.worktree(path: "evil"), services: services, send: { sent.append($0) })
        await model.load()
        #expect(model.path.unicodeScalars.contains("\u{1B}"))
        let row = model.rows(split: false).firstIndex { $0.copyText == "new" }!
        model.comment(row: row, text: "look\u{1B}[201~\u{7F}\u{9B}31m here\r\nnow\u{200B}\u{202E}")
        #expect(sent == ["x[201~ rm -rf ~.ts:1: look[201~31m here now"])
        let scalars = sent.joined().unicodeScalars
        #expect(!scalars.contains { $0.value < 0x20 || (0x7F...0x9F).contains($0.value) })
        #expect(!scalars.contains { $0.properties.generalCategory == .format })
    }

    @Test func commentDisabledWithoutSession() async {
        let model = model(.worktree(path: "src/router.ts"), send: nil)
        await model.load()
        #expect(!model.canSendComments)
        #expect(model.commentActionTitle == "Open the session to send comments")
        let withSession = self.model(.worktree(path: "src/router.ts"), send: { _ in })
        #expect(withSession.canSendComments)
        #expect(withSession.commentActionTitle == "Send to session")
    }

    // MARK: Navigation

    @Test func nextHunkScrollsToHeader() async {
        let services = ScriptedDiffServices()
        services.diffs["src/app.ts"] = GitDiff(diff: """
        diff --git a/src/app.ts b/src/app.ts
        --- a/src/app.ts
        +++ b/src/app.ts
        @@ -1,2 +1,2 @@
        -let a = 1
        +let a = 2
         let b = 1
        @@ -20,2 +20,2 @@ function tail() {
         let c = 1
        -let d = 1
        +let d = 2
        @@ -40,1 +40,1 @@
        -let e = 1
        +let e = 2

        """, truncated: false, binary: [])
        let model = model(.worktree(path: "src/app.ts"), services: services)
        await model.load()
        let rows = model.rows(split: false)
        #expect(model.scrollTarget == nil)
        #expect(!model.canGoToPreviousHunk && model.canGoToNextHunk)
        model.nextHunk()
        #expect(model.scrollTarget.map { rows[$0] } == .hunk("@@ -20,2 +20,2 @@ function tail() {"))
        model.nextHunk()
        #expect(model.scrollTarget.map { rows[$0] } == .hunk("@@ -40,1 +40,1 @@"))
        #expect(!model.canGoToNextHunk)
        model.previousHunk()
        #expect(model.scrollTarget.map { rows[$0] } == .hunk("@@ -20,2 +20,2 @@ function tail() {"))
        // Switching to two columns keeps the same hunk in view.
        model.showsSplit = true
        let split = model.rows(split: true)
        #expect(model.scrollTarget.map { split[$0] } == .hunk("@@ -20,2 +20,2 @@ function tail() {"))
    }

    @Test func changesStepFromTheVisibleTop() async {
        let services = ScriptedDiffServices()
        services.diffs["src/app.ts"] = GitDiff(diff: """
        diff --git a/src/app.ts b/src/app.ts
        --- a/src/app.ts
        +++ b/src/app.ts
        @@ -1,2 +1,2 @@
        -let a = 1
        +let a = 2
         let b = 1
        @@ -20,2 +20,2 @@
         let c = 1
        -let d = 1
        +let d = 2
        @@ -40,1 +40,1 @@
        -let e = 1
        +let e = 2

        """, truncated: false, binary: [])
        let model = model(.worktree(path: "src/app.ts"), services: services)
        await model.load()
        let hunks = model.rows(split: false).indices.filter { model.rows(split: false)[$0].isHunk }
        #expect(hunks == [0, 4, 8])
        // The user scrolls into the second change by hand.
        model.scrolled(toTopRow: 6)
        #expect(model.canGoToPreviousHunk && model.canGoToNextHunk)
        model.nextHunk()
        #expect(model.scrollTarget == 8)
        #expect(!model.canGoToNextHunk)
        // Past the last change, previous goes up to it, not to the one before.
        model.scrolled(toTopRow: 10)
        #expect(model.scrollTarget == nil)
        model.previousHunk()
        #expect(model.scrollTarget == 8)
        // Back to the top by hand: a jump to the same change scrolls again.
        model.scrolled(toTopRow: 0)
        #expect(model.scrollTarget == nil && !model.canGoToPreviousHunk)
        model.nextHunk()
        #expect(model.scrollTarget == 4)
    }

    @Test func commentMenuOnlyOnLines() {
        let view = CodeLinesUIView(frame: CGRect(x: 0, y: 0, width: 400, height: 600))
        let rows: [CodeRow] = [
            .hunk("@@ -1,1 +1,1 @@"), .notice("Showing the first 512 KB of this diff."),
            .line(number: 1, oldNumber: 1, text: "let a = 1", kind: .context, tokens: [], changes: [], clipped: false),
        ]
        view.update(rows: rows, wrap: false, showsOldNumbers: true, scrollTo: nil, onLongPress: { _ in })
        func menu(_ item: Int) -> UIContextMenuConfiguration? {
            view.collectionView(
                view.collectionView, contextMenuConfigurationForItemsAt: [IndexPath(item: item, section: 0)], point: .zero
            )
        }
        #expect(menu(0) == nil)
        #expect(menu(1) == nil)
        #expect(menu(2) != nil)
    }

    @Test func filesInACommitAreNavigable() async {
        let model = model(.commit(hash: ReviewFixtures.log[0].hash, path: nil))
        await model.load()
        #expect(model.fileNames == ["src/router.ts", "src/handlers/health.ts", "src/store.ts"])
        #expect(model.path == "src/router.ts")
        model.nextHunk()
        model.selectFile(2)
        #expect(model.path == "src/store.ts")
        #expect(model.scrollTarget == nil)
        #expect(lines(model.rows(split: false)).contains { $0.text == "export const store = new Map<string, Item>()" })
        #expect(model.actions.isEmpty)
    }

    @Test func largeDiffParsesOffMain() async throws {
        let model = model(.worktree(path: ReviewFixtures.bigDiffPath))
        let loading = Task { await model.load() }
        // Count main-actor turns while the diff is parsed and tokenized elsewhere.
        var longest: Duration = .zero
        var sawLoading = false
        let clock = ContinuousClock()
        let start = clock.now
        var last = start
        while model.state == .loading, clock.now - start < .seconds(30) {
            sawLoading = true
            try await Task.sleep(for: .milliseconds(5))
            let now = clock.now
            longest = max(longest, now - last)
            last = now
        }
        await loading.value
        #expect(sawLoading)
        #expect(model.state == .ready)
        #expect(model.rows(split: false).count > 10_000)
        // Parsing and tokenizing this diff on the main actor takes well over a second in a Debug
        // build, so 250 ms still catches that while tolerating scheduler stalls on a busy machine.
        #expect(longest < .milliseconds(250), "main actor blocked for \(longest)")
    }

    // MARK: Writes

    @Test func statusIsReadBeforeTheDiff() async {
        let fake = FakeReviewServices()
        let model = model(.worktree(path: "src/handlers/items.ts"), services: fake)
        await model.load()
        #expect(fake.calls == [.status, .diff])
        #expect(model.actions == [.stage, .discard])
    }

    @Test func stageShowsTheStagedDiff() async {
        let fake = FakeReviewServices()
        let model = model(.worktree(path: "src/handlers/items.ts"), services: fake)
        await model.load()
        await model.perform(.stage)
        #expect(fake.writes == [.stage(["src/handlers/items.ts"])])
        #expect(model.source == .staged(path: "src/handlers/items.ts"))
        #expect(model.actions == [.unstage])
        #expect(model.message == nil)
        await model.perform(.unstage)
        #expect(fake.writes.last == .unstage(["src/handlers/items.ts"]))
        #expect(model.source == .worktree(path: "src/handlers/items.ts"))
    }

    @Test func confirmedDiscardSendsWhatTheQuestionShowed() async throws {
        let fake = FakeReviewServices()
        let model = model(.worktree(path: "src/handlers/items.ts"), services: fake)
        await model.load()
        await model.perform(.discard)
        let question = try #require(model.pendingDiscard)
        #expect(question.selection.paths == ["src/handlers/items.ts"])
        #expect(question.selection.seen.keys.sorted() == ["src/handlers/items.ts"])
        await model.confirm(question)
        #expect(fake.writes == [.discard(["src/handlers/items.ts"])])
    }

    /// Stage and unstage feel done; a discard feels like a warning. Neither feels like approving
    /// an agent's request.
    @Test func writesCountForTheirOwnHaptics() async throws {
        let fake = FakeReviewServices()
        let model = model(.worktree(path: "src/handlers/items.ts"), services: fake)
        await model.load()
        await model.perform(.stage)
        await model.perform(.unstage)
        #expect(model.successes == 2)
        #expect(model.discards == 0)
        await model.perform(.discard)
        await model.confirm(try #require(model.pendingDiscard))
        #expect(model.successes == 2)
        #expect(model.discards == 1)
        #expect(model.completedWrites == 3)
    }

    @Test func hiddenDirectionCharactersAreMarked() async throws {
        let services = ScriptedDiffServices()
        services.diffs["auth.js"] = GitDiff(diff: """
            diff --git a/auth.js b/auth.js
            --- a/auth.js
            +++ b/auth.js
            @@ -1 +1 @@
            -let role = "user"
            +let role = "user\u{202E} \u{2066}// admin\u{2069}\u{2066}"

            """, truncated: false, binary: [])
        let model = model(.worktree(path: "auth.js"), services: services)
        await model.load()
        for split in [false, true] {
            #expect(model.rows(split: split).first == .notice("This file contains hidden direction characters."))
        }
        let added = try #require(lines(model.rows(split: false)).first { $0.kind == .added })
        let shown = #"let role = "user⟨U+202E⟩ ⟨U+2066⟩// admin⟨U+2069⟩⟨U+2066⟩""#
        #expect(added.text == shown)
        #expect(!added.text.unicodeScalars.contains { $0.properties.isBidiControl })
        // Each marker is a token of its own, drawn in the comment colour.
        let markers = added.tokens.filter { $0.kind == .hidden }.map { (shown as NSString).substring(with: NSRange($0.range)) }
        #expect(markers == ["⟨U+202E⟩", "⟨U+2066⟩", "⟨U+2069⟩", "⟨U+2066⟩"])
        // The string token still covers the whole literal, markers included.
        let literal = added.tokens.first { $0.kind == .string }?.range
        #expect(literal?.upperBound == (shown as NSString).length)
        let raw = "let role = \"user\u{202E} \u{2066}// admin\u{2069}\u{2066}\""
        let index = try #require(model.rows(split: false).firstIndex { $0.copyText == raw })
        #expect(model.rows(split: false)[index].accessibilityLabel.contains("hidden character right-to-left override"))
        // A comment quotes the line as the file has it.
        #expect(model.commentDraft(row: index)?.quote == raw)

        // A plain diff gets no banner.
        let plain = self.model(.worktree(path: "src/handlers/items.ts"))
        await plain.load()
        #expect(plain.rows(split: false).first?.isHunk == true)
    }

    @Test func hostileNameIsMarkedOnScreen() {
        let model = model(.worktree(path: "a\u{202E}b/c\u{200B}d.ts"))
        #expect(model.displayName == "c⟨U+200B⟩d.ts")
        #expect(model.displayFolder == "a⟨U+202E⟩b")
        #expect(model.displayPath == "a⟨U+202E⟩b/c⟨U+200B⟩d.ts")
    }

    @Test func discardNeedsConfirmation() async {
        let fake = FakeReviewServices()
        let model = model(.worktree(path: "src/handlers/items.ts"), services: fake)
        await model.load()
        await model.perform(.discard)
        #expect(fake.writes.isEmpty)
        #expect(model.pendingDiscard?.title == "Discard changes to src/handlers/items.ts?")
        #expect(model.pendingDiscard?.message == "This puts it back as it was in the last commit.")
        model.cancelDiscard()
        #expect(model.pendingDiscard == nil)
        #expect(fake.writes.isEmpty)
        await model.perform(.discard)
        await model.confirmDiscard()
        #expect(fake.writes == [.discard(["src/handlers/items.ts"])])
        #expect(model.state == .discarded)
        #expect(model.actions.isEmpty)
    }

    @Test func untrackedDiscardSaysItDeletes() async {
        let model = model(.untracked(path: "notes.txt"))
        await model.load()
        #expect(model.actions == [.stage, .discard])
        await model.perform(.discard)
        #expect(model.pendingDiscard?.title == "Delete notes.txt?")
        #expect(model.pendingDiscard?.message == "It was never committed, so it cannot be recovered.")
    }

    @Test func addedInIndexDiscardSaysItDeletes() async {
        var status = ReviewFixtures.status
        status.files.append(ReviewFixtures.file("src/fresh.ts", "A", "M"))
        let fake = FakeReviewServices(status: status)
        let model = model(.worktree(path: "src/fresh.ts"), services: fake)
        await model.load()
        #expect(model.actions == [.stage, .discard])
        #expect(model.discardActionTitle == "Delete")
        await model.perform(.discard)
        #expect(model.pendingDiscard?.title == "Delete src/fresh.ts?")
        #expect(model.pendingDiscard?.message == "It was never committed, so it cannot be recovered.")
        await model.confirmDiscard()
        #expect(fake.writes == [.discard(["src/fresh.ts"])])
        #expect(model.emptyTitle == "File deleted")
    }

    @Test func renameDiscardSaysItUndoesTheRename() async {
        var status = ReviewFixtures.status
        status.files.removeAll { $0.path == "src/store.ts" }
        status.files.append(ReviewFixtures.file("src/store.ts", "R", "M", from: "src/db.ts"))
        let model = model(.worktree(path: "src/store.ts"), services: FakeReviewServices(status: status))
        await model.load()
        #expect(model.discardActionTitle == "Undo rename")
        await model.perform(.discard)
        #expect(model.pendingDiscard?.title == "Undo the rename?")
        #expect(model.pendingDiscard?.message == "src/db.ts comes back and src/store.ts is removed.")
        #expect(model.pendingDiscard?.button == "Undo rename")
        await model.confirmDiscard()
        #expect(model.emptyTitle == "Rename undone")
        #expect(model.emptyMessage == "src/db.ts is back as it was in the last commit.")
    }

    @Test func caseOnlyRenameRefusalNamesTheMachine() async {
        var status = ReviewFixtures.status
        status.files.removeAll { $0.path == "src/store.ts" }
        status.files.append(ReviewFixtures.file("src/store.ts", "R", "M", from: "src/Store.ts"))
        let fake = FakeReviewServices(status: status)
        fake.failures[.discard] = HelperError.conflict(GitConflict.caseRename.rawValue)
        let model = DiffModel(
            subject: subject(managed: true), services: fake, source: .worktree(path: "src/store.ts"), sendToSession: nil,
            machine: "Studio"
        )
        await model.load()
        await model.perform(.discard)
        #expect(model.pendingDiscard?.message == "src/Store.ts comes back and src/store.ts is removed.")
        await model.confirmDiscard()
        #expect(model.message == DiffModel.Message(
            text: "Can't undo a rename that only changes letter case from this device. Use git on Studio.", retry: false
        ))
    }

    @Test func discardAsksInTheChangesListWords() async {
        let model = model(.worktree(path: "src/handlers/items.ts"))
        await model.load()
        await model.perform(.discard)
        #expect(model.pendingDiscard?.button == "Discard")
        #expect(model.discardActionTitle == "Discard")
    }

    @Test func staleWriteRefreshes() async {
        let fake = FakeReviewServices()
        let model = model(.worktree(path: "src/handlers/items.ts"), services: fake)
        await model.load()
        fake.editBehindBack("src/handlers/items.ts")
        await model.perform(.stage)
        #expect(fake.writes.isEmpty)
        #expect(model.message == DiffModel.Message(text: "This file changed since you looked. Check it again.", retry: false))
        #expect(fake.calls.suffix(2) == [.status, .diff])
        // The refreshed status is what the next write sends.
        await model.perform(.stage)
        #expect(fake.writes == [.stage(["src/handlers/items.ts"])])
    }

    @Test func staleDiscardConfirmationIsDropped() async {
        let fake = FakeReviewServices()
        let model = model(.worktree(path: "src/handlers/items.ts"), services: fake)
        await model.load()
        await model.perform(.discard)
        fake.editBehindBack("src/handlers/items.ts")
        await model.confirmDiscard()
        #expect(fake.writes.isEmpty)
        #expect(model.pendingDiscard == nil)
        #expect(model.message?.text == "This file changed since you looked. Check it again.")
    }

    @Test func lockedOffersRetry() async {
        let fake = FakeReviewServices()
        let model = model(.worktree(path: "src/handlers/items.ts"), services: fake)
        await model.load()
        fake.failures[.stage] = HelperError.locked
        await model.perform(.stage)
        #expect(model.message == DiffModel.Message(text: "Git is busy in this folder. Try again in a moment.", retry: true))
        fake.failures[.stage] = nil
        await model.retry()
        #expect(fake.writes == [.stage(["src/handlers/items.ts"])])
        #expect(model.message == nil)
    }

    @Test func partialWriteRefreshes() async {
        let fake = FakeReviewServices()
        let model = model(.worktree(path: "src/handlers/items.ts"), services: fake)
        await model.load()
        fake.failures[.stage] = HelperError.partialWrite(changed: ["src/handlers/items.ts"])
        await model.perform(.stage)
        #expect(model.message?.text == "Some changes were made: src/handlers/items.ts. Refresh to see where things stand.")
        #expect(fake.calls.suffix(2) == [.status, .diff])
    }

    @Test func readOnlySubjectHasNoActions() async {
        let fake = FakeReviewServices()
        let model = model(.worktree(path: "src/handlers/items.ts"), services: fake, managed: false)
        await model.load()
        #expect(model.actions.isEmpty)
        #expect(fake.calls == [.diff])
        await model.perform(.stage)
        #expect(fake.writes.isEmpty)
    }

    @Test func fileGoneFromStatusHasNoActions() async {
        let model = model(.worktree(path: "src/unlisted.ts"))
        await model.load()
        #expect(model.actions.isEmpty)
    }
}
