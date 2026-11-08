import Foundation
import Synchronization
import TogoKit
import TogoTerminal
import UIKit

/// Keeps the diagnostics log fed from what the app already knows: machine reachability, link
/// attempts, helper changes and push outcomes. Every value recorded is typed; machines appear
/// as tokens and errors as code words. Nothing leaves the device unless the person shares it.
final class DiagnosticsRecorder: Sendable {
    /// The log in Application Support. Tests get a folder of their own.
    static let shared = DiagnosticsRecorder(directory: standardDirectory)

    private static var standardDirectory: URL {
        let environment = ProcessInfo.processInfo.environment
        if environment["XCTestConfigurationFilePath"] != nil || environment["XCTestBundlePath"] != nil {
            return FileManager.default.temporaryDirectory.appendingPathComponent("diagnostics-\(ProcessInfo.processInfo.processIdentifier)")
        }
        let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return support.appendingPathComponent("Diagnostics", isDirectory: true)
    }

    private enum Job: Sendable {
        case event(DiagnosticEvent)
        case learn([String])
        case settle(CheckedContinuation<Void, Never>)
    }

    private struct Seen {
        var reach: String?
        var helper: String?
        var capabilities: Set<String>?
    }

    private struct State {
        var seen: [UUID: Seen] = [:]
        var machines: [UUID: MachineFacts] = [:]
        var lastPush: String?
        var lastError: [UUID: String] = [:]
    }

    let log: DiagnosticsLog
    private let jobs: AsyncStream<Job>.Continuation
    private let state = Mutex(State())
    private let now: @Sendable () -> Date

    init(directory: URL, now: @escaping @Sendable () -> Date = { Date() }) {
        log = DiagnosticsLog(directory: directory, salt: DiagnosticsLog.salt(in: directory))
        self.now = now
        let (stream, continuation) = AsyncStream.makeStream(of: Job.self)
        jobs = continuation
        let log = log
        // One consumer, so events land in the order they were recorded.
        Task {
            for await job in stream {
                switch job {
                case .event(let event): await log.record(event)
                case .learn(let names): await log.learn(names)
                case .settle(let done): done.resume()
                }
            }
        }
    }

    deinit { jobs.finish() }

    // MARK: Recording

    func record(_ category: DiagnosticCategory, _ name: String, _ fields: [String: DiagnosticValue] = [:]) {
        jobs.yield(.event(DiagnosticEvent(at: now(), category: category, name: name, fields: fields)))
    }

    /// Returns once everything recorded so far is in the log.
    func settle() async {
        await withCheckedContinuation { jobs.yield(.settle($0)) }
    }

    /// Time from the process starting to the first screen answering touches. Called once, by
    /// `PerfSignposts.interactive()`. A process iOS started ahead of the tap to warm it up
    /// waited for the person, not for the app, so its time is not recorded.
    func recordLaunch(environment: [String: String] = ProcessInfo.processInfo.environment) {
        guard environment["ActivePrewarm"] != "1", let started = Self.processStart else { return }
        record(.perf, "launch", ["ms": .int(Int(now().timeIntervalSince(started) * 1000))])
    }

    /// A burst of terminal output drained: frames it took, parse time per frame and how evenly
    /// the display link ticked meanwhile.
    func frames(_ burst: TerminalBinding.Burst, pacing: FrameStats) {
        func milliseconds(_ duration: Duration) -> Double {
            (Double(duration.components.seconds) * 1_000 + Double(duration.components.attoseconds) / 1e15).roundedTo(2)
        }
        record(.perf, "frames", [
            "frames": .int(burst.frames), "bytes": .int(burst.bytes),
            "work_p95_ms": .double(milliseconds(burst.p95Work)), "work_max_ms": .double(milliseconds(burst.maxWork)),
            "interval_p95_ms": .double((pacing.p95Interval * 1_000).roundedTo(2)),
            "dropped": .double(pacing.droppedFraction.roundedTo(3)),
        ])
    }

    /// Push outcomes are recorded when they change, not on every foreground.
    func push(_ outcome: String, error: (any Error)? = nil) {
        let repeated = state.withLock { state -> Bool in
            defer { state.lastPush = outcome }
            return state.lastPush == outcome && error == nil
        }
        guard !repeated else { return }
        var fields: [String: DiagnosticValue] = [:]
        if let word = DiagnosticValue.word(lowercasing: outcome) { fields["outcome"] = word }
        if let error { fields["error"] = .word(Self.code(for: error)) }
        record(.push, "registration", fields)
    }

