import Foundation
import Testing
@testable import TogoPush

struct SharedBlobFailureTests {
    private func scratch() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appending(path: "togo-blob-failure-\(UUID())")
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    @Test func corruptOrNewerDataIsNeverReplacedByAnEmptyDerivedWrite() throws {
        let directory = try scratch()
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = directory.appending(path: "records.json")
        let blob = SharedBlob<Int>(directory: directory, name: "records")
        for original in [Data("{broken".utf8), Data(#"[{"future":"record"}]"#.utf8)] {
            try original.write(to: file)
            var changed = false
            blob.update { values in changed = true; values.append(9) }
            #expect(!changed)
            #expect(try Data(contentsOf: file) == original)
        }
    }

    @Test func missingDataCanStillBeCreated() throws {
        let directory = try scratch()
        defer { try? FileManager.default.removeItem(at: directory) }
        let blob = SharedBlob<Int>(directory: directory, name: "records")
        blob.update { $0.append(7) }
        #expect(blob.read() == [7])
    }

    @Test func unreadableDataDoesNotRunTheMutation() throws {
        let directory = try scratch()
        defer { try? FileManager.default.removeItem(at: directory) }
        try FileManager.default.createDirectory(at: directory.appending(path: "records.json"), withIntermediateDirectories: false)
        let blob = SharedBlob<Int>(directory: directory, name: "records")
        var changed = false
        blob.update { _ in changed = true }
        #expect(!changed)
    }

    @Test func unavailableLockNeverRunsAnUnlockedMutation() throws {
        let directory = try scratch()
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = directory.appending(path: "records.json")
        let original = Data("[7]".utf8)
        try original.write(to: file)
        try FileManager.default.createDirectory(at: directory.appending(path: "records.lock"), withIntermediateDirectories: false)
        let blob = SharedBlob<Int>(directory: directory, name: "records")
        var changed = false
        blob.update { values in changed = true; values.append(9) }
        #expect(!changed)
        #expect(try Data(contentsOf: file) == original)
    }
}
