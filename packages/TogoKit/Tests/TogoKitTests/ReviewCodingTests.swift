import Foundation
import Testing
import TogoKit

struct ReviewCodingTests {
    @Test func stageParamsEncodePaths() throws {
        let params = GitPathsParams(id: "s_abcdefghij", paths: ["-rf", "a b.txt", "naïve.txt"])
        #expect(try json(params) == .object([
            "id": .string("s_abcdefghij"),
            "paths": .array([.string("-rf"), .string("a b.txt"), .string("naïve.txt")]),
        ]))
    }

    @Test func stageParamsFromFilesCarryWhatWasSeen() throws {
        let files = [
            GitStatus.File(path: "App.swift", origPath: nil, index: ".", worktree: "M", fingerprint: "blob:ab12"),
            GitStatus.File(path: "newpkg/a.txt", origPath: nil, index: "?", worktree: "?", fingerprint: "blob:cd34"),
            GitStatus.File(path: "Old.swift", origPath: nil, index: ".", worktree: "D"),
        ]
        let params = GitPathsParams(id: "s_abcdefghij", files: files)
        #expect(params.paths == ["App.swift", "newpkg/a.txt", "Old.swift"])
        #expect(params.seen == [
            "App.swift": GitSeen(status: ".M", fingerprint: "blob:ab12"),
            "newpkg/a.txt": GitSeen(status: "??", fingerprint: "blob:cd34"),
            "Old.swift": GitSeen(status: ".D"),
        ])
        guard case .object(let fields) = try json(params), case .object(let seen)? = fields["seen"] else {
            Issue.record("seen did not encode as an object")
            return
        }
        #expect(seen["App.swift"] == .object(["status": .string(".M"), "fingerprint": .string("blob:ab12")]))
        #expect(seen["Old.swift"] == .object(["status": .string(".D")]))
    }

    @Test func statusTruncationIsReadAndAbsentMeansComplete() throws {
        let cut = #"{"repo":true,"root":"/p","branch":"main","ahead":0,"behind":0,"truncated":true,"files":[]}"#
        let truncated = try JSONDecoder().decode(GitStatus.self, from: Data(cut.utf8))
        #expect(truncated.isTruncated)
        #expect(try json(truncated) == JSONDecoder().decode(JSONValue.self, from: Data(cut.utf8)))

        let old = #"{"repo":true,"root":"/p","branch":"main","ahead":0,"behind":0,"files":[]}"#
        let complete = try JSONDecoder().decode(GitStatus.self, from: Data(old.utf8))
        #expect(!complete.isTruncated)
        #expect(try json(complete) == JSONDecoder().decode(JSONValue.self, from: Data(old.utf8)))
    }

    @Test func repeatEditChangesSeenButNotLetters() {
        let before = GitStatus.File(path: "a.txt", origPath: nil, index: ".", worktree: "M", fingerprint: "blob:1111")
        let after = GitStatus.File(path: "a.txt", origPath: nil, index: ".", worktree: "M", fingerprint: "blob:2222")
        #expect(before.letters == after.letters)
        #expect(GitPathsParams(id: "s_abcdefghij", files: [before]) != GitPathsParams(id: "s_abcdefghij", files: [after]))
    }

    @Test func statusFingerprintDecodesAndStaysOptional() throws {
        let withFingerprint = #"{"path":"a","origPath":null,"index":".","worktree":"M","fingerprint":"stat:9000000:5"}"#
        let file = try JSONDecoder().decode(GitStatus.File.self, from: Data(withFingerprint.utf8))
        #expect(file.fingerprint == "stat:9000000:5")
        #expect(try json(file) == JSONDecoder().decode(JSONValue.self, from: Data(withFingerprint.utf8)))
        let deleted = #"{"path":"a","origPath":null,"index":".","worktree":"D"}"#
        let gone = try JSONDecoder().decode(GitStatus.File.self, from: Data(deleted.utf8))
        #expect(gone.fingerprint == nil)
        #expect(try json(gone) == JSONDecoder().decode(JSONValue.self, from: Data(deleted.utf8)))
    }

    @Test func commitParamsEncode() throws {
        #expect(try json(GitCommitParams(id: "s_abcdefghij", message: "Fix it\n\nBody", amend: false)) == .object([
            "id": .string("s_abcdefghij"), "message": .string("Fix it\n\nBody"), "amend": .bool(false),
        ]))
        let head = String(repeating: "a", count: 40)
        let amend = GitCommitParams(id: "s_abcdefghij", message: "Fix it", amend: true, head: head)
        guard case .object(let fields) = try json(amend) else {
            Issue.record("commit params did not encode as an object")
            return
        }
        #expect(fields["head"] == .string(head))
        #expect(fields["amend"] == .bool(true))
    }

    @Test func commitResultDecodes() throws {
        let hash = "0123456789abcdef0123456789abcdef01234567"
        let result = try JSONDecoder().decode(
            GitCommitResult.self, from: Data(#"{"hash":"\#(hash)","short":"0123456"}"#.utf8)
        )
        #expect(result == GitCommitResult(hash: hash, short: "0123456"))
    }

    @Test func diffCommitParamOmittedWhenNil() throws {
        #expect(try json(GitDiffParams(id: "s_abcdefghij", path: "a.txt")) == .object([
            "id": .string("s_abcdefghij"), "path": .string("a.txt"),
        ]))
        #expect(try json(GitDiffParams(id: "s_abcdefghij", commit: "HEAD~1")) == .object([
            "id": .string("s_abcdefghij"), "commit": .string("HEAD~1"),
        ]))
    }

    @Test func listDirWithRefEncodesIdAndRef() throws {
        #expect(try json(ListDirParams(path: "Sources", showHidden: true, id: "s_abcdefghij", ref: "HEAD")) == .object([
            "path": .string("Sources"), "showHidden": .bool(true), "id": .string("s_abcdefghij"), "ref": .string("HEAD"),
        ]))
        #expect(try json(ListDirParams(path: "~")) == .object(["path": .string("~")]))
    }

    @Test func rootFormEncodedOnlyWhenAsked() throws {
        #expect(try json(FsReadParams(id: "s_abcdefghij", path: "apps/web/a.ts", root: true)) == .object([
            "id": .string("s_abcdefghij"), "path": .string("apps/web/a.ts"), "root": .bool(true),
        ]))
        #expect(try json(FsReadParams(id: "s_abcdefghij", path: "a.ts")) == .object([
            "id": .string("s_abcdefghij"), "path": .string("a.ts"),
        ]))
        #expect(try json(ListDirParams(path: "apps/tools", id: "s_abcdefghij", root: true)) == .object([
            "path": .string("apps/tools"), "id": .string("s_abcdefghij"), "root": .bool(true),
        ]))
    }

    @Test func listDirWithFilesEncodesAndDecodes() throws {
        #expect(try json(ListDirParams(path: "/p", includeFiles: true)) == .object([
            "path": .string("/p"), "includeFiles": .bool(true),
        ]))
        let result = try JSONDecoder().decode(ListDirResult.self, from: Data(#"""
            {"path":"/p","entries":[{"name":"src","dir":true,"kind":"dir"},{"name":"a.txt","dir":false,"kind":"file","size":5},\#
            {"name":"link","dir":false,"kind":"symlink"},{"name":"x","dir":false,"kind":"socket"}]}
            """#.utf8))
        #expect(result.entries == [
            DirEntry(name: "src", dir: true, kind: .dir),
            DirEntry(name: "a.txt", dir: false, kind: .file, size: 5),
            DirEntry(name: "link", dir: false, kind: .symlink),
            DirEntry(name: "x", dir: false, kind: .unknown("socket")),
        ])
        // A helper listing folders only sends neither kind nor size.
        let folders = try JSONDecoder().decode(DirEntry.self, from: Data(#"{"name":"src","dir":true}"#.utf8))
        #expect(folders == DirEntry(name: "src", dir: true))
    }

    @Test func conflictReasonsReadAsGitConflicts() {
        #expect(HelperError.conflict("stale").gitConflict == .stale)
        #expect(HelperError.conflict("nothing_staged").gitConflict == .nothingStaged)
        #expect(HelperError.conflict("unborn_amend").gitConflict == .unbornAmend)
        #expect(HelperError.conflict("partial").gitConflict == .partial)
        #expect(HelperError.conflict("raced").gitConflict == .raced)
        #expect(HelperError.closed.gitConflict == nil)
    }

    @Test func incompleteWritesSayWhatHappened() {
        let partial = HelperError.partialWrite(changed: ["App.swift", "README.md", "Package.swift", "Sources/main.swift"])
        #expect(partial.localizedDescription == "Some changes were made: App.swift, README.md, Package.swift and 1 more. Refresh to see where things stand.")
        #expect(HelperError.partialWrite(changed: ["App.swift"]).localizedDescription == "Some changes were made: App.swift. Refresh to see where things stand.")
        let raced = HelperError.racedCommit(hash: "abcdef0" + String(repeating: "1", count: 33))
        #expect(raced.localizedDescription == "Someone else committed at the same time. Your commit is abcdef0.")
    }

    @Test func writeRefusalsReadPlainly() {
        let unmanaged = HelperError.rpc(RPCErrorObject(code: RPCErrorCode.notManaged, message: "not_managed"))
        #expect(unmanaged.localizedDescription == "That session was not started from Togo, so it cannot be changed from here.")
        let invalid = HelperError.rpc(RPCErrorObject(code: RPCErrorCode.invalidParams, message: "invalid params"))
        #expect(invalid.localizedDescription == "The Togo helper on the machine turned that request down. Update the app or the helper.")
        #expect(HelperError.conflict("stale").localizedDescription == "This file changed since you looked. Check it again.")
    }

    @Test func unknownConflictReasonIsNil() {
        #expect(HelperError.conflict("answered").gitConflict == nil)
        #expect(HelperError.conflict("someday").gitConflict == nil)
    }

    @Test func gitConflictsReadAsPlainSentences() {
        for reason in ["stale", "locked", "filtered", "unmerged", "nothing_staged", "identity", "unborn_amend", "partial", "raced", "case_rename"] {
            let text = HelperError.conflict(reason).localizedDescription
            #expect(!text.contains("_") && text.hasSuffix("."), "\(reason): \(text)")
            #expect(text != HelperError.conflict("answered").localizedDescription, "\(reason)")
        }
    }

    private func json(_ value: some Encodable) throws -> JSONValue {
        try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(value))
    }
}

struct WriteValidationTests {
    @Test func writePathRules() {
        #expect(GitPathsParams.isValidPath("dir/naïve file.txt"))
        #expect(GitPathsParams.isValidPath(String(repeating: "a", count: 4096)))
        #expect(!GitPathsParams.isValidPath(String(repeating: "a", count: 4097)))
        #expect(!GitPathsParams.isValidPath(String(repeating: "é", count: 2048) + "a"))
        #expect(!GitPathsParams.isValidPath("a\u{0}b"))
        #expect(!GitPathsParams.isValidPath("newpkg/"))
        #expect(!GitPathsParams.isValidPath(""))
    }

    @Test func commitHashRules() {
        #expect(GitCommitParams.isValidHash(String(repeating: "a", count: 40)))
        #expect(GitCommitParams.isValidHash(String(repeating: "0", count: 64)))
        #expect(!GitCommitParams.isValidHash(String(repeating: "a", count: 41)))
        #expect(!GitCommitParams.isValidHash(String(repeating: "A", count: 40)))
        #expect(!GitCommitParams(id: "s_abcdefghij", message: "m", amend: true).isValid)
        #expect(GitCommitParams(id: "s_abcdefghij", message: "m", amend: false).isValid)
        #expect(GitCommitParams(id: "s_abcdefghij", message: "m", amend: true, head: String(repeating: "f", count: 40)).isValid)
    }
}
