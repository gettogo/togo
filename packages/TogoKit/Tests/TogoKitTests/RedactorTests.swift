import Testing
@testable import TogoKit

struct RedactorTests {
    @Test(arguments: [
        ("connect to 100.101.5.7 failed", "100.101.5.7"),
        ("connect to 100.101.5.7:22 failed", "100.101.5.7"),
        ("link fe80::1%en0 down", "fe80::1"),
        ("peer 2001:db8:85a3:0:0:8a2e:370:7334 gone", "2001:db8"),
        ("::1 refused", "::1"),
        ("no route to mac-mini.tail1234.ts.net", "mac-mini"),
        ("host.local timed out", "host.local"),
        ("signed in as jamie@mini", "jamie"),
        ("signed in as jamie@example.com", "example"),
        ("cwd /Users/jamie/Dev/togo", "jamie"),
        ("cwd /home/deploy/app", "deploy"),
        ("open ~/work now", "work"),
        ("opened /srv/projects/secret-app", "secret-app"),
        ("key ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJkGq8c3FfE0Zl1xqf2dQmJ9Hh3vXr7yPaWbN5sT4uVz jamie@mini", "AAAAC3"),
        ("-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmU\nQyNTUxOQAAACD\n-----END OPENSSH PRIVATE KEY-----", "b3BlbnNz"),
        ("token Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MC1hYmNkZWZnaGk", "Zm9vYmFy"),
        ("digest 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b", "9f86d081"),
        ("fetch https://git.corp.example/team/repo.git", "corp"),
        ("peer ::ffff:100.101.5.7 gone", "101.5.7"),
        ("peer 64:ff9b::100.101.5.7 gone", "101.5.7"),
        ("peer ::ffff:10.0.0.7 gone", "0.0.7"),
        ("as jamie@[fe80::1]:22 refused", "jamie"),
        ("as jamie@[fe80::1]:22 refused", "fe80"),
        ("clone git@github.com:acme/secret-repo.git", "acme"),
        ("clone git@github.com:acme/secret-repo.git", "secret-repo"),
        ("MD5:16:27:ac:a5:76:28:2d:36:63:1b:56:4d:eb:df:a6:48", "16:27"),
        ("token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dBjftJeZ4CVPmB92K27uhbUJU1p1r", "eyJzdWIi"),
        ("owner ~jamie/work", "jamie"),
        ("cwd: /Users/jamie/My Project/x", "Project"),
        ("cwd: /Users/jamie/My Project/x", "jamie"),
        (#"open C:\Users\jamie\Documents"#, "jamie"),
        (#"open \\mini\share\docs"#, "share"),
        ("host münchen.example.de down", "münchen"),
        ("id 9f86d081884c7d65", "9f86d081"),
        (#"open C:\Users\bob\My Docs\x"#, "Docs"),
        (#"open C:\Users\bob\My Docs\x"#, "bob"),
        ("secret abcdefghijklmnopqrstuvwxyzabcdefgh", "abcdefghijklmnop"),
    ])
    func redactsAddressesNamesPathsKeys(input: String, mustNotContain: String) {
        let output = Redactor.scrub(input)
        #expect(!output.contains(mustNotContain), "\(output)")
    }

    @Test func placeholders() {
        #expect(Redactor.scrub("100.101.5.7") == "<address>")
        #expect(Redactor.scrub("fe80::1%en0") == "<address>")
        #expect(Redactor.scrub("mac-mini.tail1234.ts.net") == "<host>")
        #expect(Redactor.scrub("jamie@mini") == "<user>@<host>")
        #expect(Redactor.scrub("::ffff:100.101.5.7") == "<address>")
        #expect(Redactor.scrub("jamie@[fe80::1]:22") == "<user>@<host>")
        #expect(Redactor.scrub("/Users/jamie/Dev/togo") == "<path>")
        #expect(Redactor.scrub("~/work") == "<path>")
        #expect(Redactor.scrub("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJkGq8c3 me@laptop") == "<key>")
        #expect(Redactor.scrub("Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MC1hYmNkZWZnaGk") == "<secret>")
    }

    @Test func keepsPlainWords() {
        for text in ["connection refused after 3 attempts", "helper 0.3.1 ready", "retry in 2.5s", "at 10:42:07 it dropped", "ssh-agent refused connection", "ssh-keygen failed here", "ecdsa-sk failed now"] {
            #expect(Redactor.scrub(text) == text)
        }
    }

    @Test func scrubbingTwiceChangesNothing() {
        let once = Redactor.scrub("jamie@mini at 100.101.5.7 in /Users/jamie/x")
        #expect(Redactor.scrub(once) == once)
    }

    @Test func knownNamesAreReplacedExactly() {
        let out = Redactor.scrub("Could not resolve hostname mini for MacBook-Pro", known: ["mini", "MacBook-Pro", "ab"])
        #expect(!out.contains("mini") && !out.contains("MacBook"))
        #expect(Redactor.scrub("a b ab", known: ["ab"]) == "a b ab", "names under three characters are ignored")
    }

    @Test(arguments: ["deploy-prod-db", "deploy_prod_db", "deploy prod db", "deploy.prod.db", "Deploy-Prod-DB", "deployproddb"])
    func knownNamesMatchAcrossSeparators(spelling: String) {
        let out = Redactor.scrub("failed on \(spelling) twice", known: ["deploy-prod-db"])
        #expect(out == "failed on <redacted> twice", "\(out)")
    }

    @Test(arguments: ["Jamie's MacBook", "Jamie\u{2019}s MacBook", "Jamies MacBook", "jamie_s_macbook"])
    func knownNamesIgnoreApostrophes(spelling: String) {
        for known in ["Jamie's MacBook", "Jamie\u{2019}s MacBook"] {
            let out = Redactor.scrub("host \(spelling) down", known: [known])
            #expect(!out.lowercased().contains("macbook"), "\(known) vs \(spelling): \(out)")
        }
    }
}
