import Foundation
import Testing
import TogoKit

struct HelperProbeTests {
    @Test func probesEtserver() {
        #expect(HelperProbe.script.contains("command -v etserver"))
        #expect(HelperProbe.parse("et=yes\n").et)
        #expect(!HelperProbe.parse("et=no\n").et)
        #expect(!HelperProbe.parse("mosh=yes\n").et)
    }

    @Test func parsesPresentHelper() {
        let output = """
        togo.path=/Users/sam/.togo/bin/togo
        togo.version=togo 0.3.0 (darwin/arm64)
        tmux=tmux 3.5a
        mosh=yes
        os=Darwin
        """
        let report = HelperProbe.parse(output)
        #expect(report == HelperReport(
            helper: .present(version: "0.3.0"), path: "/Users/sam/.togo/bin/togo", tmux: "tmux 3.5a", mosh: true, os: "Darwin"
        ))
        #expect(report.tmuxSupported)
        #expect(!report.tailscaleSSH)
    }

    @Test func parsesMissingHelper() {
        let output = "tmux=\nmosh=no\nos=Linux\nssh=tailscale\n"
        let report = HelperProbe.parse(output)
        #expect(report.helper == .missing)
        #expect(report.path == nil)
        #expect(report.tmux == nil)
        #expect(!report.mosh)
        #expect(report.os == "Linux")
        #expect(report.tailscaleSSH)
        #expect(!report.tmuxSupported)
    }

    @Test func parsesOldTmux() {
        let report = HelperProbe.parse("togo.path=/usr/local/bin/togo\ntogo.version=togo 0.3.0 (linux/amd64)\ntmux=tmux 2.9a\nos=Linux\n")
        #expect(report.tmux == "tmux 2.9a")
        #expect(!report.tmuxSupported)
        #expect(HelperProbe.parse("tmux=tmux 3.2\n").tmuxSupported)
        #expect(HelperProbe.parse("tmux=tmux next-3.6\n").tmuxSupported)
        #expect(!HelperProbe.parse("tmux=tmux 3.1c\n").tmuxSupported)
    }

    @Test func parsesHelperWithoutReadableVersion() {
        let report = HelperProbe.parse("togo.path=/opt/homebrew/bin/togo\ntogo.version=\n")
        #expect(report.helper == .outdated(""))
        #expect(report.path == "/opt/homebrew/bin/togo")
    }

    @Test func ignoresNoiseFromLoginFiles() {
        let report = HelperProbe.parse("Welcome to Ubuntu\nos=Linux\nnot a pair\n")
        #expect(report.os == "Linux")
        #expect(report.helper == .missing)
    }

    /// A fish, csh or nushell login shell receives the command first. It must see one quoted
    /// word with nothing those shells treat specially inside single quotes.
    @Test func scriptRunsUnderShQuoted() throws {
        let script = HelperProbe.script
        #expect(!script.contains("'"))
        #expect(!script.contains("\\"))
        #expect(!script.contains("\n"))
        #expect(!script.contains("!"))
        #expect(HelperProbe.command == "/bin/sh -c '\(script)'")

        let argv = try run("/bin/sh", ["-c", "printf '%s\\0' " + HelperProbe.command])
            .split(separator: 0, omittingEmptySubsequences: false).dropLast()
            .map { String(decoding: $0, as: UTF8.self) }
        #expect(argv == ["/bin/sh", "-c", script])

        let report = HelperProbe.parse(String(decoding: try run("/bin/sh", ["-c", HelperProbe.command]), as: UTF8.self))
        let uname = String(decoding: try run("/bin/sh", ["-c", "uname -s"]), as: UTF8.self)
        #expect(report.os == uname.trimmingCharacters(in: .whitespacesAndNewlines))
        if let fish = ["/opt/homebrew/bin/fish", "/usr/local/bin/fish"].first(where: FileManager.default.isExecutableFile) {
            let viaFish = HelperProbe.parse(String(decoding: try run(fish, ["-c", HelperProbe.command]), as: UTF8.self))
            #expect(viaFish == report)
        }
    }

    @Test func installCommandIsOneLine() {
        for os in [nil, "Darwin", "Linux", "FreeBSD"] {
            let command = HelperProbe.installCommand(os: os)
            #expect(!command.isEmpty)
            #expect(!command.contains("\n"))
        }
    }

    /// tmux missing or too old: a command to paste, by system. Homebrew on a Mac, apt where it is
    /// likely; elsewhere nil, and the screen says to use the system's package manager.
    @Test func tmuxInstallCommandBySystem() {
        #expect(HelperProbe.tmuxInstallCommand(os: "Darwin") == "brew install tmux")
        #expect(HelperProbe.tmuxInstallCommand(os: nil) == "brew install tmux")
        #expect(HelperProbe.tmuxInstallCommand(os: "Linux") == "sudo apt install tmux")
        #expect(HelperProbe.tmuxInstallCommand(os: "FreeBSD") == nil)
    }

    private func run(_ executable: String, _ arguments: [String]) throws -> Data {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: executable)
        process.arguments = arguments
        let out = Pipe()
        process.standardOutput = out
        process.standardError = Pipe()
        try process.run()
        let data = out.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        return data
    }
}

struct TailnetHintTests {
    @Test func tailnetHintRanges() {
        let tailnet = [
            "studio.tail1234.ts.net", "Studio.Tail1234.TS.NET.", "100.64.0.1", "100.101.102.103", "100.127.255.254",
            "fd7a:115c:a1e0::1", "[fd7a:115c:a1e0:ab12::42]", "FD7A:115C:A1E0::9",
        ]
        let elsewhere = [
            "studio.local", "100.63.255.255", "100.128.0.1", "10.0.0.4", "192.168.1.20", "ts.net.example.com",
            "fd7a:115c:a1e1::1", "fe80::1%en0", "", "100.64.0", "100.64.0.1.5", "nets.net",
        ]
        for host in tailnet { #expect(TailnetHint.isTailnet(host), "\(host)") }
        for host in elsewhere { #expect(!TailnetHint.isTailnet(host), "\(host)") }
    }
}
