import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct ReviewStateTests {
    private func subject(managed: Bool = true, capabilities: Set<String>) -> ReviewSubject {
        ReviewSubject(
            hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
            managed: managed, capabilities: capabilities
        )
    }

    @Test func subjectWriteNeedsManagedAndCapability() {
        let write: Set = [Capability.git, Capability.gitWrite]
        #expect(subject(capabilities: write).canWrite)
        #expect(!subject(managed: false, capabilities: write).canWrite)
        #expect(!subject(capabilities: [Capability.git]).canWrite)
    }

    @Test func subjectReadNeedsGit() {
        #expect(subject(capabilities: [Capability.git]).canRead)
        #expect(!subject(capabilities: [Capability.gitWrite, Capability.ports]).canRead)
        #expect(!subject(capabilities: []).canRead)
        #expect(subject(capabilities: [Capability.ports]).canPorts)
        #expect(!subject(capabilities: [Capability.git]).canPorts)
    }

    @Test func openReviewBuildsSubjectFromRow() {
        var state = Samples.controlCenter
        state.machines[0].capabilities = [Capability.git, Capability.gitWrite]
        let store = AppStore(services: FakeAppServices(), controlCenter: state)
        let row = state.machines[0].sessions[0]
        store.review(row, tab: .changes)
        let expected = ReviewSubject(
            hostID: Samples.studio, sessionID: row.id, bib: "01", title: "api", cwd: "/Users/sam/src/api",
            managed: true, capabilities: [Capability.git, Capability.gitWrite]
        )
        #expect(store.sheet == .review(expected, .changes))

        let shell = state.machines[0].sessions[2]
        store.review(shell, tab: .files)
        guard case .review(let unmanaged, .files)? = store.sheet else {
            Issue.record("no review sheet")
            return
        }
        #expect(!unmanaged.managed && !unmanaged.canWrite && unmanaged.canRead)
    }

    @Test func openReviewOnMachineWithoutCapabilitiesStillOpens() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.review(Samples.controlCenter.machines[0].sessions[0], tab: .ports)
        guard case .review(let found, .ports)? = store.sheet else {
            Issue.record("no review sheet")
            return
        }
        #expect(found.capabilities.isEmpty && !found.canRead)
    }

    @Test func fakeServesFixtures() async throws {
        let fake = FakeReviewServices()
        let s = subject(capabilities: [Capability.git, Capability.gitWrite, Capability.ports])
        let status = try await fake.status(s)
        let letters = Set(status.files.map(\.letters))
        #expect(status.files.contains { $0.index != "." } && status.files.contains { $0.worktree == "M" })
        #expect(status.files.contains { $0.letters == "??" })
        #expect(status.files.contains { $0.origPath != nil })
        #expect(letters.count >= 4)
        #expect(try await fake.log(s, ref: nil, limit: 100).count == 60)
        #expect(try await fake.ports(s).count == 3)
        #expect(try await fake.branches(s).branches.count >= 2)
        #expect(try await fake.listDir(s, path: "", ref: nil).entries.contains { $0.name == "newpkg" })
        let big = try await fake.diff(s, .worktree(path: ReviewFixtures.bigDiffPath))
        #expect(big.diff.split(separator: "\n", omittingEmptySubsequences: false).count > 10_000)
        for file in status.files where file.letters != "??" {
            let diff = try await fake.diff(s, .worktree(path: file.path))
            #expect(!diff.diff.isEmpty || !diff.binary.isEmpty, Comment(rawValue: file.path))
        }
    }

    @Test func failuresAreSwitchablePerCall() async throws {
        let fake = FakeReviewServices()
        let s = subject(capabilities: [Capability.git])
        fake.failures[.status] = ReviewFixtureError.offline
        await #expect(throws: ReviewFixtureError.self) { try await fake.status(s) }
        #expect(try await fake.log(s, ref: nil, limit: 5).count == 5)
        fake.failures[.status] = nil
        _ = try await fake.status(s)
        #expect(fake.calls == [.status, .log, .status])
    }

    private let writer = Set([Capability.git, Capability.gitWrite])

    @Test func stageSendsWhatTheUserSawAndUpdatesStatus() async throws {
        let fake = FakeReviewServices()
        let s = subject(capabilities: writer)
        let status = try await fake.status(s)
        let target = try #require(status.files.first { $0.worktree == "M" })
        let selection = try ReviewSelection(status, paths: [target.path])
        #expect(selection.seen[target.path] == GitSeen(target))
        let after = try await fake.stage(s, paths: selection.paths, seen: selection.seen)
        #expect(after.files.first { $0.path == target.path }?.index == "M")
        #expect(fake.writes.last == .stage([target.path]))
    }

    @Test func staleWriteIsRefusedAsConflictAndChangesNothing() async throws {
        let fake = FakeReviewServices()
        let s = subject(capabilities: writer)
        let status = try await fake.status(s)
        let target = try #require(status.files.first { $0.worktree == "M" })
        let selection = try ReviewSelection(status, paths: [target.path])
        fake.editBehindBack(target.path)
        await #expect(throws: HelperError.stale) {
            try await fake.discard(s, paths: selection.paths, seen: selection.seen)
        }
        #expect(fake.current.files.contains { $0.path == target.path })
        #expect(fake.writes.isEmpty)
        #expect(HelperError.stale.gitConflict == .stale)
        // A path the status never listed is stale too.
        await #expect(throws: HelperError.stale) { try await fake.stage(s, paths: ["nope.txt"], seen: [:]) }
    }

    @Test func lockedIsTheHelpersConflict() async throws {
        let fake = FakeReviewServices()
        let s = subject(capabilities: writer)
        let status = try await fake.status(s)
        let selection = try ReviewSelection(status, paths: [status.files[0].path])
        fake.failures[.stage] = HelperError.locked
        await #expect(throws: HelperError.locked) {
            try await fake.stage(s, paths: selection.paths, seen: selection.seen)
        }
        #expect((HelperError.locked).gitConflict == .locked)
    }

    @Test func unmanagedSessionRefusesEveryWrite() async throws {
        let fake = FakeReviewServices()
        let s = subject(managed: false, capabilities: writer)
        let status = try await fake.status(s)
        let selection = try ReviewSelection(status, paths: [status.files[0].path])
        guard case .rpc(let error) = HelperError.unmanaged else { return }
        #expect(error.code == -32007)
        await #expect(throws: HelperError.unmanaged) { try await fake.stage(s, paths: selection.paths, seen: selection.seen) }
        await #expect(throws: HelperError.unmanaged) { try await fake.unstage(s, paths: selection.paths, seen: selection.seen) }
        await #expect(throws: HelperError.unmanaged) { try await fake.discard(s, paths: selection.paths, seen: selection.seen) }
        await #expect(throws: HelperError.unmanaged) { try await fake.commit(s, message: "m", amend: false, head: nil) }
        #expect(fake.writes.isEmpty)
        // Reads still work.
        _ = try await fake.diff(s, .worktree(path: status.files[0].path))
    }

    @Test func amendNamesTheCommitItReplaces() async throws {
        let fake = FakeReviewServices()
        let s = subject(capabilities: writer)
        let seenHead = try #require(try await fake.log(s, ref: nil, limit: 1).first).hash
        fake.moveHead()
        await #expect(throws: HelperError.stale) { try await fake.commit(s, message: "m", amend: true, head: seenHead) }
        let result = try await fake.commit(s, message: "m", amend: false, head: nil)
        #expect(result.short == "bbbbbbb")
    }

    @Test func truncatedStatusCannotBeSelectedAsAll() throws {
        #expect(throws: ReviewSelection.Failure.truncated) { try ReviewSelection.all(in: ReviewFixtures.truncatedStatus) }
        let whole = try ReviewSelection.all(in: ReviewFixtures.status) { $0.worktree == "M" }
        #expect(!whole.paths.isEmpty && whole.seen.count == whole.paths.count)
        // Choosing files one by one still works on a truncated list.
        let one = try ReviewSelection(ReviewFixtures.truncatedStatus, paths: ["src/router.ts"])
        #expect(one.paths == ["src/router.ts"])
    }

    @Test func selectionRefusesUnseenAndInvalidPaths() {
        let status = ReviewFixtures.status
        #expect(throws: ReviewSelection.Failure.notShown("zzz")) { try ReviewSelection(status, paths: ["zzz"]) }
        #expect(throws: ReviewSelection.Failure.invalidPath("newpkg/")) { try ReviewSelection(status, paths: ["newpkg/"]) }
        #expect(throws: ReviewSelection.Failure.empty) { try ReviewSelection(status, paths: []) }
    }
}
