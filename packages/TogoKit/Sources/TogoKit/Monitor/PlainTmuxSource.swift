import Foundation

/// Plain tmux sessions over SSH, for hosts without the Togo helper.
///
/// There is no event stream, so `HostMonitor` polls `snapshot()` while the app is in the
/// foreground; this source never schedules a poll of its own. Session names come from the
/// host and are untrusted: every remote command passes them as separate argv elements
/// through `shellQuote`, never spliced into a script.
public struct PlainTmuxSource: SessionSource {
    public let events: AsyncStream<EventEnvelope>
    private let link: PlainTmuxLink

    /// `helperFound` is told the helper's path when the machine turns out to have one now, so a
    /// record saved without it can switch to the helper. It runs at most once per connection.
    public init(record: HostRecord, env: SessionEnvironment, helperFound: (@Sendable (String) -> Void)? = nil) {
        self.init(host: record.profile.name, now: { Date() }, helperFound: helperFound) {
            let connection = env.connection(for: record.profile)
            do {
                try await connection.connect()
            } catch {
                await connection.close()
                throw error
            }
            return PlainTmuxDial(
                run: { command in
                    let result = try await connection.run(command, timeout: PlainTmuxLink.commandTimeout)
                    return (result.stdout, result.status)
                },
                hangUp: { await connection.close() }
            )
        }
    }

    /// Runs over a connection that is already signed in. The source owns it from here.
    public init(
        record: HostRecord, connected connection: SSHConnection, helperFound: (@Sendable (String) -> Void)? = nil
    ) {
        self.init(host: record.profile.name, now: { Date() }, helperFound: helperFound) {
            PlainTmuxDial(
                run: { command in
                    let result = try await connection.run(command, timeout: PlainTmuxLink.commandTimeout)
                    return (result.stdout, result.status)
                },
                hangUp: { await connection.close() }
            )
        }
    }

    init(
        host: String, now: @escaping @Sendable () -> Date, helperFound: (@Sendable (String) -> Void)? = nil,
        dial: @escaping @Sendable () async throws -> PlainTmuxDial
    ) {
        // The host has no events.v1, so the stream is empty and already ended.
        let (events, sink) = AsyncStream<EventEnvelope>.makeStream()
        sink.finish()
        self.events = events
        link = PlainTmuxLink(host: host, now: now, helperFound: helperFound, dial: dial)
    }

    public func open() async throws -> (HelperPresence, Set<String>) { try await link.open() }
    public func snapshot() async throws -> EventSnapshot { try await link.snapshot() }
    public func call(_ op: SessionOp) async throws -> Session? { try await link.call(op) }
    public func close() async { await link.close() }
}

public enum PlainTmux {
    /// Sessions Togo creates in this mode carry this prefix, which is what lets kill and
    /// rename refuse sessions the user started themselves.
    public static let ownedPrefix = "togo-"

    private static let field = "\t"
    public static let standardTmuxPaths = ["/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/usr/bin/tmux"]

    /// Finds tmux on the bare PATH of an exec channel, which rarely includes Homebrew.
    static func resolveTmux(candidates: [String] = standardTmuxPaths) -> String {
        #"t=$(command -v tmux 2>/dev/null); if [ -z "$t" ]; then for c in "#
            + candidates.joined(separator: " ")
            + #"; do if [ -x "$c" ]; then t=$c; break; fi; done; fi; "#
    }

    /// One line per session: id, name as hex, created, attached, pane path as hex.
    /// Name and path are the fields a user controls byte for byte, so both are hex-encoded
    /// per session and can never add a line or shift a field; what remains is the tmux id
    /// and two counters. Lines are written with `printf`, never `echo`, which would turn
    /// backslash sequences in remote data back into control characters. `-u` matters:
    /// without a UTF-8 locale, which an exec channel often lacks, tmux prints every tab as
    /// "_". A server that is not running reads as no sessions; exit status 127 means tmux
    /// itself was not found.
    public static let listScript: String = resolveTmux()
        + #"[ -n "$t" ] || exit 127; "$t" -u list-sessions -F "#
        + "'#{session_id}\(field)#{session_created}\(field)#{session_attached}' 2>/dev/null | "
        + "while IFS='\(field)' read -r id rest; do "
        + #"n=$("$t" -u display-message -p -t "$id" '#{session_name}' 2>/dev/null | od -An -v -tx1 | tr -d ' \n'); "#
        + #"p=$("$t" -u display-message -p -t "$id" '#{pane_current_path}' 2>/dev/null | od -An -v -tx1 | tr -d ' \n'); "#
        + #"printf '%s\#(field)%s\#(field)%s\#(field)%s\n' "$id" "$n" "$rest" "$p"; done; true"#

    static let probeScript = resolveTmux() + #"printf %s "$t""#

    static var listCommand: String { shellCommand(listScript) }

