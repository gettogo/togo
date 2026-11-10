import Foundation

/// Where a freshly started mosh-server listens, and the session key it printed.
public struct MoshEndpoint: Sendable, Equatable, CustomStringConvertible {
    public var host: String
    public var port: Int
    public var key: String
    /// The detached server's process id, when it reported one, so a start that never
    /// reached the client can be stopped instead of waiting out its timeout.
    public var serverPID: Int32?

    public init(host: String, port: Int, key: String, serverPID: Int32? = nil) {
        self.host = host
        self.port = port
        self.key = key
        self.serverPID = serverPID
    }

    public var description: String { "MoshEndpoint(\(host):\(port), key: <redacted>)" }
}

public enum MoshBootstrapError: Error, Equatable, LocalizedError {
    case serverMissing(String)
    case noConnectLine(String)
    case malformedKey

    public var errorDescription: String? {
        switch self {
        case .serverMissing:
            String(localized: "mosh-server was not found on the machine. Install mosh there, or connect with SSH.", bundle: .module)
        case .noConnectLine:
            String(localized: "mosh-server did not start on the machine. A missing UTF-8 locale is a common cause.", bundle: .module)
        case .malformedKey:
            String(localized: "mosh-server sent a session key that could not be read.", bundle: .module)
        }
    }
}

/// Builds the command that starts mosh-server over SSH and reads its reply.
public enum MoshBootstrap {
    static let notFoundMessage = "togo: mosh-server not found"

    /// Non-interactive SSH sessions often lack Homebrew and Nix on PATH, so the usual
    /// install locations are tried after PATH.
    static let searchPath = [
        "mosh-server",
        "/opt/homebrew/bin/mosh-server",
        "/usr/local/bin/mosh-server",
        "/usr/bin/mosh-server",
        "/nix/var/nix/profiles/default/bin/mosh-server",
    ]

    /// The script is one line with no backslashes, so it survives the outer quoting
    /// under fish (which unescapes `\\` and `\'` inside single quotes) and csh (which
    /// rejects newlines inside quotes). Backslashes and newlines inside argv are
    /// rebuilt on the machine from shell variables instead.
    public static func command(argv: [String], serverPath: String?, ports: PortRange?, bind: MoshBind = .sshAddress) -> String {
        let candidates = serverPath.map { [quoteBackslashFree($0)] }
            ?? searchPath.map(quoteBackslashFree) + [#""$HOME/.nix-profile/bin/mosh-server""#]
        var arguments = ["new"]
        switch bind {
        case .sshAddress: arguments.append("-s")
        case .address(let address): arguments += ["-i", address]
        }
        arguments += ["-c", "256", "-l", "LANG=en_US.UTF-8"]
        if let ports { arguments += ["-p", "\(ports.lower):\(ports.upper)"] }
        let joined = argv.joined()
        var steps = ["MOSH_SERVER_NETWORK_TMOUT=3600", "export MOSH_SERVER_NETWORK_TMOUT"]
        if joined.contains("\n") { steps.append(characterVariable("togo_nl", code: 10)) }
        if joined.contains("\\") { steps.append(characterVariable("togo_bs", code: 92)) }
        let invocation = (arguments + ["--"]).map(quoteBackslashFree).joined(separator: " ")
            + " " + argv.map(quoteBackslashFree).joined(separator: " ")
        steps.append(
            "for togo_p in \(candidates.joined(separator: " ")); do "
                + "if command -v \"$togo_p\" >/dev/null 2>&1; then exec \"$togo_p\" \(invocation); fi; done"
        )
        steps.append("echo '\(notFoundMessage)' >&2")
        steps.append("exit 127")
        return shellCommand(steps.joined(separator: "; "))
    }

    public static func parse(stdout: Data, stderr: Data, status: Int32, host: String) throws -> MoshEndpoint {
        let lastError = lines(stderr).last { !$0.isEmpty } ?? ""
        if status == 127 || lines(stderr).contains(notFoundMessage) {
            throw MoshBootstrapError.serverMissing(lastError)
        }
        for line in lines(stdout) {
            guard let match = line.wholeMatch(of: /MOSH CONNECT (\d+) ([A-Za-z0-9\/+]{22})\s*/) else { continue }
            guard let port = Int(match.1), (1...65_535).contains(port) else { continue }
            let key = String(match.2)
            // A mosh key is 16 random bytes in unpadded base64.
            guard Data(base64Encoded: key + "==")?.count == 16 else { throw MoshBootstrapError.malformedKey }
            return MoshEndpoint(host: host, port: port, key: key, serverPID: serverPID(stderr))
        }
        throw MoshBootstrapError.noConnectLine(lastError)
    }

    /// mosh-server's parent prints `[mosh-server detached, pid = N]` before it exits.
    static func serverPID(_ stderr: Data) -> Int32? {
        for line in lines(stderr) {
            guard let match = line.firstMatch(of: /\[mosh-server detached, pid = (\d+)\]/),
                  let pid = Int32(match.1), pid > 1 else { continue }
            return pid
        }
        return nil
    }

    /// Stops the server a failed start left behind. It checks the process is still a
    /// mosh-server first, so a recycled pid is never signalled.
    public static func stopCommand(pid: Int32) -> String {
        shellCommand("case \"$(ps -p \(pid) -o comm= 2>/dev/null)\" in *mosh-server*) kill \(pid) ;; esac; exit 0")
    }

    private static func lines(_ data: Data) -> [String] {
        String(decoding: data, as: UTF8.self)
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map { $0.hasSuffix("\r") ? String($0.dropLast()) : String($0) }
    }

    /// POSIX single quotes, with `'` written as `'"'"'` and backslash or newline taken
    /// from the variables `characterVariable` defines.
    private static func quoteBackslashFree(_ element: String) -> String {
        var out = "'"
        for scalar in element.unicodeScalars {
            switch scalar {
            case "'": out += #"'"'"'"#
            case "\n": out += #"'"$togo_nl"'"#
            case "\\": out += #"'"$togo_bs"'"#
            default: out.unicodeScalars.append(scalar)
            }
        }
        return out + "'"
    }

    /// Command substitution strips trailing newlines, so a sentinel `x` follows the
    /// character and is removed afterwards.
    private static func characterVariable(_ name: String, code: Int) -> String {
        "\(name)=$(awk 'BEGIN { printf \"%c%c\", \(code), 120 }'); \(name)=${\(name)%x}"
    }
}
