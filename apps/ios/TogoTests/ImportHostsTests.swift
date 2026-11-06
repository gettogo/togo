import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor
struct ImportHostsTests {
    private static let config = """
    Host build
      HostName build.example.com
      User sam
      Port 2222
      ProxyJump bastion
    Host bastion
      HostName bastion.example.com
      User jump
    Host noUser
      HostName nouser.example.com
    Host lost
      HostName lost.example.com
      User sam
      ProxyJump ghost
    Host direct
      HostName direct.example.com
      User sam
      ProxyJump ops@gate.example.com:2200
    IdentityFile ~/.ssh/id_ed25519
    Match host x
    """

    private func existing(host: String, user: String, port: Int = 22) -> HostRecord {
        let id = UUID()
        return HostRecord(
            id: id,
            profile: HostProfile(id: id, name: host, endpoint: HostEndpoint(host: host, port: port, user: user), auth: .password),
            symbol: "laptop"
        )
    }

    private func model(existing: [HostRecord] = [], saved: SavedImports = SavedImports()) -> ImportHostsModel {
        ImportHostsModel(existing: { existing + saved.records }, deviceKeyID: { "device-key" }, save: { saved.records.append($0) })
    }

    @Test func duplicatesUnticked() {
        let model = model(existing: [existing(host: "Build.Example.com", user: "sam", port: 2222)])
        model.text = Self.config
        model.review()
        #expect(model.rows.first { $0.host.alias == "build" }?.status == .alreadyAdded)
        #expect(model.isTicked("build") == false)
        #expect(model.isTicked("bastion"))
        // The same host and user on another port is a different machine.
        let other = self.model(existing: [existing(host: "build.example.com", user: "sam", port: 22)])
        other.text = Self.config
        other.review()
        #expect(other.rows.first { $0.host.alias == "build" }?.status == .ready)
    }

    @Test func importedJumpAliasBecomesAJumpEndpoint() async {
        let saved = SavedImports()
        let model = model(saved: saved)
        model.text = Self.config
        model.review()
        await model.importTicked()
        let build = saved.records.first { $0.profile.name == "build" }
        #expect(build?.profile.endpoint == HostEndpoint(host: "build.example.com", port: 2222, user: "sam"))
        #expect(build?.profile.jumps == [HostEndpoint(host: "bastion.example.com", port: 22, user: "jump")])
        #expect(build?.profile.auth == .key(id: "device-key"))
        let direct = saved.records.first { $0.profile.name == "direct" }
        #expect(direct?.profile.jumps == [HostEndpoint(host: "gate.example.com", port: 2200, user: "ops")])
    }

    @Test func jumpToAnExistingMachineIsFound() {
        let model = model(existing: [existing(host: "ghost", user: "ops", port: 2200)])
        model.text = Self.config
        model.review()
        #expect(model.rows.first { $0.host.alias == "lost" }?.status == .ready)
    }

    @Test func unresolvedJumpAndMissingUserAreUntickedWithReasons() {
        let model = model()
        model.text = Self.config
        model.review()
        #expect(model.rows.first { $0.host.alias == "lost" }?.status == .blocked("ghost isn't in this file or your machines."))
        #expect(model.rows.first { $0.host.alias == "noUser" }?.status == .blocked("Enter the user name you sign in with."))
        model.toggle("lost")
        #expect(model.isTicked("lost") == false, "a blocked row cannot be ticked")
    }

    @Test func skippedLinesKeepTheirReasons() {
        let model = model()
        model.text = Self.config
        model.review()
        let reasons = model.skipped.map(\.reason)
        #expect(reasons.contains("Keys stay on this iPhone; pick one after import."))
        #expect(reasons.contains("Match blocks aren't supported."))
    }

    @Test func importSavesOnlyTickedRows() async {
        let saved = SavedImports()
        let model = model(saved: saved)
        model.text = Self.config
        model.review()
        model.toggle("bastion")
        await model.importTicked()
        #expect(Set(saved.records.map(\.profile.name)) == ["build", "direct"])
        #expect(model.imported == 2)
    }

    @Test func oversizedTextGetsAPlainSentence() {
        let model = model()
        model.text = String(repeating: "#", count: SSHConfigImport.maxBytes + 1)
        model.review()
        #expect(model.rows.isEmpty)
        #expect(model.message == "That is more than 256 KiB. Paste only the hosts you need.")
        #expect(model.loadFile(Data(count: SSHConfigImport.maxBytes + 1)) == false)
        #expect(model.message == "That file is more than 256 KiB. Paste only the hosts you need.")
    }

    @Test func textWithNoHostsSaysSo() {
        let model = model()
        model.text = "# nothing here\n"
        model.review()
        #expect(model.message == "No hosts found in that text.")
    }

