import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct ReviewChangesSnapshotTests {
    private func subject(managed: Bool = true) -> ReviewSubject {
        ReviewSubject(
            hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
            managed: managed, capabilities: [Capability.git, Capability.gitWrite, Capability.ports]
        )
    }

    private func model(
        _ status: GitStatus = ReviewFixtures.status, managed: Bool = true,
        prepare: (FakeReviewServices) -> Void = { _ in }
    ) async -> (ChangesModel, FakeReviewServices) {
        let services = FakeReviewServices(status: status)
        prepare(services)
        let model = ChangesModel(subject: subject(managed: managed), services: services, machine: "Studio")
        await model.refresh()
        return (model, services)
    }

    private func screen(_ model: ChangesModel) -> some View {
        NavigationStack {
            ChangesView(model: model) { _ in }
                .navigationTitle("Changes")
                .navigationBarTitleDisplayMode(.inline)
        }
        .togoControls()
    }

    @Test func list() async {
        var status = ReviewFixtures.status
        status.files.append(ReviewFixtures.file("src/merge.ts", "U", "U"))
        let (model, _) = await model(status)
        assertScreen(named: "changes-list") { screen(model) }
    }

    @Test func readOnly() async {
        let (model, _) = await model(managed: false)
        assertScreen(named: "changes-read-only") { screen(model) }
    }

    @Test func truncated() async {
        let (model, _) = await model(ReviewFixtures.truncatedStatus)
        assertScreen(named: "changes-truncated") { screen(model) }
    }

    @Test func empty() async {
        var status = ReviewFixtures.status
        status.files = []
        status.ahead = 0
        let (model, _) = await model(status)
        assertScreen(named: "changes-empty") { screen(model) }
    }

    @Test func notRepo() async {
        let status = GitStatus(repo: false, root: nil, branch: nil, ahead: 0, behind: 0, truncated: false, files: [])
        let (model, _) = await model(status)
        assertScreen(named: "changes-not-repo") { screen(model) }
    }

    @Test func loading() {
        let services = FakeReviewServices()
        services.delays[.status] = .seconds(60)
        let model = ChangesModel(subject: subject(), services: services, machine: "Studio")
        assertScreen(named: "changes-loading") { screen(model) }
    }

    @Test func failed() async {
        let (model, _) = await model { $0.failures[.status] = ReviewFixtureError.offline }
        assertScreen(named: "changes-failed") { screen(model) }
    }

    @Test func stale() async {
        let (model, services) = await model()
        model.requestDiscard(["src/handlers/items.ts"])
        services.editBehindBack("src/handlers/items.ts")
        await model.confirm()
        assertScreen(named: "changes-stale") { screen(model) }
    }

    @Test func locked() async {
        let (model, services) = await model()
        services.failures[.stage] = HelperError.locked
        await model.stage(["src/handlers/items.ts"])
        assertScreen(named: "changes-locked") { screen(model) }
    }

    @Test func partial() async {
        let (model, services) = await model()
        services.failures[.discard] = HelperError.partialWrite(changed: ["notes.txt", "assets/logo.png"])
        model.requestDiscard(["notes.txt", "assets/logo.png", "generated/table.json"])
        await model.confirm()
        assertScreen(named: "changes-partial") { screen(model) }
    }

    @Test func commitSheet() async {
        let (model, _) = await model()
        model.openCommitSheet()
        model.message = "Validate ids before the lookup\n\nUnknown ids now return 404 instead of an empty body."
        assertScreen(named: "changes-commit") { CommitSheet(model: model) }
    }

    @Test func commitAmendPushed() async {
        var status = ReviewFixtures.status
        status.ahead = 0
        let (model, _) = await model(status)
        model.openCommitSheet()
        model.amend = true
        model.message = "Return 404 for unknown ids"
        assertScreen(named: "changes-commit-amend") { CommitSheet(model: model) }
    }

    @Test func commitLongSubjectAndError() async {
        let (model, services) = await model()
        model.openCommitSheet()
        model.message = "Validate every id before the lookup so that unknown ids return a 404 to the caller"
        services.failures[.commit] = HelperError.conflict(GitConflict.identity.rawValue)
        _ = await model.commit(message: model.message, amend: false)
        assertScreen(named: "changes-commit-error") { CommitSheet(model: model) }
    }
}