    /// Rows that do not have exactly five fields, a session id first and valid hex name and
    /// path, are dropped.
    public static func parse(_ out: String, now: Date) -> [Session] {
        out.split(whereSeparator: \.isNewline).compactMap { line in
            let parts = line.split(separator: Character(field), omittingEmptySubsequences: false)
            guard parts.count == 5, isSessionID(parts[0]),
                  var name = decodeHex(parts[1]), var cwd = decodeHex(parts[4]) else { return nil }
            // display-message ends its output with a newline that is not part of the value.
            if name.hasSuffix("\n") { name.removeLast() }
            if cwd.hasSuffix("\n") { cwd.removeLast() }
            guard !name.isEmpty else { return nil }
            let created = TimeInterval(parts[2]).map { Date(timeIntervalSince1970: $0) } ?? now
            return Session(
                id: id(forName: name), bib: nil, name: name, agent: "shell", cwd: cwd,
                createdAt: created, status: .idle, activity: nil,
                attachedClients: Int(parts[3]) ?? 0, managed: false
            )
        }
    }

    private static func isSessionID(_ text: Substring) -> Bool {
        text.count > 1 && text.hasPrefix("$") && text.dropFirst().allSatisfy { $0.isASCII && $0.isNumber }
    }

    /// Lowercase hex pairs only, and the bytes must be valid UTF-8; anything else is nil.
    static func decodeHex(_ text: some Collection<UInt8>) -> String? {
        let digits = Array(text)
        guard digits.count.isMultiple(of: 2) else { return nil }
        func nibble(_ byte: UInt8) -> UInt8? {
            switch byte {
            case UInt8(ascii: "0") ... UInt8(ascii: "9"): byte - UInt8(ascii: "0")
            case UInt8(ascii: "a") ... UInt8(ascii: "f"): byte - UInt8(ascii: "a") + 10
            default: nil
            }
        }
        var bytes: [UInt8] = []
        for index in stride(from: 0, to: digits.count, by: 2) {
            guard let high = nibble(digits[index]), let low = nibble(digits[index + 1]) else { return nil }
            bytes.append(high << 4 | low)
        }
        return String(validating: bytes, as: UTF8.self)
    }

    private static func decodeHex(_ text: Substring) -> String? { decodeHex(text.utf8) }

    public static func id(forName name: String) -> String {
        "u_" + name.utf8.map { String(format: "%02x", $0) }.joined()
    }

    public static func name(forID id: String) -> String? {
        guard id.hasPrefix("u_") else { return nil }
        return decodeHex(id.utf8.dropFirst(2))
    }

