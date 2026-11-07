import Foundation
import Testing
@testable import TogoKit

struct SessionActivityHistoryTests {
    private static let base = """
        "id":"s_abcdefghij","bib":"01","name":"Claude Code","agent":"claude","cwd":"/home/u",\
        "createdAt":"2026-11-03T12:00:00Z","status":"working","activity":null,"attachedClients":0,\
        "managed":true,"muted":false,"meta":{"model":null,"contextPercent":null,"costUsd":null}
        """

    private func decode(_ extra: String) throws -> Session {
        try JSONDecoder().decode(Session.self, from: Data("{\(Self.base)\(extra)}".utf8))
    }

    @Test func absentFieldIsEmpty() throws {
        #expect(try decode("").activityHistory.isEmpty)
    }

    @Test func decodesValuesOldestFirst() throws {
        let session = try decode(#","activityHistory":[0,0.25,1]"#)
        #expect(session.activityHistory == [0, 0.25, 1])
    }

    @Test func clampsValuesToUnitRange() throws {
        let session = try decode(#","activityHistory":[-1,0.5,3]"#)
        #expect(session.activityHistory == [0, 0.5, 1])
    }

    @Test func keepsNewestSixteen() throws {
        let values = (0..<20).map { Double($0) / 20 }
        let list = values.map { String($0) }.joined(separator: ",")
        let session = try decode(#","activityHistory":[\#(list)]"#)
        #expect(session.activityHistory == Array(values.suffix(16)))
    }

    @Test func initAndAssignmentNormalise() {
        var session = Session(
            id: "s_abcdefghij", bib: nil, name: "n", agent: "shell", cwd: "/", createdAt: Date(timeIntervalSince1970: 0),
            status: .idle, activity: nil, attachedClients: 0, managed: true, activityHistory: [2, -2])
        #expect(session.activityHistory == [1, 0])
        session.activityHistory = Array(repeating: 0.5, count: 40)
        #expect(session.activityHistory.count == 16)
    }

    @Test func roundTripOmitsEmptyField() throws {
        let empty = try decode("")
        let data = try JSONEncoder().encode(empty)
        #expect(!String(decoding: data, as: UTF8.self).contains("activityHistory"))
        let full = try decode(#","activityHistory":[0.5]"#)
        let again = try JSONDecoder().decode(Session.self, from: JSONEncoder().encode(full))
        #expect(again.activityHistory == [0.5])
    }
}