    @Test func importedHostPrefillsTheForm() {
        let draft = MachineDraft(imported: ImportedHost(
            alias: "build", hostName: "build.example.com", user: "sam", port: 2222,
            jump: ImportedJump(host: "gate.example.com", user: "ops", port: 2200)
        ))
        #expect(draft.name == "build")
        #expect(draft.address == "build.example.com")
        #expect(draft.port == "2222")
        #expect(draft.user == "sam")
        #expect(draft.jump == "ops@gate.example.com:2200")
        #expect(MachineDraft(imported: ImportedHost(alias: "a", hostName: "a")).port == "22")
    }

    @Test func rowsFailTheFormChecksAreNeverSaved() async {
        let saved = SavedImports()
        let model = model(saved: saved)
        model.text = """
        Host spaced
          HostName "a b"
          User sam
        Host slashed
          HostName a/b
          User sam
        Host badUser
          HostName ok.example.com
          User "x y"
        Host dashed
          HostName -oProxyCommand=x
          User sam
        Host dashUser
          HostName ok2.example.com
          User -x
        Host badJump
          HostName ok3.example.com
          User sam
          ProxyJump "a b@c"
        Host fine
          HostName fine.example.com
          User sam
        """
        model.review()
        for alias in ["spaced", "slashed", "badUser", "dashed", "dashUser", "badJump"] {
            guard case .blocked = model.rows.first(where: { $0.host.alias == alias })?.status else {
                Issue.record("\(alias) should be blocked")
                continue
            }
            #expect(model.isTicked(alias) == false)
        }
        #expect(model.rows.first { $0.host.alias == "spaced" }?.status == .blocked("Enter only the host name, without a user or path."))
        #expect(model.rows.first { $0.host.alias == "badUser" }?.status == .blocked("User names can't contain spaces."))
        await model.importTicked()
        #expect(saved.records.map(\.profile.name) == ["fine"])
    }

    @Test func retryAfterPartialFailureNeverDuplicates() async {
        let saved = SavedImports()
        var calls = 0
        let model = ImportHostsModel(
            existing: { saved.records },
            deviceKeyID: { "k" },
            save: {
                calls += 1
                if calls == 2 { throw CocoaError(.fileWriteUnknown) }
                saved.records.append($0)
            }
        )
        model.text = Self.config
        model.review()
        await model.importTicked()
        #expect(saved.records.count == 1)
        #expect(model.finished == false)
        #expect(model.failure == "Added 1, then couldn't save the rest. Try again to add the others.")
        await model.importTicked()
        #expect(model.finished)
        #expect(saved.records.count == 3)
        #expect(Set(saved.records.map(\.profile.name)).count == 3)
        #expect(model.imported == 3)
    }

    @Test func machineAddedSinceThePreviewIsNotAddedAgain() async {
        let saved = SavedImports()
        let model = model(saved: saved)
        model.text = "Host a\n  HostName a.example.com\n  User sam\n"
        model.review()
        saved.records.append(existing(host: "a.example.com", user: "sam"))
        await model.importTicked()
        #expect(saved.records.count == 1)
    }

    @Test func duplicateHostInTheFileIsListedOnce() {
        let model = model()
        model.text = "Host a b\n  HostName x.example.com\n  User sam\n"
        model.review()
        #expect(model.rows.map(\.status) == [.ready, .alreadyAdded])
    }

    @Test func fileLoadingReportsProblemsInWords() throws {
        let model = model()
        let dir = FileManager.default.temporaryDirectory
        let empty = dir.appendingPathComponent(UUID().uuidString)
        try Data().write(to: empty)
        #expect(model.load(empty) == false)
        #expect(model.message == "Couldn't read that file.")
        let good = dir.appendingPathComponent(UUID().uuidString)
        try Data("Host a\n  User sam\n".utf8).write(to: good)
        #expect(model.load(good))
        #expect(model.rows.map(\.id) == ["a"])
        #expect(model.load(dir.appendingPathComponent("missing")) == false)
    }

    @Test func skippedLinesShowWhenNoHostsFound() {
        let model = model()
        model.text = "Include other\n"
        model.review()
        #expect(model.message == "No hosts found in that text.")
        #expect(model.skipped.count == 1)
    }

    @Test func ipv6JumpIsBracketedInTheForm() {
        let draft = MachineDraft(imported: ImportedHost(
            alias: "a", hostName: "a", user: "sam", jump: ImportedJump(host: "fe80::1", user: "ops", port: 2200)
        ))
        #expect(draft.jump == "ops@[fe80::1]:2200")
        #expect(draft.errors[.jump] == nil)
    }
}

@MainActor final class SavedImports {
    var records: [HostRecord] = []
}
