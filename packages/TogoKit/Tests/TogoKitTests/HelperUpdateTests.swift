import Foundation
import Testing
import TogoKit

@Suite struct HelperUpdateTests {
    @Test func readsAvailable() {
        let out = "togo v1.0.1 is available (installed: v1.0.0)\n"
        #expect(HelperUpdate.parseCheck(stdout: out, stderr: "", status: 0) == .available(latest: "v1.0.1", installed: "v1.0.0"))
    }

    @Test func readsUpToDate() {
        #expect(HelperUpdate.parseCheck(stdout: "togo v1.0.1 is up to date\n", stderr: "", status: 0) == .upToDate(installed: "v1.0.1"))
    }

    @Test func ignoresBannersAroundTheLine() {
        let out = "Last login: today\ntogo v2.0.0 is available (installed: v1.9.0)\n"
        #expect(HelperUpdate.parseCheck(stdout: out, stderr: "", status: 0) == .available(latest: "v2.0.0", installed: "v1.9.0"))
    }

    @Test(arguments: [
        ("", "togo: unknown flag --check", Int32(2)),
        ("something else\n", "", Int32(0)),
        ("togo v1.0.1 is available (installed: v1.0.0)\n", "", Int32(1)),
        ("togo vX is available (installed: v1)\n", "", Int32(0)),
    ])
    func unrecognisedShowsNothing(stdout: String, stderr: String, status: Int32) {
        #expect(HelperUpdate.parseCheck(stdout: stdout, stderr: stderr, status: status) == nil)
    }

    @Test func homebrewRefusal() {
        let err = "togo: Installed with Homebrew: run brew upgrade togo\n"
        #expect(HelperUpdate.parseCheck(stdout: "", stderr: err, status: 1) == .homebrew)
        #expect(HelperUpdate.parseUpdate(stdout: "", stderr: err, status: 1) == .homebrew)
    }

    @Test func updateOutcomes() {
        let ok = "Installed togo v1.0.1 at /Users/a/.togo/bin/togo (previous binary kept at /Users/a/.togo/bin/togo.previous)\nrestarted\n"
        #expect(HelperUpdate.parseUpdate(stdout: ok, stderr: "", status: 0) == .updated(version: "v1.0.1"))
        #expect(HelperUpdate.parseUpdate(stdout: "togo v1.0.1 is up to date\n", stderr: "", status: 0) == .upToDate)
        #expect(
            HelperUpdate.parseUpdate(stdout: "", stderr: "togo: download failed: no route\n", status: 1)
                == .failed("download failed: no route")
        )
        #expect(HelperUpdate.parseUpdate(stdout: "", stderr: "", status: 3) == .failed("The update failed (exit 3)."))
    }

    @Test func dueOncePerDay() {
        let now = Date(timeIntervalSince1970: 1_000_000)
        #expect(HelperUpdate.isDue(lastChecked: nil, now: now))
        #expect(!HelperUpdate.isDue(lastChecked: now.addingTimeInterval(-3600), now: now))
        #expect(!HelperUpdate.isDue(lastChecked: now.addingTimeInterval(-86_399), now: now))
        #expect(HelperUpdate.isDue(lastChecked: now.addingTimeInterval(-86_400), now: now))
        #expect(HelperUpdate.isDue(lastChecked: now.addingTimeInterval(3600), now: now))
    }

    @Test func comparesVersions() {
        #expect(HelperUpdate.isNewer("v1.0.1", than: "1.0.0"))
        #expect(HelperUpdate.isNewer("v1.10.0", than: "v1.9.9"))
        #expect(!HelperUpdate.isNewer("v1.0.0", than: "v1.0.0"))
        #expect(!HelperUpdate.isNewer("v1.0.0", than: "v1.0.1"))
        #expect(!HelperUpdate.isNewer("dev", than: "v1.0.0"))
        #expect(HelperUpdate.display("v1.0.1") == "1.0.1")
    }

    @Test func homebrewPaths() {
        #expect(HelperUpdate.isHomebrewPath("/opt/homebrew/Cellar/togo/1.0.0/bin/togo"))
        #expect(HelperUpdate.isHomebrewPath("/opt/homebrew/bin/togo"))
        #expect(HelperUpdate.isHomebrewPath("/home/linuxbrew/.linuxbrew/bin/togo"))
        #expect(!HelperUpdate.isHomebrewPath("/Users/a/.togo/bin/togo"))
        #expect(!HelperUpdate.isHomebrewPath("/usr/local/bin/togo"))
        #expect(!HelperUpdate.isHomebrewPath(nil))
    }

    @Test func quotesTheSavedPath() {
        #expect(
            HelperUpdate.checkCommand(helperPath: "/Users/a b/.togo/bin/togo")
                == shellQuote(["/Users/a b/.togo/bin/togo", "update", "--check"])
        )
        let hostile = HelperUpdate.updateCommand(helperPath: "/tmp/x; rm -rf ~")
        #expect(hostile == shellQuote(["/tmp/x; rm -rf ~", "update"]))
        #expect(!hostile.hasPrefix("/tmp/x;"))
    }

    @Test func fallsBackLikeServeStdio() {
        let command = HelperUpdate.checkCommand(helperPath: nil)
        #expect(command.hasPrefix("/bin/sh -c "))
        #expect(command.contains("/opt/homebrew/bin/togo"))
        #expect(command.contains("exec \"$t\" update --check"))
        #expect(!command.contains("\n"))
    }

    @Test func ledgerKeepsKnowledgeWhenACheckFails() {
        let id = UUID(), t0 = Date(timeIntervalSince1970: 0), t1 = t0.addingTimeInterval(90_000)
        var ledger = HelperUpdateLedger()
        #expect(ledger.isDue(id, now: t0))
        ledger.record(.available(latest: "v1.0.1", installed: "v1.0.0"), for: id, at: t0)
        #expect(!ledger.isDue(id, now: t0.addingTimeInterval(60)))
        #expect(ledger.available(for: id, current: "1.0.0") == "v1.0.1")
        ledger.record(nil, for: id, at: t1)
        #expect(ledger.machines[id]?.checkedAt == t1)
        #expect(ledger.available(for: id, current: "1.0.0") == "v1.0.1")
    }

    @Test func availableClearsOnceTheHelperIsCurrent() {
        let id = UUID(), now = Date(timeIntervalSince1970: 0)
        var ledger = HelperUpdateLedger()
        ledger.record(.available(latest: "v1.0.1", installed: "v1.0.0"), for: id, at: now)
        #expect(ledger.available(for: id, current: "1.0.1") == nil)
        ledger.updated(id, to: "v1.0.1", at: now)
        #expect(ledger.available(for: id, current: nil) == nil)
        #expect(ledger.machines[id]?.installed == "v1.0.1")
    }

    @Test func homebrewIsRemembered() {
        let id = UUID(), now = Date(timeIntervalSince1970: 0)
        var ledger = HelperUpdateLedger()
        ledger.record(.homebrew, for: id, at: now)
        #expect(ledger.machines[id]?.homebrew == true)
        ledger.record(.upToDate(installed: "v1.0.0"), for: id, at: now)
        #expect(ledger.machines[id]?.homebrew == false)
    }
}
