import Foundation
import Testing
import TogoKit

struct SSHConfigImportTests {
    @Test func parsesAliasesAndDefaults() throws {
        let result = try SSHConfigImport.parse("""
        Host web
            HostName 10.0.0.5
            User deploy
            Port 2200
        Host bare
        """)
        #expect(result.hosts.map(\.alias) == ["web", "bare"])
        #expect(result.hosts[0].hostName == "10.0.0.5")
        #expect(result.hosts[0].user == "deploy")
        #expect(result.hosts[0].port == 2200)
        #expect(result.hosts[1].hostName == "bare")
        #expect(result.hosts[1].user == nil)
        #expect(result.hosts[1].port == nil)
        #expect(result.hosts[1].jump == nil)
        #expect(result.skipped.isEmpty)
    }

    @Test func proxyJumpToAlias() throws {
        let result = try SSHConfigImport.parse("""
        Host app
            HostName app.internal
            ProxyJump bastion
        Host viaUser
            ProxyJump ops@gate.example.com:2222,other
        Host plain
            ProxyJump somewhere
        Host bastion
            HostName 203.0.113.9
            User jump
            Port 2201
            ProxyJump elsewhere
        """)
        let app = try #require(result.hosts.first { $0.alias == "app" })
        #expect(app.jump == ImportedJump(host: "203.0.113.9", user: "jump", port: 2201, alias: "bastion"))
        let via = try #require(result.hosts.first { $0.alias == "viaUser" })
        #expect(via.jump == ImportedJump(host: "gate.example.com", user: "ops", port: 2222, alias: nil))
        let plain = try #require(result.hosts.first { $0.alias == "plain" })
        #expect(plain.jump == ImportedJump(host: "somewhere", user: nil, port: nil, alias: nil))
    }

    @Test func wildcardOnlyDefaults() throws {
        let result = try SSHConfigImport.parse("""
        Host *
            User everyone
            Port 2022
        Host prod-*
            User prod
        Host dev?
            Port 3000
        Host *.lab !skip.lab
            User lab
        Host web1 skip.lab other.lab
        """)
        #expect(result.hosts.map(\.alias) == ["web1", "skip.lab", "other.lab"])
        #expect(result.hosts[0].user == "everyone")
        #expect(result.hosts[0].port == 2022)
        #expect(result.hosts[1].user == "everyone")
        #expect(result.hosts[2].user == "everyone")
    }

    @Test func wildcardMatchingRules() throws {
        let result = try SSHConfigImport.parse("""
        Host prod-*
            User prod
        Host dev?
            Port 3000
        Host *.lab !skip.lab
            HostName lab.example
        Host prod-1 dev1 dev12 skip.lab a.lab
        """)
        let byAlias = Dictionary(uniqueKeysWithValues: result.hosts.map { ($0.alias, $0) })
        #expect(byAlias["prod-1"]?.user == "prod")
        #expect(byAlias["dev1"]?.port == 3000)
        #expect(byAlias["dev12"]?.port == nil)
        #expect(byAlias["skip.lab"]?.hostName == "skip.lab")
        #expect(byAlias["a.lab"]?.hostName == "lab.example")
    }

