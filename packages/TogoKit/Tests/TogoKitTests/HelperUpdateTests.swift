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

    @Test func brewPathComesFromTheCellar() {
        #expect(HelperUpdate.brewPath(helperPath: "/opt/homebrew/Cellar/togo/1.0.1/bin/togo") == "/opt/homebrew/bin/brew")
        #expect(HelperUpdate.brewPath(helperPath: "/home/linuxbrew/.linuxbrew/Cellar/togo/1.0.1/bin/togo") == "/home/linuxbrew/.linuxbrew/bin/brew")
        #expect(HelperUpdate.brewPath(helperPath: "/opt/homebrew/bin/togo") == nil)
        #expect(HelperUpdate.brewPath(helperPath: "/Cellar/togo/1.0.1/bin/togo") == nil)
        #expect(HelperUpdate.brewPath(helperPath: nil) == nil)
    }

    @Test func brewCommandsUseTheCellarPrefixOrTheDefaults() {
        let known = HelperUpdate.brewCheckCommand(helperPath: "/opt/homebrew/Cellar/togo/1.0.1/bin/togo")
        #expect(known.hasPrefix("/bin/sh -c "))
        #expect(known.contains("/opt/homebrew/bin/brew"))
        #expect(!known.contains("/usr/local/bin/brew"))
        #expect(known.contains("NONINTERACTIVE=1 HOMEBREW_NO_ENV_HINTS=1"))
        #expect(known.contains("update --quiet"))
        #expect(known.contains("outdated --json=v2 togo"))
        let unknown = HelperUpdate.brewCheckCommand(helperPath: "/Users/a/.togo/bin/togo")
        #expect(unknown.contains("/opt/homebrew/bin/brew") && unknown.contains("/usr/local/bin/brew"))
        let upgrade = HelperUpdate.brewUpgradeCommand(helperPath: nil)
        #expect(upgrade.contains("HOMEBREW_NO_INSTALL_CLEANUP=1"))
        #expect(upgrade.contains("upgrade togo"))
        #expect(upgrade.contains("daemon restart --when-idle"))
        #expect(!upgrade.contains("\n"))
    }

    @Test func brewCommandsQuoteTheHelperPath() {
        let hostile = HelperUpdate.brewUpgradeCommand(helperPath: "/tmp/x'; rm -rf ~; '/Cellar/togo/1/bin/togo")
        #expect(!hostile.contains("x'; rm"))
    }

    @Test func readsBrewOutdated() {
        let json = #"{"formulae":[{"name":"togo","installed_versions":["1.0.1"],"current_version":"1.0.2","pinned":false}],"casks":[]}"#
        #expect(HelperUpdate.parseBrewOutdated(stdout: json) == .available(latest: "v1.0.2", installed: "v1.0.1"))
        #expect(HelperUpdate.parseBrewOutdated(stdout: "Warning: x\n" + json) == .available(latest: "v1.0.2", installed: "v1.0.1"))
        #expect(HelperUpdate.parseBrewOutdated(stdout: #"{"formulae":[],"casks":[]}"#) == .upToDate(installed: ""))
        #expect(HelperUpdate.parseBrewOutdated(stdout: "") == nil)
        #expect(HelperUpdate.parseBrewOutdated(stdout: "Error: no") == nil)
    }

    @Test func readsBrewUpgrade() {
        let out = "==> Upgrading gettogo/tap/togo\n==> Summary\ntogo v1.0.2 (darwin/arm64)\n"
        #expect(HelperUpdate.parseBrewUpgrade(stdout: out, stderr: "", status: 0) == .updated(version: "v1.0.2"))
        #expect(HelperUpdate.parseBrewUpgrade(stdout: "done\n", stderr: "", status: 0) == .updated(version: nil))
        #expect(
            HelperUpdate.parseBrewUpgrade(stdout: "", stderr: "Error: boom\n", status: 1) == .failed("Error: boom"))
        #expect(
            HelperUpdate.parseBrewUpgrade(stdout: "", stderr: "togo: Homebrew was not found on this machine\n", status: 127)
                == .failed("Homebrew was not found on this machine"))
    }

    @Test func aHelperThatUpdatesItselfWithHomebrewCountsAsUpdated() {
        let out = "Installed togo v1.0.2 with Homebrew\n"
        #expect(HelperUpdate.parseUpdate(stdout: out, stderr: "", status: 0) == .updated(version: "v1.0.2"))
        #expect(!HelperUpdate.isHomebrewRefusal(stdout: out, stderr: ""))
        #expect(HelperUpdate.isHomebrewRefusal(stdout: "", stderr: "togo: Installed with Homebrew: run brew upgrade togo"))
    }

    @Test func brewAnswersKeepTheHomebrewMark() {
        let id = UUID(), now = Date(timeIntervalSince1970: 0)
        var ledger = HelperUpdateLedger()
        ledger.record(.upToDate(installed: "v1.0.1"), for: id, at: now)
        ledger.record(.available(latest: "v1.0.2", installed: "v1.0.1"), for: id, at: now, viaHomebrew: true)
        #expect(ledger.machines[id]?.homebrew == true)
        ledger.record(.upToDate(installed: ""), for: id, at: now, viaHomebrew: true)
        #expect(ledger.machines[id]?.installed == "v1.0.1")
        #expect(ledger.available(for: id, current: "1.0.1") == nil)
    }
}
