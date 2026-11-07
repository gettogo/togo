import Foundation

/// What a machine has for Togo: the helper, tmux, mosh-server and the kind of system.
public struct HelperReport: Sendable, Equatable {
    public var helper: HelperPresence
    public var path: String?
    /// The first line `tmux -V` prints, such as "tmux 3.5a".
    public var tmux: String?
    public var mosh: Bool
    /// `uname -s`: "Darwin", "Linux".
    public var os: String?
    /// The login was handled by Tailscale SSH rather than an OpenSSH server.
    public var tailscaleSSH: Bool
    /// The routes the helper reported; nil without a helper new enough to say.
    public var routes: [HostRoute]?

    public init(
        helper: HelperPresence, path: String? = nil, tmux: String? = nil, mosh: Bool = false, os: String? = nil,
        tailscaleSSH: Bool = false, routes: [HostRoute]? = nil
    ) {
        self.helper = helper
        self.path = path
        self.tmux = tmux
        self.mosh = mosh
        self.os = os
        self.tailscaleSSH = tailscaleSSH
        self.routes = routes
    }

    /// Whether the helper said the machine is on a tailnet.
    public var machineOnTailnet: Bool {
        routes?.contains { $0.kind == .tailscale } ?? false
    }

    /// The helper needs tmux 3.2 or newer, the same floor `togo doctor` checks.
    public var tmuxSupported: Bool {
        guard let tmux, let (major, minor) = Self.majorMinor(in: tmux) else { return false }
        return major > 3 || major == 3 && minor >= 2
    }

    private static func majorMinor(in text: String) -> (Int, Int)? {
        guard let match = text.firstMatch(of: /(\d+)\.(\d+)/), let major = Int(match.1), let minor = Int(match.2) else {
            return nil
        }
        return (major, minor)
    }
}

/// Looks at a machine over an open SSH connection before it is saved.
public enum HelperProbe {
    /// One line with no single quotes, backslashes or `!`, so csh, fish and nushell login
    /// shells pass it to `/bin/sh` untouched (see `HelperCommand.serveStdio`). An exec
    /// channel's PATH rarely includes Homebrew or `~/.togo/bin`, so those go first, in the
    /// order `serveStdio` tries them. The last step walks a few parent processes: under
    /// Tailscale SSH the shell's ancestors are `tailscaled`, not `sshd`.
    public static let script: String = [
        #"PATH="$HOME/.togo/bin:/opt/homebrew/bin:/usr/local/bin:$PATH""#,
        "export PATH",
        "t=$(command -v togo 2>/dev/null)",
        #"if [ -n "$t" ]; then echo "togo.path=$t"; echo "togo.version=$("$t" version 2>/dev/null | head -n 1)"; fi"#,
        #"echo "tmux=$(tmux -V 2>/dev/null | head -n 1)""#,
        "if command -v mosh-server >/dev/null 2>&1; then echo mosh=yes; else echo mosh=no; fi",
        #"echo "os=$(uname -s)""#,
        "p=$PPID",
        "n=0",
        #"while [ -n "$p" ] && [ "$p" -gt 1 ] && [ "$n" -lt 4 ]; do "#
            + #"case "$(ps -o comm= -p "$p" 2>/dev/null)" in *tailscale*) echo ssh=tailscale; break;; esac; "#
            + #"p=$(ps -o ppid= -p "$p" 2>/dev/null | tr -d " "); n=$((n+1)); done"#,
    ].joined(separator: "; ")

    /// `script` as the exec command, run by `/bin/sh` whatever the login shell is.
    public static var command: String { shellCommand(script) }

    /// Reads the `key=value` lines `script` prints. Anything else, such as a banner from a
    /// login file, is ignored.
    public static func parse(_ output: String) -> HelperReport {
        var values: [String: String] = [:]
        for line in output.split(whereSeparator: \.isNewline) {
            guard let equals = line.firstIndex(of: "=") else { continue }
            let key = String(line[..<equals])
            let value = line[line.index(after: equals)...].trimmingCharacters(in: .whitespaces)
            values[key] = value
        }
        let path = values["togo.path"].flatMap(nonEmpty)
        return HelperReport(
            helper: path == nil ? .missing : presence(versionLine: values["togo.version"] ?? ""),
            path: path,
            tmux: values["tmux"].flatMap(nonEmpty),
            mosh: values["mosh"] == "yes",
            os: values["os"].flatMap(nonEmpty),
            tailscaleSSH: values["ssh"] == "tailscale"
        )
    }

    /// The command a person pastes on the machine to install the helper. Systems the
    /// release installer does not cover build it with Go instead.
    public static func installCommand(os: String?) -> String {
        switch os {
        case nil, "Darwin", "Linux":
            "curl -fsSL https://github.com/gettogo/togo/releases/latest/download/install.sh | sh"
        default:
            "go install github.com/gettogo/togo/helper/cmd/togo@latest"
        }
    }

    public static func run(on connection: SSHConnection) async throws -> HelperReport {
        let result = try await connection.run(command, timeout: .seconds(15))
        var report = parse(String(decoding: result.stdout, as: UTF8.self))
        if case .present = report.helper, let path = report.path {
            report.routes = await routes(on: connection, helperPath: path)
        }
        return report
    }

    /// The routes the helper at `helperPath` reports, or nil when it cannot say.
    static func routes(on connection: SSHConnection, helperPath: String) async -> [HostRoute]? {
        guard let channel = try? await connection.openExec(shellQuote([helperPath, "serve", "--stdio"])) else { return nil }
        let helper = HelperClient(channel: channel)
        defer { Task { await helper.close() } }
        guard (try? await helper.hello(client: SessionAttach.client)) != nil else { return nil }
        return (try? await helper.hostInfo())?.hostRoutes
    }

    /// `togo version` prints "togo 0.3.0 (darwin/arm64)".
    private static func presence(versionLine: String) -> HelperPresence {
        let words = versionLine.split(separator: " ")
        guard words.count >= 2, words[0] == "togo" else { return .outdated(versionLine) }
        return .present(version: String(words[1]))
    }

    private static func nonEmpty(_ value: String) -> String? {
        value.isEmpty ? nil : value
    }
}
