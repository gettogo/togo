import Foundation
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct FilesModelTests {
    private let subject = ReviewSubject(
        hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
        managed: true, capabilities: [Capability.git]
    )

    private func folder(
        _ entries: [DirEntry], path: String = "", ref: String? = nil
    ) async -> FilesModel {
        let services = FakeReviewServices()
        services.trees[path] = entries
        let model = FilesModel(subject: subject, services: services, path: path, ref: ref)
        await model.load()
        return model
    }

    private func viewer(_ path: String, _ result: FsReadResult? = nil) async -> FileViewerModel {
        let services = FakeReviewServices()
        if let result { services.reads[path] = result }
        let model = FileViewerModel(subject: subject, services: services, path: path, ref: nil)
        await model.load()
        return model
    }

    private func png() -> Data {
        UIGraphicsImageRenderer(size: CGSize(width: 8, height: 8)).pngData { context in
            UIColor.systemTeal.setFill()
            context.fill(CGRect(x: 0, y: 0, width: 8, height: 8))
        }
    }

    // MARK: Tree

    @Test func foldersOpenInPlaceAndCloseKeepingWhatTheyRead() async {
        let services = FakeReviewServices()
        let model = FilesModel(subject: subject, services: services, path: "", ref: nil)
        await model.load()
        #expect(model.children(of: "src") == nil)
        await model.setExpanded("src", true)
        #expect(model.isExpanded("src"))
        #expect(model.children(of: "src")?.map(\.name) == ["handlers", "router.ts", "store.ts"])
        #expect(model.children(of: "src")?.first?.path == "src/handlers")
        let reads = services.calls.filter { $0 == .listDir }.count
        await model.setExpanded("src", false)
        await model.setExpanded("src", true)
        #expect(services.calls.filter { $0 == .listDir }.count == reads)
    }

    @Test func refreshReadsOpenFoldersAgainAndForgetsClosedOnes() async {
        let services = FakeReviewServices()
        let model = FilesModel(subject: subject, services: services, path: "", ref: nil)
        await model.load()
        await model.setExpanded("src", true)
        await model.setExpanded("newpkg", true)
        await model.setExpanded("newpkg", false)
        await model.load()
        #expect(model.folders["src"] != nil)
        #expect(model.folders["newpkg"] == nil)
    }

    @Test func changedFilesAndTheirFoldersAreMarked() async {
        let model = FilesModel(subject: subject, services: FakeReviewServices(), path: "", ref: nil)
        await model.load()
        #expect(model.marks.mark("src/router.ts") == .modified)
        #expect(model.marks.mark("src/handlers/health.ts") == .added)
        #expect(model.marks.mark("notes.txt") == .untracked)
        #expect(model.marks.mark("README.md") == nil)
        #expect(model.marks.holdsChanges("src"))
        #expect(model.marks.holdsChanges("src/handlers"))
        #expect(!model.marks.holdsChanges("docs"))
        let untracked = FilesModel.Item(name: "notes.txt", path: "notes.txt", kind: .file, size: 3, route: nil)
        #expect(model.changesRoute(for: untracked) == .diff(.untracked(path: "notes.txt")))
        let staged = FilesModel.Item(name: "router.ts", path: "src/router.ts", kind: .file, size: 3, route: nil)
        #expect(model.changesRoute(for: staged) == .diff(.staged(path: "src/router.ts")))
    }

    @Test func commitsAndSubfoldersMapMarksToTheirOwnPaths() async {
        let atCommit = FilesModel(subject: subject, services: FakeReviewServices(), path: "", ref: ReviewFixtures.log[0].hash)
        await atCommit.load()
        #expect(atCommit.marks == ChangeMarks())

        var status = ReviewFixtures.status
        status.root = "/Users/sam"
        let marks = ChangeMarks(status, prefix: "src")
        #expect(marks.mark("router.ts") == .modified)
        #expect(marks.mark("notes.txt") == nil)
        #expect(ChangeMarks.sessionPrefix(cwd: "/Users/sam/src/api", root: "/Users/sam/src") == "api")
        #expect(ChangeMarks.sessionPrefix(cwd: "/Users/sam/src/api/", root: "/Users/sam/src/api") == "")
        #expect(ChangeMarks.sessionPrefix(cwd: "/var/tmp/x", root: "/private/var/tmp") == "x")
        #expect(ChangeMarks.sessionPrefix(cwd: "/Users/sam/other", root: "/Users/sam/src") == nil)
    }

    // MARK: Search

    @Test func searchRanksNamesBeforePaths() {
        let paths = ["src/handlers/items.ts", "docs/items/guide.md", "items.ts", "src/itemsStore.ts", ".hidden/items.ts"]
        #expect(FileSearch.rank(paths, query: "items.ts", hidden: false, limit: 10) == ["items.ts", "src/handlers/items.ts"])
        #expect(FileSearch.rank(paths, query: "ITEMS", hidden: false, limit: 10) == [
            "items.ts", "src/itemsStore.ts", "src/handlers/items.ts", "docs/items/guide.md",
        ])
        #expect(FileSearch.rank(paths, query: "items", hidden: true, limit: 10).contains(".hidden/items.ts"))
        #expect(FileSearch.rank(paths, query: "items", hidden: false, limit: 2).count == 2)
    }

    @Test func searchLooksThroughTheWholeRepository() async {
        let services = FakeReviewServices()
        let model = FilesModel(subject: subject, services: services, path: "", ref: nil)
        model.search.query = "health"
        await model.search.settle()
        #expect(model.search.hits.map(\.path) == ["src/handlers/health.ts"])
        #expect(model.search.hits.first?.route == .file(path: "src/handlers/health.ts", ref: nil, base: .repository))
        #expect(!model.search.partial)
        model.search.query = ""
        #expect(model.search.hits.isEmpty)
        #expect(!model.search.isActive)
    }

    @Test func searchWithoutTheFileListUsesOpenedFolders() async {
        let services = FakeReviewServices()
        services.failures[.files] = HelperError.unsupported(capability: Capability.gitFiles)
        let model = FilesModel(subject: subject, services: services, path: "", ref: nil)
        await model.load()
        await model.setExpanded("src", true)
        model.search.query = "store"
        await model.search.settle()
        #expect(model.search.partial)
        #expect(model.search.hits.map(\.path) == ["src/store.ts"])
        #expect(model.search.hits.first?.route == .file(path: "src/store.ts", ref: nil, base: .session))
    }

    @Test func browserSortsFoldersFirst() async {
        let model = await folder([
            DirEntry(name: "zeta.txt", dir: false, kind: .file, size: 10),
            DirEntry(name: "docs", dir: true, kind: .dir),
            DirEntry(name: "Alpha.md", dir: false, kind: .file, size: 20),
            DirEntry(name: "src", dir: true, kind: .dir),
            DirEntry(name: "latest", dir: false, kind: .symlink),
        ])
        #expect(model.items.map(\.name) == ["docs", "src", "Alpha.md", "latest", "zeta.txt"])
        #expect(model.items[0].route == .folder(path: "docs", ref: nil))
        #expect(model.items[2].route == .file(path: "Alpha.md", ref: nil))
    }

    @Test func symlinksAreListedNeverOpened() async {
        let model = await folder([
            DirEntry(name: "escape", dir: false, kind: .symlink), DirEntry(name: "a.txt", dir: false, kind: .file, size: 1),
        ])
        let link = model.items.first { $0.name == "escape" }
        #expect(link?.kind == .link)
        #expect(link?.route == nil)
    }

    @Test func hiddenFilesSitBehindAToggle() async {
        let model = await folder([
            DirEntry(name: ".env", dir: false, kind: .file, size: 4), DirEntry(name: ".github", dir: true, kind: .dir),
            DirEntry(name: "main.go", dir: false, kind: .file, size: 4),
        ])
        #expect(model.items.map(\.name) == ["main.go"])
        #expect(model.hiddenCount == 2)
        model.showsHidden = true
        #expect(model.items.map(\.name) == [".github", ".env", "main.go"])
    }

    @Test func nestedPathsJoinOntoTheFolder() async {
        let model = await folder([DirEntry(name: "handlers", dir: true, kind: .dir), DirEntry(name: "a.ts", dir: false, kind: .file, size: 1)], path: "src")
        #expect(model.items[0].route == .folder(path: "src/handlers", ref: nil))
        #expect(model.items[1].route == .file(path: "src/a.ts", ref: nil))
    }

    @Test func repositoryFolderKeepsItsBase() async {
        let services = FakeReviewServices()
        services.trees["apps/tools"] = [DirEntry(name: "bin", dir: true, kind: .dir), DirEntry(name: "a.txt", dir: false, kind: .file, size: 1)]
        let model = FilesModel(subject: subject, services: services, path: "apps/tools", ref: nil, base: .repository)
        await model.load()
        #expect(services.bases["apps/tools"] == .repository)
        #expect(model.items[0].route == .folder(path: "apps/tools/bin", ref: nil, base: .repository))
        #expect(model.items[1].route == .file(path: "apps/tools/a.txt", ref: nil, base: .repository))
    }

    @Test func refBannerAndReturn() async {
        let hash = "a1b2c3d" + String(repeating: "0", count: 33)
        let model = await folder([DirEntry(name: "a.ts", dir: false, kind: .file, size: 1)], path: "src", ref: hash)
        #expect(model.refBanner == "At a1b2c3d")
        #expect(model.items[0].route == .file(path: "src/a.ts", ref: hash))
        #expect(model.workingTreeRoute == .folder(path: "", ref: nil))
        let plain = await folder([])
        #expect(plain.refBanner == nil)
    }

    @Test func deepPathBreadcrumbTruncatesMiddle() {
        #expect(FilesModel.breadcrumb("") == "")
        #expect(FilesModel.breadcrumb("src/handlers") == "src/handlers")
        #expect(FilesModel.breadcrumb("a/b/c/d/e/f/g") == "a/…/f/g")
        #expect(FilesModel.breadcrumb("a/b/c/d") == "a/b/c/d")
    }

    @Test func folderFailureCanRetry() async {
        let services = FakeReviewServices()
        services.failures[.listDir] = ReviewFixtureError.offline
        let model = FilesModel(subject: subject, services: services, path: "", ref: nil)
        await model.load()
        #expect(model.state == .failed("The machine didn't answer."))
        services.failures[.listDir] = nil
        await model.load()
        #expect(model.state == .ready)
    }

    @Test func textFileShowsHighlightedRows() async {
        let model = await viewer("src/router.ts")
        guard case .text(let rows) = model.content else { Issue.record("expected text"); return }
        #expect(rows.count >= 10)
        guard case .line(let number, _, _, _, let tokens, _, _) = rows[0] else { Issue.record("expected a line"); return }
        #expect(number == 1)
        #expect(!tokens.isEmpty)
    }

    @Test func imageFileRendersImage() async {
        let data = png()
        let reply = FsReadResult(path: "assets/logo.png", size: data.count, encoding: .base64, content: data.base64EncodedString(), truncated: false)
        let model = await viewer("assets/logo.png", reply)
        #expect(model.content == .image(data))
    }

    @Test func bigImageIsBinary() async {
        let reply = FsReadResult(path: "photo.jpg", size: 2_000_000, encoding: .base64, content: "AAAA", truncated: true)
        let model = await viewer("photo.jpg", reply)
        #expect(model.content == .binary(size: 2_000_000))
    }

    @Test func binaryNonImageShowsSize() async {
        let reply = FsReadResult(path: "bin/tool", size: 1_200_000, encoding: .base64, content: "AAAA", truncated: true)
        let model = await viewer("bin/tool", reply)
        #expect(model.content == .binary(size: 1_200_000))
        #expect(FilesCopy.binary(size: 1_200_000) == "Binary file, 1.2 MB, not shown")
    }

    @Test func nulBytesInTextAreBinary() async {
        let reply = FsReadResult(path: "data.bin", size: 4, encoding: .utf8, content: "ab\u{0}c", truncated: false)
        let model = await viewer("data.bin", reply)
        #expect(model.content == .binary(size: 4))
    }

    @Test func truncatedFileSaysSo() async {
        let reply = FsReadResult(path: "big.log", size: 900_000, encoding: .utf8, content: "one\ntwo\n", truncated: true)
        let model = await viewer("big.log", reply)
        #expect(model.truncatedNotice == "Showing the first 512 KB.")
        let whole = await viewer("src/router.ts")
        #expect(whole.truncatedNotice == nil)
    }

    @Test func emptyFileSaysSo() async {
        let reply = FsReadResult(path: "empty.txt", size: 0, encoding: .utf8, content: "", truncated: false)
        let model = await viewer("empty.txt", reply)
        #expect(model.content == .empty)
    }

    @Test func findJumpsBetweenMatches() async {
        let text = "alpha\nbeta Item\ngamma\ndelta item item\nepsilon\n"
        let reply = FsReadResult(path: "n.txt", size: text.utf8.count, encoding: .utf8, content: text, truncated: false)
        let model = await viewer("n.txt", reply)
        model.query = "item"
        await model.settle()
        #expect(model.matches.count == 3)
        #expect(model.matches.map(\.row) == [1, 3, 3])
        #expect(model.currentMatch == 0)
        #expect(model.scrollTarget == 1)
        model.next()
        #expect(model.currentMatch == 1)
        #expect(model.scrollTarget == 3)
        model.next()
        model.next()
        #expect(model.currentMatch == 0)
        model.previous()
        #expect(model.currentMatch == 2)
        #expect(model.position == "3 of 3")
        guard case .line(_, _, _, _, _, let changes, _) = model.displayRows[1] else { Issue.record("expected a line"); return }
        #expect(changes == [5..<9])
        model.query = "zzz"
        await model.settle()
        #expect(model.matches.isEmpty)
        #expect(model.position == "No matches")
        model.query = ""
        #expect(model.position == nil)
    }

    @Test func hostileNamesAreNeutralised() async {
        let model = await folder([DirEntry(name: "inv\u{202E}gnp.exe", dir: false, kind: .file, size: 1), DirEntry(name: "a\u{1B}[2Jb", dir: false, kind: .file, size: 1)])
        #expect(model.items.allSatisfy { !$0.name.unicodeScalars.contains { $0.value == 0x202E || $0.value == 0x1B } })
        #expect(model.items.contains { $0.name == "inv⟨U+202E⟩gnp.exe" })
        // The route keeps the real path.
        #expect(model.items.contains { $0.route == .file(path: "inv\u{202E}gnp.exe", ref: nil) })
        #expect(FilesModel.breadcrumb("x\u{202E}y/z") == "x⟨U+202E⟩y/z")
        #expect(DisplayText.clean("\u{1B}\u{202E}") == "⟨U+001B⟩⟨U+202E⟩")
        #expect(DisplayText.clean("a\tb") == "a b")
        #expect(DisplayText.hasHidden("a\u{2066}b") && !DisplayText.hasHidden("a\tb"))
    }

    @Test func joinersThatShapeWritingStay() {
        // Persian "mi-ravam" is written with a zero-width non-joiner.
        let persian = "\u{0645}\u{06CC}\u{200C}\u{0631}\u{0648}\u{0645}"
        #expect(DisplayText.clean(persian) == persian)
        let family = "👨\u{200D}👩\u{200D}👧\u{200D}👦 notes.md"
        #expect(DisplayText.clean(family) == family)
        let england = "🏴\u{E0067}\u{E0062}\u{E0065}\u{E006E}\u{E0067}\u{E007F}"
        #expect(DisplayText.clean(england) == england)
        #expect(DisplayText.clean("a\u{FE0F}b") == "a\u{FE0F}b")
        #expect(DisplayText.clean("in\u{202E}voice") == "in⟨U+202E⟩voice")
        #expect(DisplayText.clean("a\u{200E}b\u{061C}c") == "a⟨U+200E⟩b⟨U+061C⟩c")
        let rows = HiddenCharacters.reveal(CodeRow.file(TextFile(lines: [persian, family])))
        #expect(rows.first?.isHunk == false && rows.allSatisfy { if case .notice = $0 { false } else { true } })
    }

    @Test func markedLineStaysWithinTheColumnCap() {
        let line = String(repeating: "\u{202E}", count: 2_000)
        let rows = HiddenCharacters.reveal(CodeRow.file(TextFile(lines: [line], clipped: [0])))
        guard case .line(_, _, let shown, _, let tokens, _, let clipped)? = rows.last else { Issue.record("expected a line"); return }
        #expect(shown.utf16.count <= ReviewLimits.maxColumns)
        #expect(clipped)
        #expect(tokens.allSatisfy { $0.range.upperBound <= shown.utf16.count })
        // The cut never lands inside a marker.
        #expect(shown.hasSuffix("⟩"))
        let split = HiddenCharacters.reveal([.split(left: nil, right: CodeRowSide(number: 1, text: line, kind: .added, tokens: [], changes: []))])
        guard case .split(_, let right?)? = split.dropFirst().first else { Issue.record("expected a pair"); return }
        #expect(right.text.utf16.count <= ReviewLimits.maxColumns)
        #expect(split.last == .notice(CodeCopy.clippedNotice))
    }

    @Test func leadingByteOrderMarkIsNotHidden() {
        let rows = HiddenCharacters.reveal(CodeRow.file(TextFile(lines: ["\u{FEFF}let a = 1", "let b = 2"])))
        #expect(rows.count == 2 && rows.allSatisfy { if case .notice = $0 { false } else { true } })
        guard case .line(_, _, let first, _, _, _, _) = rows[0] else { Issue.record("expected a line"); return }
        #expect(first == "\u{FEFF}let a = 1")
        // Anywhere else it is marked and raises the banner.
        let later = HiddenCharacters.reveal(CodeRow.file(TextFile(lines: ["let a = 1", "let\u{FEFF} b"])))
        #expect(later.first == .notice("This file contains hidden characters."))
        let inside = HiddenCharacters.reveal(CodeRow.file(TextFile(lines: ["a\u{FEFF}b"])))
        #expect(inside.first == .notice("This file contains hidden characters."))
    }

    @Test func hiddenCharactersAreMarkedInAFile() async {
        let text = "let a = 1\nlet b\u{200B} = 2\n"
        let model = await viewer("x.ts", FsReadResult(path: "x.ts", size: text.utf8.count, encoding: .utf8, content: text, truncated: false))
        guard case .text(let rows)? = model.content else { Issue.record("expected text"); return }
        #expect(rows.first == .notice("This file contains hidden characters."))
        guard case .line(_, _, let shown, _, _, _, _) = rows[2] else { Issue.record("expected a line"); return }
        #expect(shown == "let b⟨U+200B⟩ = 2")
        // Copy gives the line back as the file has it.
        #expect(rows[2].copyText == "let b\u{200B} = 2")
    }

    @Test func findHandlesALargeFile() async {
        let text = (0..<10_000).map { "line \($0) needle" }.joined(separator: "\n") + "\n"
        let reply = FsReadResult(path: "big.txt", size: text.utf8.count, encoding: .utf8, content: text, truncated: false)
        let model = await viewer("big.txt", reply)
        model.query = "needle"
        await model.settle()
        #expect(model.matches.count == 5_000)
        #expect(model.capped)
        #expect(model.position == "1 of 5,000+")
    }
}
