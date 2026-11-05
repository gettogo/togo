import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor
struct ReviewHistorySnapshotTests {
    private func history(_ services: FakeReviewServices = FakeReviewServices()) async -> HistoryModel {
        let model = HistoryModel.preview(services: services)
        await model.load()
        return model
    }

    private func png() -> Data {
        UIGraphicsImageRenderer(size: CGSize(width: 240, height: 160)).pngData { context in
            UIColor.systemTeal.setFill()
            context.fill(CGRect(x: 0, y: 0, width: 240, height: 160))
            UIColor.white.setFill()
            context.fill(CGRect(x: 60, y: 40, width: 120, height: 80))
        }
    }

    @Test func historyList() async {
        let services = FakeReviewServices()
        services.branchList = GitBranches(current: "main", branches: [.init(name: "main", upstream: "origin/main", current: true)])
        let model = HistoryModel.preview(services: services)
        await model.load()
        assertScreen(named: "history") { NavigationStack { HistoryView(model: model, onOpen: { _ in }) } }
    }

    @Test func historyBranches() async {
        let model = await history()
        assertScreen(named: "history-branches") { NavigationStack { HistoryView(model: model, onOpen: { _ in }) } }
    }

    @Test func historyFilteredAndCapped() async {
        let services = FakeReviewServices()
        services.commits = ReviewFixtures.makeLog(240)
        services.refLogs["fix/ids"] = services.commits
        let model = await history(services)
        await model.select(branch: "fix/ids")
        while model.canLoadMore { await model.loadMore() }
        assertScreen(named: "history-capped") { NavigationStack { HistoryView(model: model, onOpen: { _ in }) } }
    }

    @Test func historyEmptyAndError() async {
        let empty = FakeReviewServices()
        empty.commits = []
        let model = await history(empty)
        assertScreen(named: "history-empty") { NavigationStack { HistoryView(model: model, onOpen: { _ in }) } }
        let broken = FakeReviewServices()
        broken.failures[.log] = ReviewFixtureError.offline
        let failed = await history(broken)
        assertScreen(named: "history-error") { NavigationStack { HistoryView(model: failed, onOpen: { _ in }) } }
    }

    @Test func commitDetail() async {
        let model = CommitDetailModel(
            subject: DiffModel.previewSubject, services: FakeReviewServices(), commit: ReviewFixtures.log[0]
        )
        await model.load()
        assertScreen(named: "commit") { NavigationStack { CommitDetailView(model: model, onOpen: { _ in }) } }
    }

    @Test func browserRoot() async {
        let services = FakeReviewServices()
        services.trees[""] = ReviewFixtures.tree[""]! + [
            DirEntry(name: ".gitignore", dir: false, kind: .file, size: 120),
            DirEntry(name: "current", dir: false, kind: .symlink),
        ]
        let model = FilesModel.preview(services: services)
        await model.load()
        assertScreen(named: "browser") { NavigationStack { FileBrowserView(model: model, onOpen: { _ in }) } }
    }

    @Test func browserAtCommit() async {
        let model = FilesModel.preview(path: "src/handlers", ref: ReviewFixtures.log[2].hash)
        await model.load()
        assertScreen(named: "browser-commit") { NavigationStack { FileBrowserView(model: model, onOpen: { _ in }) } }
    }

    @Test func browserEmptyHidden() async {
        let services = FakeReviewServices()
        services.trees[""] = [DirEntry(name: ".keep", dir: false, kind: .file, size: 0)]
        let model = FilesModel.preview(services: services)
        await model.load()
        assertScreen(named: "browser-hidden") { NavigationStack { FileBrowserView(model: model, onOpen: { _ in }) } }
    }

    @Test func viewerCode() async {
        let model = FileViewerModel.preview("src/router.ts", ref: ReviewFixtures.log[2].hash)
        await model.load()
        assertScreen(named: "viewer") { NavigationStack { FileViewer(model: model) } }
    }

    @Test func viewerFind() async {
        let model = FileViewerModel.preview("src/router.ts")
        await model.load()
        model.query = "app"
        await model.settle()
        assertScreen(named: "viewer-find") { NavigationStack { FileViewer(model: model) } }
    }

    @Test func viewerTruncated() async {
        let services = FakeReviewServices()
        let path = "deploy/logs/prod/eu/server.log"
        services.reads[path] = FsReadResult(
            path: path, size: 900_000, encoding: .utf8,
            content: "listening on 3000\nGET /items 200\nGET /items/9 404\n", truncated: true
        )
        let model = FileViewerModel.preview(path, services: services)
        await model.load()
        assertScreen(named: "viewer-truncated") { NavigationStack { FileViewer(model: model) } }
    }

    @Test func viewerBinary() async {
        let services = FakeReviewServices()
        services.reads["bin/tool"] = FsReadResult(path: "bin/tool", size: 1_200_000, encoding: .base64, content: "AAAA", truncated: true)
        let model = FileViewerModel.preview("bin/tool", services: services)
        await model.load()
        assertScreen(named: "viewer-binary") { NavigationStack { FileViewer(model: model) } }
    }

    @Test func viewerImage() async {
        let data = png()
        let services = FakeReviewServices()
        services.reads["assets/logo.png"] = FsReadResult(
            path: "assets/logo.png", size: data.count, encoding: .base64, content: data.base64EncodedString(), truncated: false
        )
        let model = FileViewerModel.preview("assets/logo.png", services: services)
        await model.load()
        assertScreen(named: "viewer-image") { NavigationStack { FileViewer(model: model) } }
    }
}
