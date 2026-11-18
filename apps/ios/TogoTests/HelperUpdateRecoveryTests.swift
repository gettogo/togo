import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct HelperUpdateRecoveryTests {
    private actor Script {
        enum Reply: Sendable { case lost, notStarted, output(String), wait, waitForHomebrew }
        var replies: [Reply]
        var commands: [String] = []
        var waiting: CheckedContinuation<Void, Never>?
        init(_ replies: [Reply]) { self.replies = replies }
        func run(_ command: String) async throws -> HelperUpdates.Output {
            commands.append(command)
            guard !replies.isEmpty else { throw HelperError.closed }
            switch replies.removeFirst() {
            case .lost: throw HelperUpdateCommandInterrupted(HelperError.closed)
            case .notStarted: throw LiveServiceError.unknownMachine
            case .output(let text): return (Data(text.utf8), Data(), 0)
            case .wait:
                await withCheckedContinuation { waiting = $0 }
                return (Data("Installed togo v1.1.0 at /tmp/togo\n".utf8), Data(), 0)
            case .waitForHomebrew:
                await withCheckedContinuation { waiting = $0 }
                return (Data("togo: Installed with Homebrew: run brew upgrade togo\n".utf8), Data(), 0)
            }
        }
        func release() { waiting?.resume(); waiting = nil }
        var isWaiting: Bool { waiting != nil }
    }

    private let current = "togo v1.1.0 is up to date\n"
    private let available = "togo v1.1.0 is available (installed: v1.0.2)\n"

    private func make(_ script: Script, host: HostID, suite: String) throws -> HelperUpdates {
        let defaults = UserDefaults(suiteName: suite)!
        var ledger = HelperUpdateLedger()
        ledger.record(.available(latest: "v1.1.0", installed: "v1.0.2"), for: host, at: .now)
        defaults.set(try JSONEncoder().encode(ledger), forKey: "helperUpdates.v1")
        return HelperUpdates(defaults: defaults, run: { _, command, _ in try await script.run(command) })
    }

    @Test func aLostUpdateReplyChecksTheInstalledVersionWithoutUpdatingAgain() async throws {
        let suite = "HelperUpdateRecoveryTests.\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let host = UUID()
        let script = Script([.lost, .output(current)])
        let updates = try make(script, host: host, suite: suite)
        await updates.update(host)
        #expect(updates.phase(host) == .updated("v1.1.0"))
        #expect(updates.available(host) == nil)
        #expect(await script.commands == [HelperUpdate.updateCommand(helperPath: nil), HelperUpdate.checkCommand(helperPath: nil)])
    }

    @Test func aReconnectChecksAnUncertainUpdateEvenInsideTheDailyWindow() async throws {
        let suite = "HelperUpdateRecoveryTests.\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let host = UUID()
        let script = Script([.lost, .lost, .output(current)])
        let updates = try make(script, host: host, suite: suite)
        await updates.update(host)
        await updates.checkIfDue(host)
        #expect(updates.phase(host) == .updated("v1.1.0"))
        #expect(await script.commands == [HelperUpdate.updateCommand(helperPath: nil), HelperUpdate.checkCommand(helperPath: nil), HelperUpdate.checkCommand(helperPath: nil)])
    }

    @Test func aCheckThatStillFindsTheOldVersionOffersAnExplicitUpdate() async throws {
        let suite = "HelperUpdateRecoveryTests.\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let host = UUID()
        let script = Script([.lost, .output(available)])
        let updates = try make(script, host: host, suite: suite)
        await updates.update(host)
        #expect(updates.phase(host) == .idle)
        #expect(updates.available(host) == "v1.1.0")
        #expect(await script.commands.count == 2)
    }

    @Test func retryingAnUncertainUpdateOnlyChecksItsVersion() async throws {
        let suite = "HelperUpdateRecoveryTests.\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let host = UUID()
        let script = Script([.lost, .lost, .output(current)])
        let updates = try make(script, host: host, suite: suite)
        await updates.update(host)
        #expect(updates.requiresCheck(host))
        updates.settle(host)
        #expect(updates.requiresCheck(host))
        await updates.update(host)
        #expect(updates.phase(host) == .updated("v1.1.0"))
        #expect(await script.commands == [HelperUpdate.updateCommand(helperPath: nil), HelperUpdate.checkCommand(helperPath: nil), HelperUpdate.checkCommand(helperPath: nil)])
    }

    @Test func aHomebrewRecoveryChecksWithoutUpgradingAgain() async throws {
        let suite = "HelperUpdateRecoveryTests.\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let host = UUID()
        let script = Script([.lost, .output("togo: Installed with Homebrew: run brew upgrade togo\n"),
                             .output("{\"formulae\":[]}")])
        let updates = try make(script, host: host, suite: suite)
        await updates.update(host)
        #expect(updates.phase(host) == .idle)
        #expect(updates.available(host) == nil)
        #expect(await script.commands == [HelperUpdate.updateCommand(helperPath: nil), HelperUpdate.checkCommand(helperPath: nil), HelperUpdate.brewCheckCommand(helperPath: nil)])
    }

    @Test func aConfirmedUpdateDoesNotNeedAnotherCheck() async throws {
        let suite = "HelperUpdateRecoveryTests.\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let host = UUID()
        let script = Script([.output("Installed togo v1.1.0 at /tmp/togo\n")])
        let updates = try make(script, host: host, suite: suite)
        await updates.update(host)
        #expect(updates.phase(host) == .updated("v1.1.0"))
        #expect(await script.commands.count == 1)
    }

    @Test func aFailureBeforeExecDoesNotRetryTheConnection() async throws {
        let suite = "HelperUpdateRecoveryTests.\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let host = UUID()
        let script = Script([.notStarted])
        let updates = try make(script, host: host, suite: suite)
        await updates.update(host)
        #expect(updates.phase(host) == .failed(ServiceErrors.message(LiveServiceError.unknownMachine)))
        #expect(await script.commands.count == 1)
    }

    @Test func anUnchangedVersionDoesNotClaimTheHelperWasUpdated() async throws {
        let suite = "HelperUpdateRecoveryTests.\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let host = UUID()
        let script = Script([.lost, .output("togo v1.0.2 is up to date\n")])
        let updates = try make(script, host: host, suite: suite)
        await updates.update(host)
        #expect(updates.phase(host) == .idle)
        #expect(updates.state(host)?.installed == "v1.0.2")
        #expect(await script.commands.count == 2)
    }

    @Test func forgettingAMachineRejectsTheLateUpdateResult() async throws {
        let suite = "HelperUpdateRecoveryTests.\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let host = UUID()
        let script = Script([.wait])
        let updates = try make(script, host: host, suite: suite)
        let task = Task { await updates.update(host) }
        let deadline = ContinuousClock.now + .seconds(3)
        while !(await script.isWaiting), ContinuousClock.now < deadline { await Task.yield() }
        #expect(await script.isWaiting)
        #expect(updates.isChecking(host))
        await updates.checkIfDue(host)
        await updates.update(host)
        #expect(await script.commands.count == 1)
        updates.forget(host)
        await script.release()
        await task.value
        #expect(updates.state(host) == nil)
        #expect(updates.phase(host) == .idle)
    }

    @Test func forgettingDuringTheCheckDoesNotStartAnotherCommand() async throws {
        let suite = "HelperUpdateRecoveryTests.\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let host = UUID()
        let script = Script([.lost, .waitForHomebrew, .output("{\"formulae\":[]}")])
        let updates = try make(script, host: host, suite: suite)
        let task = Task { await updates.update(host) }
        let deadline = ContinuousClock.now + .seconds(3)
        while !(await script.isWaiting), ContinuousClock.now < deadline { await Task.yield() }
        #expect(await script.isWaiting)
        updates.forget(host)
        await script.release()
        await task.value
        #expect(await script.commands.count == 2)
        #expect(updates.state(host) == nil && updates.phase(host) == .idle)
    }
}
