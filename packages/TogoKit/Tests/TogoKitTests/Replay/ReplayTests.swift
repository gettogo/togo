import Foundation
import Testing
@testable import TogoKit

/// Replays every recorded and hand-written stream in protocol/v1/replays through the app's
/// state and checks the invariants, then checks that duplicated, swapped and interrupted
/// delivery end in the same state. Product bugs a fixture exposes are listed in
/// `ReplayKnownIssues`; their tests pass while the bug reproduces and fail once it is fixed,
/// so the entry gets removed.
@Suite("Replay")
struct ReplayTests {
    static let fixtures = ReplayFixture.all

    @Test func fixturesExist() {
        #expect(Self.fixtures.count >= 10, "record with scripts/record-replays.sh")
    }

    @Test("Invariants", arguments: fixtures)
    func invariants(_ name: String) throws {
        let replay = try Replay.run(name)
        #expect(!replay.fixture.records.isEmpty)
        report(ReplayInvariants.check(replay), fixture: name, check: "invariants")
    }

    @Test("Duplicate delivery changes nothing", arguments: fixtures)
    func duplicated(_ name: String) throws {
        let fixture = try ReplayFixture.load(name)
        compare(Replay(fixture: fixture), Replay(fixture: fixture, .duplicate), fixture: name, check: "duplicate")
    }

    @Test("Swapped frames end in the same state", arguments: fixtures)
    func swapped(_ name: String) throws {
        let fixture = try ReplayFixture.load(name)
        let straight = Replay(fixture: fixture)
        let base = Self.rules(straight)
        var problems: [ReplayViolation] = []
        let live = straight.liveIndices
        for (a, b) in zip(live, live.dropFirst()) where b == a + 1 {
            let swapped = Replay(fixture: fixture, .swap(at: a))
            let diff = ReplaySignature(swapped.final).differences(from: ReplaySignature(straight.final))
            if !diff.isEmpty {
                problems.append(.init(rule: "swap-\(diff.joined(separator: "+"))", detail: "swap at \(a): \(diff)"))
            }
            problems += Self.new(ReplayInvariants.check(swapped), beyond: base)
                .map { .init(rule: "swap-" + $0.rule, detail: "swap at \(a): \($0.detail)") }
        }
        report(Self.firstPerRule(problems), fixture: name, check: "swap")
    }

    @Test("Reconnect mid-stream with resync ends in the same state", arguments: fixtures)
    func reconnected(_ name: String) throws {
        let fixture = try ReplayFixture.load(name)
        let straight = Replay(fixture: fixture)
        let base = Self.rules(straight)
        let live = straight.liveIndices
        var problems: [ReplayViolation] = []
        // Drop the link at a quarter, half and three quarters of the stream, losing a few frames.
        for fraction in [0.25, 0.5, 0.75] where live.count > 4 {
            let at = live[Int(Double(live.count) * fraction)]
            let reconnected = Replay(fixture: fixture, .reconnect(at: at, missing: 3))
            let diff = ReplaySignature(reconnected.final).differences(from: ReplaySignature(straight.final))
            if !diff.isEmpty {
                problems.append(.init(rule: "reconnect-\(diff.joined(separator: "+"))",
                                      detail: "reconnect after \(at): \(diff)\n  straight: \(ReplaySignature(straight.final))\n  reconnected: \(ReplaySignature(reconnected.final))"))
            }
            problems += Self.new(ReplayInvariants.check(reconnected), beyond: base)
                .map { .init(rule: "reconnect-" + $0.rule, detail: "reconnect after \(at): \($0.detail)") }
        }
        report(Self.firstPerRule(problems), fixture: name, check: "reconnect")
    }

    // MARK: Reporting

    private func compare(_ a: Replay, _ b: Replay, fixture: String, check: String) {
        let diff = ReplaySignature(b.final).differences(from: ReplaySignature(a.final))
        var problems = diff.isEmpty ? [] : [ReplayViolation(rule: check, detail: "\(b.perturbation): \(diff)")]
        problems += Self.new(ReplayInvariants.check(b), beyond: Self.rules(a))
            .map { .init(rule: "\(check)-\($0.rule)", detail: $0.detail) }
        report(problems, fixture: fixture, check: check)
    }

    /// The rules the unperturbed replay already breaks: reported once, by "Invariants".
    private static func rules(_ replay: Replay) -> Set<String> {
        Set(ReplayInvariants.check(replay).map(\.rule))
    }

    /// Violations a perturbation adds. Rules about the end of a settled run are left out:
    /// a perturbed run's end is compared with the straight one's instead.
    private static func new(_ violations: [ReplayViolation], beyond base: Set<String>) -> [ReplayViolation] {
        violations.filter { !base.contains($0.rule) && !ReplayInvariants.endRules.contains($0.rule) }
    }

    private static func firstPerRule(_ violations: [ReplayViolation]) -> [ReplayViolation] {
        var seen = Set<String>()
        return violations.filter { seen.insert($0.rule).inserted }
    }

    /// Records each violation as a failure; product bugs already known for this fixture are
    /// expected (and must still reproduce).
    private func report(_ violations: [ReplayViolation], fixture: String, check: String) {
        ReplayKnownIssues.run(fixture, check) {
            for v in violations { Issue.record("\(fixture): \(v)") }
        }
    }
}
