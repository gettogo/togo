import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct HistoryModelTests {
    private let subject = ReviewSubject(
        hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
        managed: true, capabilities: [Capability.git]
    )

    private func commits(_ count: Int, prefix: String = "") -> [GitCommit] {
        (0..<count).map { n in
            let hash = String(format: "%040x", n + 1)
            return GitCommit(
                hash: hash, short: String(hash.suffix(7)), author: "Sam", date: ReviewFixtures.now, subject: "\(prefix)Change \(n)"
            )
        }
    }

    @Test func logPagesUpTo200() async {
        let services = FakeReviewServices()
        services.commits = commits(260)
        let model = HistoryModel(subject: subject, services: services)
        await model.load()
        #expect(model.commits.count == 50)
        #expect(model.canLoadMore)
        for expected in [100, 150, 200] {
            await model.loadMore()
            #expect(model.commits.count == expected)
        }
        #expect(!model.canLoadMore)
        #expect(model.reachedLimit)
        await model.loadMore()
        #expect(model.commits.count == 200)
        #expect(services.logRequests.map(\.limit) == [50, 100, 150, 200])
    }

    @Test func shortHistoryHasNoMore() async {
        let services = FakeReviewServices()
        services.commits = commits(12)
        let model = HistoryModel(subject: subject, services: services)
        await model.load()
        #expect(model.commits.count == 12)
        #expect(!model.canLoadMore)
        #expect(!model.reachedLimit)
    }

    @Test func branchesListCurrentFirst() async {
        let services = FakeReviewServices()
        let model = HistoryModel(subject: subject, services: services)
        await model.load()
        #expect(model.branches.map(\.name) == ["main", "fix/ids", "release/1.4"])
        #expect(HistoryCopy.upstream("origin/main", current: true).contains("origin/main"))
        #expect(HistoryCopy.upstream(nil, current: false) == HistoryCopy.localOnly)
    }

    @Test func branchFilterReloadsLog() async {
        let services = FakeReviewServices()
        services.commits = commits(60)
        services.refLogs["fix/ids"] = commits(3, prefix: "ids: ")
        let model = HistoryModel(subject: subject, services: services)
        await model.load()
        await model.loadMore()
        #expect(model.commits.count == 60)
        #expect(model.shownBranch == "main")
        await model.select(branch: "fix/ids")
        #expect(model.selectedRef == "fix/ids")
        #expect(model.shownBranch == "fix/ids")
        #expect(model.commits.map(\.subject) == ["ids: Change 0", "ids: Change 1", "ids: Change 2"])
        #expect(services.logRequests.last?.ref == "fix/ids")
        #expect(services.logRequests.last?.limit == 50)
        // Picking it again keeps it; picking the current branch follows HEAD again.
        let requests = services.logRequests.count
        await model.select(branch: "fix/ids")
        #expect(services.logRequests.count == requests)
        await model.select(branch: "main")
        #expect(model.selectedRef == nil)
        #expect(model.shownBranch == "main")
        #expect(model.commits.count == 50)
        #expect(services.logRequests.last?.ref == nil)
    }

    @Test func failedLogShowsMessageAndRetries() async {
        let services = FakeReviewServices()
        services.failures[.log] = ReviewFixtureError.offline
        let model = HistoryModel(subject: subject, services: services)
        await model.load()
        #expect(model.state == .failed("The machine didn't answer."))
        services.failures[.log] = nil
        await model.load()
        #expect(model.state == .ready)
    }

    @Test func commitOpensFileList() async {
        let services = FakeReviewServices()
        let history = HistoryModel(subject: subject, services: services)
        await history.load()
        let commit = history.commits[0]
        #expect(history.route(for: commit) == .diff(.commit(hash: commit.hash, path: nil)))
        #expect(history.filesRoute(for: commit) == .commit(commit))

        let detail = CommitDetailModel(subject: subject, services: services, commit: commit)
        await detail.load()
        #expect(detail.state == .ready)
        #expect(detail.files.map(\.id) == ["src/router.ts", "src/handlers/health.ts", "src/store.ts"])
        #expect(detail.route(for: detail.files[0]) == .diff(.commit(hash: commit.hash, path: "src/router.ts")))
        #expect(detail.browseRoute == .folder(path: "", ref: commit.hash))
    }

    // MARK: Graph

    private func node(_ name: String, _ parents: [String]) -> GitCommit {
        GitCommit(hash: name, short: name, author: "Sam", date: ReviewFixtures.now, subject: name, parents: parents)
    }

    @Test func straightHistoryIsOneLane() {
        let rows = HistoryGraph.rows([node("c", ["b"]), node("b", ["a"]), node("a", [])])
        #expect(rows.map(\.lane) == [0, 0, 0])
        #expect(rows.map(\.width) == [1, 1, 1])
        #expect(rows[0].top.isEmpty)
        #expect(rows[0].bottom == [.init(from: 0, to: 0, lane: 0)])
        #expect(rows[1].top == [.init(from: 0, to: 0, lane: 0)])
        #expect(rows[2].bottom.isEmpty)
        let merges = rows.contains(where: \.merge)
        #expect(!merges)
    }

    @Test func mergeOpensALaneThatMeetsAtTheFork() {
        // m merges s into main; s and b both come from a.
        let rows = HistoryGraph.rows([
            node("m", ["b", "s"]), node("b", ["a"]), node("s", ["a"]), node("a", []),
        ])
        #expect(rows[0].merge)
        #expect(rows[0].bottom.contains(.init(from: 0, to: 1, lane: 1)))
        #expect(rows.map(\.lane) == [0, 0, 1, 0])
        // b's row carries s's lane past it.
        #expect(rows[1].top.contains(.init(from: 1, to: 1, lane: 1)))
        #expect(rows[1].bottom.contains(.init(from: 1, to: 1, lane: 1)))
        // At a, the side lane bends back into the first.
        #expect(rows[3].top.contains(.init(from: 1, to: 0, lane: 1)))
        #expect(rows[3].top.contains(.init(from: 0, to: 0, lane: 0)))
        #expect(rows[3].width == 2)
    }

    @Test func logWithoutParentsIsDrawnAsOneLine() {
        let rows = HistoryGraph.rows([node("c", []), node("b", []), node("a", [])])
        #expect(rows.map(\.lane) == [0, 0, 0])
        #expect(rows[1].top == [.init(from: 0, to: 0, lane: 0)])
    }

    @Test func modelKeepsGraphInStepWithPages() async {
        let services = FakeReviewServices()
        let model = HistoryModel(subject: subject, services: services)
        await model.load()
        #expect(model.graph.count == model.commits.count)
        #expect(model.graph[1].merge)
        #expect(model.graphWidth == 2)
        await model.loadMore()
        #expect(model.graph.count == model.commits.count)
    }

    @Test func relativeDatesUseTheClockGiven() async {
        let services = FakeReviewServices()
        let now = ReviewFixtures.now.addingTimeInterval(3 * 3_600)
        let model = HistoryModel(subject: subject, services: services, now: { now })
        let text = model.when(GitCommit(hash: "a", short: "a", author: "x", date: ReviewFixtures.now, subject: "s"))
        #expect(text.contains("3"))
    }

    @Test func hostileCommitTextIsNeutralised() {
        let commit = GitCommit(
            hash: "a", short: "a", author: "Mal\u{202E}lory", date: ReviewFixtures.now, subject: "fix\u{1B}[31m\u{202E}txt.exe"
        )
        #expect(commit.displaySubject == "fix⟨U+001B⟩[31m⟨U+202E⟩txt.exe")
        #expect(commit.displayAuthor == "Mal⟨U+202E⟩lory")
        #expect(GitBranches.Branch(name: "x\u{202E}y", upstream: nil, current: false).displayName == "x⟨U+202E⟩y")
    }
}