    @Test func skipsMatchAndInclude() throws {
        let result = try SSHConfigImport.parse("""
        Include ~/.ssh/extra
        Host a
            HostName a.example
        Match host b
            User ignored
        Host c
            User kept
        """)
        #expect(result.hosts.map(\.alias) == ["a", "c"])
        #expect(result.hosts[0].user == nil)
        #expect(result.hosts[1].user == "kept")
        #expect(result.skipped == [
            .init(line: 1, keyword: "Include", reason: "Include isn't supported; paste the included file too."),
            .init(line: 4, keyword: "Match", reason: "Match blocks aren't supported."),
        ])
    }

    @Test func matchBodyIsIgnored() throws {
        let result = try SSHConfigImport.parse("""
        match all
            User ignored
            IdentityFile ~/.ssh/id
        Host c
        """)
        #expect(result.hosts.map(\.alias) == ["c"])
        #expect(result.hosts[0].user == nil)
        #expect(result.skipped.count == 1)
        #expect(result.skipped[0].keyword == "match")
    }

    @Test func quotedAndEqualsForms() throws {
        let result = try SSHConfigImport.parse("""
        Host = "my box" other
          HOSTNAME=1.2.3.4
          port = 2222
          uSeR "some user"
        """)
        #expect(result.hosts.map(\.alias) == ["my box", "other"])
        #expect(result.hosts[0].hostName == "1.2.3.4")
        #expect(result.hosts[0].port == 2222)
        #expect(result.hosts[0].user == "some user")
        #expect(result.hosts[1].hostName == "1.2.3.4")
    }

    @Test func ignoresIdentityFileWithNote() throws {
        let result = try SSHConfigImport.parse("Host a\r\n  identityfile ~/.ssh/id_ed25519\r\n  ProxyCommand ssh -W %h:%p x\r\n")
        #expect(result.hosts == [ImportedHost(alias: "a", hostName: "a")])
        #expect(result.skipped == [
            .init(line: 2, keyword: "identityfile", reason: "Keys stay on this iPhone; pick one after import."),
            .init(line: 3, keyword: "ProxyCommand", reason: "ProxyCommand isn't supported; use ProxyJump."),
        ])
    }

    @Test func rejectsOver256KiB() throws {
        let limit = SSHConfigImport.maxBytes
        #expect(limit == 262_144)
        #expect(throws: SSHConfigImportError.tooLarge) {
            try SSHConfigImport.parse(String(repeating: "a", count: limit + 1))
        }
        let result = try SSHConfigImport.parse(String(repeating: "#", count: limit))
        #expect(result.hosts.isEmpty)
    }

    @Test func invalidPortIgnored() throws {
        let result = try SSHConfigImport.parse("""
        Host a
            Port 0
            Port 70000
            Port abc
        Host b
            Port 0
            Port 2222
            Port 3333
        """)
        #expect(result.hosts[0].port == nil)
        #expect(result.hosts[1].port == 2222)
    }

    @Test func firstValueWins() throws {
        let result = try SSHConfigImport.parse("""
        Host *
            User wild
        Host web
            User mine
            HostName web.example
        Host web
            HostName other.example
        """)
        #expect(result.hosts.count == 1)
        #expect(result.hosts[0].user == "wild")
        #expect(result.hosts[0].hostName == "web.example")
    }

    @Test func pathologicalGlobFinishesFast() throws {
        let pattern = String(repeating: "*a", count: 40) + "*b"
        let text = "Host \(pattern)\n User x\nHost \(String(repeating: "a", count: 200))\n"
        let clock = ContinuousClock()
        var result = ImportResult()
        let elapsed = try clock.measure { result = try SSHConfigImport.parse(text) }
        #expect(elapsed < .seconds(1))
        #expect(result.hosts.count == 1)
        #expect(result.hosts[0].user == nil)
    }

    @Test func hugePatternAndAliasStayLinearish() throws {
        let long = String(repeating: "a", count: 100_000)
        let result = try SSHConfigImport.parse("Host *b\n User x\nHost \(long)\n")
        #expect(result.hosts.isEmpty)
        #expect(result.skipped.map(\.reason) == ["Name is too long."])
    }

    @Test func emptyHostNamesAreRejected() throws {
        let result = try SSHConfigImport.parse("Host \"\"\n User x\nHost a \"\"\n")
        #expect(result.hosts.map(\.alias) == ["a"])
        #expect(result.hosts[0].user == nil)
    }

    @Test func bareHostMatchesNothingAndEndsPreviousBlock() throws {
        let result = try SSHConfigImport.parse("Host a\n User one\nHost\n User two\nHost b\n")
        #expect(result.hosts.map(\.alias) == ["a", "b"])
        #expect(result.hosts[0].user == "one")
        #expect(result.hosts[1].user == nil)
    }

    @Test func bareHostEndsMatchBlock() throws {
        let result = try SSHConfigImport.parse("Match all\nHost\n User x\nHost b\n")
        #expect(result.hosts.map(\.alias) == ["b"])
        #expect(result.hosts[0].user == nil)
    }

    @Test func proxyJumpNoneMeansNoJump() throws {
        let result = try SSHConfigImport.parse("""
        Host bastion
            HostName 1.2.3.4
            ProxyJump none
        Host *
            ProxyJump bastion
        Host inner
        """)
        let byAlias = Dictionary(uniqueKeysWithValues: result.hosts.map { ($0.alias, $0) })
        #expect(byAlias["bastion"]?.jump == nil)
        #expect(byAlias["inner"]?.jump?.alias == "bastion")
        #expect(byAlias["inner"]?.jump?.host == "1.2.3.4")
    }

    @Test func percentHIsSubstitutedAndOtherTokensDropped() throws {
        let result = try SSHConfigImport.parse("""
        Host web db
            HostName %h.example.com
        Host odd
            HostName %n.example.com
        Host pct
            HostName 100%%.example
        """)
        let byAlias = Dictionary(uniqueKeysWithValues: result.hosts.map { ($0.alias, $0) })
        #expect(byAlias["web"]?.hostName == "web.example.com")
        #expect(byAlias["db"]?.hostName == "db.example.com")
        #expect(byAlias["odd"]?.hostName == "odd")
        #expect(byAlias["pct"]?.hostName == "100%.example")
    }

    @Test func trailingCommentsAndExtraWordsAreDropped() throws {
        let result = try SSHConfigImport.parse("Host a\n HostName h.example # prod\n User bob extra\n Port 22 #x\n")
        #expect(result.hosts[0].hostName == "h.example")
        #expect(result.hosts[0].user == "bob")
        #expect(result.hosts[0].port == 22)
    }

    @Test func unterminatedQuotesAreIgnored() throws {
        let result = try SSHConfigImport.parse("Host \"abc\nHost b\n User \"abc\n User ok\n")
        #expect(result.hosts.map(\.alias) == ["b"])
        #expect(result.hosts[0].user == "ok")
    }

    @Test func proxyJumpAliasIsCaseInsensitiveAndUserOverrides() throws {
        let result = try SSHConfigImport.parse("""
        Host bastion
            HostName 9.9.9.9
            User base
            Port 2201
        Host a
            ProxyJump Bastion
        Host b
            ProxyJump ops@bastion
        Host c
            ProxyJump bastion:3000
        """)
        let byAlias = Dictionary(uniqueKeysWithValues: result.hosts.map { ($0.alias, $0) })
        #expect(byAlias["a"]?.jump == ImportedJump(host: "9.9.9.9", user: "base", port: 2201, alias: "bastion"))
        #expect(byAlias["b"]?.jump == ImportedJump(host: "9.9.9.9", user: "ops", port: 2201, alias: "bastion"))
        #expect(byAlias["c"]?.jump == ImportedJump(host: "9.9.9.9", user: "base", port: 3000, alias: "bastion"))
    }

    @Test func proxyJumpIPv6AndUri() throws {
        let result = try SSHConfigImport.parse("""
        Host a
            ProxyJump fe80::1
        Host b
            ProxyJump u@[::1]:2222
        Host c
            ProxyJump ssh://v@gate.example:2200
        """)
        #expect(result.hosts[0].jump == ImportedJump(host: "fe80::1"))
        #expect(result.hosts[1].jump == ImportedJump(host: "::1", user: "u", port: 2222))
        #expect(result.hosts[2].jump == ImportedJump(host: "gate.example", user: "v", port: 2200))
    }

    @Test func byteOrderMarkIsStripped() throws {
        let result = try SSHConfigImport.parse("\u{FEFF}Host a\n User x\n")
        #expect(result.hosts.map(\.alias) == ["a"])
        #expect(result.hosts[0].user == "x")
    }

    @Test func sizeLimitCountsBytesNotCharacters() throws {
        let text = String(repeating: "\u{E9}", count: 131_073)
        #expect(text.utf8.count == 262_146)
        #expect(throws: SSHConfigImportError.tooLarge) { try SSHConfigImport.parse(text) }
    }

    @Test func negatedOnlyBlockAppliesToNothing() throws {
        let result = try SSHConfigImport.parse("Host !x\n User n\nHost x y\n")
        #expect(result.hosts.map(\.alias) == ["x", "y"])
        #expect(result.hosts.allSatisfy { $0.user == nil })
    }

    @Test func longNamesAreSkippedAndNeverMatched() throws {
        let long = String(repeating: "a", count: 256)
        let result = try SSHConfigImport.parse("Host \(long) ok\n User x\n")
        #expect(result.hosts.map(\.alias) == ["ok"])
        #expect(result.skipped == [.init(line: 1, keyword: "Host", reason: "Name is too long.")])
        let atCap = String(repeating: "a", count: 255)
        #expect(try SSHConfigImport.parse("Host \(atCap)\n").hosts.count == 1)
    }

    @Test func patternCountIsCapped() throws {
        var text = ""
        for i in 0..<1_005 { text += "Host h\(i)\n" }
        let result = try SSHConfigImport.parse(text)
        #expect(result.hosts.count == 1_000)
        #expect(result.skipped.count == 1)
        #expect(result.skipped[0].line == 1_001)
    }

    @Test func worstCaseAtTheCapsIsFast() throws {
        let pattern = String(repeating: "*a", count: 126) + "*b"
        var text = ""
        for _ in 0..<400 { text += "Host \(pattern)\n" }
        for i in 0..<400 { text += "Host \(String(repeating: "a", count: 250))\(String(format: "%05d", i))\n" }
        #expect(text.utf8.count <= SSHConfigImport.maxBytes)
        let clock = ContinuousClock()
        let elapsed = try clock.measure { _ = try SSHConfigImport.parse(text) }
        #expect(elapsed < .milliseconds(500))
    }

    @Test func thirtyKilobytePathologicalConfigIsFast() throws {
        let text = "Host *" + String(repeating: "a*", count: 100) + "b\nHost " + String(repeating: "a", count: 255) + "\n"
            + String(repeating: "# padding\n", count: 3_000)
        let clock = ContinuousClock()
        let elapsed = try clock.measure { _ = try SSHConfigImport.parse(text) }
        #expect(elapsed < .seconds(1))
    }
}