    /// A machine's newest snapshot. Reachability, helper version and capabilities are recorded
    /// when they change, and the machine's facts are kept for the report.
    func reached(_ snapshot: HostSnapshot, record machine: HostRecord) {
        jobs.yield(.learn(
            [machine.profile.name, machine.profile.endpoint.host, machine.profile.endpoint.user]
                + snapshot.sessions.flatMap { [$0.name, $0.id] }
        ))
        let transport = machine.profile.transport
        let token = log.token(for: snapshot.hostID)
        let reach = Self.word(of: snapshot.reach)
        let (helper, version) = Self.word(of: snapshot.helper)
        let capabilities = snapshot.capabilities
        let facts = MachineFacts(
            token: token, helperVersion: version, capabilities: capabilities.sorted(), transport: transport.rawValue
        )
        let change = state.withLock { state -> (reach: Bool, helper: Bool, capabilities: Bool) in
            var seen = state.seen[snapshot.hostID] ?? Seen()
            let result = (seen.reach != reach, seen.helper != helper + version, seen.capabilities != capabilities)
            seen.reach = reach
            seen.helper = helper + version
            seen.capabilities = capabilities
            state.seen[snapshot.hostID] = seen
            state.machines[snapshot.hostID] = facts
            return result
        }
        if change.reach {
            record(.machine, "reachability", ["machine": .machine(token), "state": .word(reach)])
        }
        if change.helper {
            var fields: [String: DiagnosticValue] = ["machine": .machine(token), "state": .word(helper)]
            if version != "unknown" { fields["version"] = .word(version) }
            record(.helper, "helper", fields)
        }
        if change.capabilities {
            record(.helper, "capabilities", ["machine": .machine(token), "count": .int(capabilities.count)])
        }
    }

    /// Wraps a terminal's connect closure so each attempt, its transport and how it ended are
    /// recorded. The attempt number counts from one for this terminal.
    func observing(_ connect: @escaping LinkConnect, host: UUID, preference: TransportPreference) -> LinkConnect {
        let token = log.token(for: host)
        let attempts = Mutex(0)
        return { [self] cols, rows, onWaking in
            let attempt = attempts.withLock { count -> Int in
                count += 1
                return count
            }
            let phase: @Sendable (String, [String: DiagnosticValue]) -> Void = { word, extra in
                var fields = extra
                fields["machine"] = .machine(token)
                fields["phase"] = .word(word)
                fields["attempt"] = .int(attempt)
                self.record(.link, "phase", fields)
            }
            phase("connecting", ["transport": .word(preference.rawValue)])
            do {
                let attachment = try await connect(cols, rows) { host in
                    phase("waking", [:])
                    onWaking(host)
                }
                state.withLock { _ = $0.lastError.removeValue(forKey: host) }
                phase("live", ["transport": .word(attachment.kind.rawValue), "fallbacks": .int(attachment.notes.count)])
                return attachment
            } catch {
                let code = Self.code(for: error)
                state.withLock { $0.lastError[host] = code }
                phase(error is CancellationError ? "cancelled" : "failed", ["error": .word(code)])
                throw error
            }
        }
    }

    /// Watches a terminal's link states for what happens after the first connect: roaming,
    /// reconnecting with its attempt number, and the end of the session (once). The first
    /// connect's phases come from `observing`; free-text reasons in the states are ignored.
    func linkStates(host: UUID, preference: TransportPreference) -> @Sendable (LinkState) -> Void {
        struct Seen { var transport = ""; var roaming = false; var ended = false }
        let token = log.token(for: host)
        let seen = Mutex(Seen(transport: preference.rawValue))
        return { [self] linkState in
            var fields: [String: DiagnosticValue] = ["machine": .machine(token)]
            let phase: String
            let transport: String
            switch linkState {
            case .live(let kind, _):
                seen.withLock { $0.transport = kind.rawValue; $0.roaming = false }
                return
            case .roaming:
                let repeated = seen.withLock { seen -> Bool in
                    defer { seen.roaming = true }
                    return seen.roaming
                }
                guard !repeated else { return }
                phase = "roaming"
            case .reconnecting(let attempt):
                seen.withLock { $0.roaming = false }
                phase = "reconnecting"
                fields["attempt"] = .int(attempt)
            case .ended:
                let first = seen.withLock { seen -> Bool in
                    defer { seen.ended = true }
                    return !seen.ended
                }
                guard first else { return }
                phase = "ended"
            case .waking, .connecting, .failed:
                return
            }
            transport = seen.withLock { $0.transport }
            fields["phase"] = .word(phase)
            fields["transport"] = .word(transport)
            if phase != "ended", let code = state.withLock({ $0.lastError[host] }) { fields["error"] = .word(code) }
            record(.link, "phase", fields)
        }
    }

