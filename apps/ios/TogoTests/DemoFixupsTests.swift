import Foundation
import Testing
import TogoKit
@testable import Togo

/// The status project's history and tree for `-ui-demo-api`, one title on every Review tab, and
/// a roster headline that wraps without running under the first machine's heading.
@MainActor
struct DemoFixupsTests {
    private let subject = ReviewSubject(
        hostID: DemoMachine.id, sessionID: "s-01", bib: "01", title: "status", cwd: "/Users/sam/status",
        managed: true, capabilities: DemoServices.capabilities
    )

    @Test func theCapturesReviewTheStatusProjectsHistoryAndTree() async throws {
        let project = try #require(DemoProject.load())
        let written = Set(project.status.map(\.path))
        let review = DemoReviewServices(project: project, root: "/Users/sam/status", started: Samples.now) { written }

        let log = try await review.log(subject, ref: nil, limit: 50)
        #expect(log.map(\.subject) == project.commits.map(\.subject))
        #expect(log.first?.parents == [project.commits[1].hash])
        #expect(log.last?.parents == [])
        let first = try #require(project.commits.last)
        let diff = try await review.diff(subject, .commit(hash: first.hash, path: nil))
        #expect(diff.diff.contains("diff --git a/src/server.ts"))

        #expect(try await review.status(subject).root == "/Users/sam/status")
        let root = try await review.listDir(subject, path: "", ref: nil, base: .session)
        #expect(root.entries.prefix(4).map(\.name) == ["data", "public", "src", "test"])
        let src = try await review.listDir(subject, path: "/Users/sam/status/src", ref: nil, base: .session)
        #expect(src.entries.map(\.name).contains("incidents.ts"))
        let read = try await review.read(subject, path: "/Users/sam/status/src/render.ts", ref: nil, base: .session)
        #expect(read.content == project.work["src/render.ts"])
    }

    @Test func everyReviewTabHasOneTitle() {
        #expect(subject.displayTitle == "01 status")
        let files = FilesModel(subject: subject, services: NoReviewServices(), path: "", ref: nil)
        #expect(files.title == subject.displayTitle)
        let folder = FilesModel(subject: subject, services: NoReviewServices(), path: "src/routes", ref: nil)
        #expect(folder.title == "routes")
        var unnumbered = subject
        unnumbered.bib = nil
        #expect(unnumbered.displayTitle == "status")
    }

    /// One line gives back the target's blank room; a wrapped headline keeps all of its height.
    @Test func aWrappedHeadlineIsNeverClipped() {
        #expect(RosterFilterHeader.overlap(words: 18) == 8)
        #expect(RosterFilterHeader.overlap(words: 36) == 4)
        #expect(RosterFilterHeader.overlap(words: 62) == 0)
        #expect(RosterFilterHeader.overlap(words: 0) == 8)
    }
}