    /// Attaches to the session, or opens a login shell when the host has no tmux.
    /// The name is `$1`, so it reaches tmux as one argument whatever it contains.
    public static func attachArgv(name: String, tmuxCandidates: [String] = standardTmuxPaths) -> [String] {
        script(candidates: tmuxCandidates, #"if [ -n "$t" ]; then exec "$t" attach-session -t "=$1"; fi; exec "${SHELL:-/bin/sh}" -l"#, name)
    }

    /// Creates a detached session. `cwd` is `$2` and only used when not empty.
    public static func newArgv(name: String, cwd: String? = nil) -> [String] {
        script(
            #"if [ -n "$2" ]; then exec "$t" new-session -d -s "$1" -c "$2"; fi; exec "$t" new-session -d -s "$1""#,
            name, cwd ?? ""
        )
    }

    static func killArgv(name: String) -> [String] {
        script(#"exec "$t" kill-session -t "=$1""#, name)
    }

    static func renameArgv(name: String, to newName: String) -> [String] {
        script(#"exec "$t" rename-session -t "=$1" -- "$2""#, name, newName)
    }

    private static func script(candidates: [String] = standardTmuxPaths, _ body: String, _ arguments: String...) -> [String] {
        ["/bin/sh", "-c", resolveTmux(candidates: candidates) + body, "togo"] + arguments
    }

    /// tmux turns "." and ":" in a new session's name into "_".
    static func nameTmuxStores(_ name: String) -> String {
        String(name.map { $0 == "." || $0 == ":" ? "_" : $0 })
    }

    static func nextName(taking taken: Set<String>) -> String {
        var number = 1
        while taken.contains("\(ownedPrefix)\(number)") { number += 1 }
        return "\(ownedPrefix)\(number)"
    }
}

enum PlainTmuxError: Error, Equatable, LocalizedError {
    case noTmux(host: String)
    case foreignSession
    case nameTaken(String)
    case unsupported
    case failed(String)

    var errorDescription: String? {
        switch self {
        case .noTmux(let host): String(localized: "tmux is not installed on \(host); Togo can still open a plain shell.", bundle: .module)
        case .foreignSession: String(localized: "This session was started outside Togo.", bundle: .module)
        case .nameTaken(let name): String(localized: "A session named \(name) already exists.", bundle: .module)
        case .unsupported: String(localized: "This needs the Togo helper on the machine.", bundle: .module)
        case .failed(let message): message
        }
    }
}

struct PlainTmuxDial: Sendable {
    let run: @Sendable (String) async throws -> (stdout: Data, status: Int32)
    let hangUp: @Sendable () async -> Void
}

actor PlainTmuxLink {
    static let commandTimeout: Duration = .seconds(15)

    private let host: String
    private let now: @Sendable () -> Date
    private let helperFound: (@Sendable (String) -> Void)?
    private let dial: @Sendable () async throws -> PlainTmuxDial
    private var connection: PlainTmuxDial?
    private var isClosed = false

    init(
        host: String, now: @escaping @Sendable () -> Date, helperFound: (@Sendable (String) -> Void)?,
        dial: @escaping @Sendable () async throws -> PlainTmuxDial
    ) {
        self.host = host
        self.now = now
        self.helperFound = helperFound
        self.dial = dial
    }

    func open() async throws -> (HelperPresence, Set<String>) {
        let dialed = try await dial()
        guard !isClosed else {
            await dialed.hangUp()
            throw HelperError.closed
        }
        connection = dialed
        let found = try await dialed.run(shellCommand(PlainTmux.probeScript))
        let path = String(decoding: found.stdout, as: UTF8.self)
        await lookForHelper(on: dialed)
        return (path.isEmpty ? .missing : .unknown, [])
    }

    /// A machine saved without the helper may have it by now. Looked at once per connection;
    /// a probe that fails or finds nothing changes nothing.
    private func lookForHelper(on dialed: PlainTmuxDial) async {
        guard let helperFound,
              let result = try? await dialed.run(HelperProbe.command), result.status == 0 else { return }
        // Any helper counts, even an old one: the helper connection then reports its version,
        // so an outdated helper can be updated rather than hidden behind "no helper".
        let report = HelperProbe.parse(String(decoding: result.stdout, as: UTF8.self))
        if report.helper != .missing, let path = report.path { helperFound(path) }
    }

    func snapshot() async throws -> EventSnapshot {
        EventSnapshot(seq: 0, sessions: try await listSessions(), approvals: [])
    }

    func call(_ op: SessionOp) async throws -> Session? {
        switch op {
        case .create(let params): return try await create(params)
        case .kill(let id):
            try await exec(PlainTmux.killArgv(name: try ownedName(id)))
            return nil
        case .rename(let id, let newName):
            let name = try ownedName(id)
            try await exec(PlainTmux.renameArgv(name: name, to: newName))
            return try await listSessions().first { $0.name == newName }
        case .setMute, .answer:
            throw PlainTmuxError.unsupported
        }
    }

    func close() async {
        guard !isClosed else { return }
        isClosed = true
        let connection = self.connection
        self.connection = nil
        await connection?.hangUp()
    }

    private func create(_ params: CreateSessionParams) async throws -> Session? {
        let existing = Set(try await listSessions().map(\.name))
        let requested = params.name.flatMap { $0.isEmpty ? nil : $0 }
        let name = requested ?? PlainTmux.nextName(taking: existing)
        if existing.contains(PlainTmux.nameTmuxStores(name)) { throw PlainTmuxError.nameTaken(name) }
        try await exec(PlainTmux.newArgv(name: name, cwd: params.cwd))
        let stored = PlainTmux.nameTmuxStores(name)
        return try await listSessions().first { $0.name == stored }
    }

    /// A listed name with a backslash is tmux's escaped form of a different real name, so it
    /// cannot be targeted reliably and is refused like a session started outside Togo.
    private func ownedName(_ id: String) throws -> String {
        guard let name = PlainTmux.name(forID: id), name.hasPrefix(PlainTmux.ownedPrefix),
              !name.contains("\\") else {
            throw PlainTmuxError.foreignSession
        }
        return name
    }

    private func listSessions() async throws -> [Session] {
        let result = try await run(PlainTmux.listCommand)
        if result.status == 127 { throw PlainTmuxError.noTmux(host: host) }
        return PlainTmux.parse(String(decoding: result.stdout, as: UTF8.self), now: now())
    }

    private func exec(_ argv: [String]) async throws {
        let result = try await run(shellQuote(argv))
        guard result.status == 0 else {
            if result.status == 127 { throw PlainTmuxError.noTmux(host: host) }
            throw PlainTmuxError.failed(String(localized: "tmux could not complete that on \(host).", bundle: .module))
        }
    }

    /// A dropped connection reads as closed, which is what makes the monitor reconnect.
    private func run(_ command: String) async throws -> (stdout: Data, status: Int32) {
        guard !isClosed, let connection else { throw HelperError.closed }
        do {
            return try await connection.run(command)
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            throw HelperError.closed
        }
    }
}