    // MARK: Report

    func clear() async {
        await settle()
        await log.clear()
        state.withLock { $0 = State() }
    }

    func report(settings: AppSettings) async -> DiagnosticsReport {
        await settle()
        let machines = state.withLock { Array($0.machines.values) }.sorted { $0.token.value < $1.token.value }
        return DiagnosticsReport(
            app: Self.appFacts(), settings: DiagnosticsReport.summary(of: settings),
            machines: machines, events: await log.events()
        )
    }

    static func appFacts() -> AppFacts {
        let info = Bundle.main.infoDictionary
        return AppFacts(
            version: info?["CFBundleShortVersionString"] as? String ?? "0", build: info?["CFBundleVersion"] as? String ?? "0",
            osVersion: UIDevice.current.systemVersion, model: modelIdentifier, locale: Locale.current.identifier
        )
    }

    private static var modelIdentifier: String {
        if let simulated = ProcessInfo.processInfo.environment["SIMULATOR_MODEL_IDENTIFIER"] { return simulated }
        var size = 0
        sysctlbyname("hw.machine", nil, &size, nil, 0)
        var bytes = [CChar](repeating: 0, count: size)
        sysctlbyname("hw.machine", &bytes, &size, nil, 0)
        return String(decoding: bytes.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }, as: UTF8.self)
    }

    // MARK: Words

    /// An error as a short code: its domain and number, never its description, which can
    /// carry a host name or a path.
    static func code(for error: any Error) -> String {
        let nsError = error as NSError
        let suffix = ".\(nsError.code)"
        let domain = nsError.domain.lowercased().filter { $0.isLetter || $0 == "." }
        let code = String(domain.suffix(max(1, 40 - suffix.count))) + suffix
        if case .word(let word)? = DiagnosticValue.word(lowercasing: code) { return word }
        return "unknown"
    }

    private static func word(of reach: HostSnapshot.Reach) -> String {
        switch reach {
        case .connecting: "connecting"
        case .waking: "waking"
        case .online: "online"
        case .offline: "offline"
        case .authNeeded: "auth_needed"
        case .failed: "failed"
        case .keyMissing: "key_missing"
        case .hostKeyChanged: "host_key_changed"
        case .deviceOffline: "device_offline"
        }
    }

    private static func word(of helper: HelperPresence) -> (state: String, version: String) {
        switch helper {
        case .unknown: ("unknown", "unknown")
        case .present(let version): ("present", releaseWord(version))
        case .missing: ("missing", "unknown")
        case .outdated(let version): ("outdated", releaseWord(version))
        case .newer: ("newer", "unknown")
        }
    }

    /// A release number such as `0.3.0` or `1.2.0-rc1`. Anything else, which could carry a
    /// host name, is "unknown".
    private static func releaseWord(_ version: String) -> String {
        let shape = #"^\d+(\.\d+){1,2}(-(alpha|beta|rc)\.?\d+)?$"#
        guard version.range(of: shape, options: .regularExpression) != nil else { return "unknown" }
        return version
    }

    /// When this process started, from the kernel.
    private static var processStart: Date? {
        var info = kinfo_proc()
        var size = MemoryLayout<kinfo_proc>.size
        var mib: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_PID, getpid()]
        guard sysctl(&mib, UInt32(mib.count), &info, &size, nil, 0) == 0 else { return nil }
        let start = info.kp_proc.p_un.__p_starttime
        return Date(timeIntervalSince1970: TimeInterval(start.tv_sec) + TimeInterval(start.tv_usec) / 1_000_000)
    }
}

extension PushRegistrar.Authorization {
    var diagnosticWord: String {
        switch self {
        case .notDetermined: "not_determined"
        case .denied: "denied"
        case .provisional: "provisional"
        case .authorized: "authorized"
        }
    }
}

private extension Double {
    func roundedTo(_ places: Int) -> Double {
        let scale = pow(10, Double(places))
        return (self * scale).rounded() / scale
    }
}
