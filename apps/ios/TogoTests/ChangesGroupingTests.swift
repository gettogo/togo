import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct ChangesGroupingTests {
    @Test func cappedStatusKeepsEveryShownFileAndGroupMember() throws {
        var status = ReviewFixtures.status
        status.files = (0..<500).flatMap { n in [
            ReviewFixtures.file("src/d\(n)/tracked.txt", ".", "M"),
            ReviewFixtures.file("src/d\(n)/new.txt", "?", "?")
        ] }
        status.files += (0..<800).map { ReviewFixtures.file("new/generated\($0).txt", "?", "?") }
        status.files += (0..<200).map { ReviewFixtures.file("loose\($0).txt", "?", "?") }
        var grouped: [ChangeSection] = []
        let grouping = ContinuousClock().measure {
            for _ in 0..<4 { grouped = ChangesModel.sections(for: status) }
        }
        let untracked = try #require(grouped.first { $0.kind == .untracked })
        #expect(untracked.rows.count == 701)
        #expect(untracked.rows.flatMap(\.paths) == status.files.filter { $0.index == "?" }.map(\.path))
        #expect(untracked.rows.first { $0.path == "new/" }?.paths.count == 800)
        var selected: ReviewSelection?
        let selection = try ContinuousClock().measure {
            for _ in 0..<4 { selected = try ReviewSelection.all(in: status) }
        }
        #expect(selected?.paths == status.files.map(\.path))
        #expect(selected?.seen.count == 2000)
        for file in status.files { #expect(selected?.seen[file.path] == GitSeen(file)) }
        let metrics = ["grouping": String(describing: grouping), "selection": String(describing: selection)]
        let url = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("review-grouping-metrics.json")
        try JSONSerialization.data(withJSONObject: metrics, options: .sortedKeys).write(to: url)
    }

    @Test func duplicateSeenPathsStillUseTheFirstDisplayedRecord() throws {
        var status = ReviewFixtures.status
        let first = ReviewFixtures.file("same.txt", ".", "M")
        let second = ReviewFixtures.file("same.txt", "A", ".")
        status.files = [first, second]
        let selection = try ReviewSelection(status, paths: [first.path])
        #expect(selection.seen[first.path] == GitSeen(first))
    }

    @Test func trackedSiblingsBlockOnlyTheirOwnOuterFolder() {
        var status = ReviewFixtures.status
        status.files = [
            ReviewFixtures.file("a/kept", ".", "M"),
            ReviewFixtures.file("a/new one/first", "?", "?"),
            ReviewFixtures.file("ab/first", "?", "?"),
            ReviewFixtures.file("a/new one/second", "?", "?"),
            ReviewFixtures.file("ab/second", "?", "?")
        ]
        let rows = ChangesModel.sections(for: status).first { $0.kind == .untracked }?.rows
        #expect(rows?.map(\.path) == ["a/new one/", "ab/"])
        #expect(rows?.map(\.paths) == [["a/new one/first", "a/new one/second"], ["ab/first", "ab/second"]])
    }
}
