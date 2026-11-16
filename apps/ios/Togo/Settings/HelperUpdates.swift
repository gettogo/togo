import Observation
import SwiftUI
import TogoKit

/// The SSH exec was attempted and its result was lost. Connection/authentication failures
/// before that point remain ordinary errors, so checking cannot prompt for login a second time.
struct HelperUpdateCommandInterrupted: Error {
    let cause: any Error
    init(_ cause: any Error) { self.cause = cause }
}

/// Which machines have a newer helper, and the update itself. A machine is asked at most once a
/// day, on connecting, by running the helper's own `update --check` over its SSH connection; an
/// uncertain installation is checked again on reconnect or an explicit Check Update. The
/// machine reaches the release server, this device never does. What it learned is kept per
/// machine on this device. An answer it cannot read shows nothing and is tried again tomorrow.
///
/// A helper before 1.0.2 refuses both commands on a Homebrew install. The app then runs the
/// Homebrew steps itself over the same connection, so every machine gets the same Update button.
@MainActor @Observable final class HelperUpdates {
    typealias Output = (stdout: Data, stderr: Data, status: Int32)
    typealias Run = @Sendable (HostID, String, Duration) async throws -> Output

    enum Phase: Equatable {
        case idle
        case updating
        case updated(String?)
        case failed(String)
        case unconfirmed(String)
    }

    private(set) var ledger: HelperUpdateLedger
    private(set) var phases: [HostID: Phase] = [:]
    private var active: [HostID: UUID] = [:]
    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let helperPath: (HostID) -> String?
    @ObservationIgnored private let run: Run?
    @ObservationIgnored private let now: () -> Date
    private static let key = "helperUpdates.v1"
    private static let helperTimeout = Duration.seconds(120)

    /// `run` gives the command's output, or wraps a possibly started exec's error in
    /// `HelperUpdateCommandInterrupted`. Errors before the exec are not wrapped.
    init(
        defaults: UserDefaults = .standard, helperPath: @escaping (HostID) -> String? = { _ in nil },
        now: @escaping () -> Date = Date.init, run: Run? = nil
    ) {
        self.defaults = defaults
        self.helperPath = helperPath
        self.now = now
        self.run = run
        ledger = defaults.data(forKey: Self.key).flatMap { try? JSONDecoder().decode(HelperUpdateLedger.self, from: $0) }
            ?? HelperUpdateLedger()
    }

    /// The release on offer for the machine, given the version it reports now.
    func available(_ host: HostID, current: String? = nil) -> String? {
        ledger.available(for: host, current: current)
    }

    func state(_ host: HostID) -> HelperUpdateState? { ledger.machines[host] }

    func phase(_ host: HostID) -> Phase { phases[host] ?? .idle }
    func isChecking(_ host: HostID) -> Bool { active[host] != nil }
    func requiresCheck(_ host: HostID) -> Bool {
        if case .unconfirmed = phase(host) { return true }
        return false
    }

    /// Asks once a day, or again to resolve an uncertain installation. Never retries an update.
    func checkIfDue(_ host: HostID) async {
        guard run != nil, active[host] == nil,
              requiresCheck(host) || ledger.isDue(host, now: now()) else { return }
        let token = UUID()
        active[host] = token
        defer { if active[host] == token { active[host] = nil } }
        await check(host, token: token)
    }

    private func check(_ host: HostID, token: UUID) async {
        guard let run else { return }
        let recovering = requiresCheck(host)
        let path = helperPath(host)
        var check: HelperUpdateCheck?
        var viaHomebrew = false
        if let output = try? await run(host, HelperUpdate.checkCommand(helperPath: path), Self.helperTimeout) {
            guard active[host] == token, !Task.isCancelled else { return }
            check = HelperUpdate.parseCheck(stdout: text(output.stdout), stderr: text(output.stderr), status: output.status)
            if check == .homebrew {
                viaHomebrew = true
                check = nil
                if let brew = try? await run(host, HelperUpdate.brewCheckCommand(helperPath: path), HelperUpdate.brewTimeout) {
                    check = HelperUpdate.parseBrewOutdated(stdout: text(brew.stdout))
                }
            }
        }
        guard active[host] == token, !Task.isCancelled else { return }
        // A failed read cannot turn an uncertain installation into either success or failure.
        if recovering, check == nil { return }
        let previous = ledger.machines[host]?.installed
        ledger.record(check, for: host, at: now(), viaHomebrew: viaHomebrew)
        if recovering {
            switch check {
            case .upToDate(let installed):
                let advanced = previous.map { HelperUpdate.isNewer(installed, than: $0) } ?? false
                phases[host] = advanced ? .updated(installed) : .idle
            case .available: phases[host] = .idle
            default: break
            }
        }
        save()
    }

    /// Runs `togo update` on the machine. The helper restarts its own daemon, so the connection
    /// may drop; the app reconnects on its own. A helper that refuses because Homebrew owns it
    /// gets the Homebrew steps run for it.
    func update(_ host: HostID) async {
        if requiresCheck(host) { await checkIfDue(host); return }
        guard let run, phase(host) != .updating, active[host] == nil else { return }
        let token = UUID()
        active[host] = token
        defer { if active[host] == token { active[host] = nil } }
        phases[host] = .updating
        let path = helperPath(host)
        do {
            var output = try await run(host, HelperUpdate.updateCommand(helperPath: path), Self.helperTimeout)
            guard active[host] == token else { return }
            var outcome = HelperUpdate.parseUpdate(stdout: text(output.stdout), stderr: text(output.stderr), status: output.status)
            if outcome == .homebrew {
                output = try await run(host, HelperUpdate.brewUpgradeCommand(helperPath: path), HelperUpdate.brewTimeout)
                guard active[host] == token else { return }
                outcome = HelperUpdate.parseBrewUpgrade(stdout: text(output.stdout), stderr: text(output.stderr), status: output.status)
            }
            switch outcome {
            case .updated(let version):
                ledger.updated(host, to: version, at: now())
                phases[host] = .updated(version)
            case .upToDate, .homebrew:
                ledger.updated(host, to: nil, at: now())
                phases[host] = .idle
            case .failed(let message):
                phases[host] = .failed(message)
            }
            save()
        } catch {
            guard active[host] == token else { return }
            guard let interrupted = error as? HelperUpdateCommandInterrupted else {
                phases[host] = .failed(ServiceErrors.message(error))
                return
            }
            phases[host] = .unconfirmed(ServiceErrors.message(interrupted.cause))
            if !Task.isCancelled { await check(host, token: token) }
        }
    }

    private func text(_ data: Data) -> String { String(decoding: data, as: UTF8.self) }

    /// Puts a finished update or failure away, so its line leaves the Control Center.
    func settle(_ host: HostID) {
        guard phase(host) != .updating, !requiresCheck(host) else { return }
        phases[host] = nil
    }

    #if DEBUG
    /// Shows a phase without running anything, for previews and snapshots.
    func show(_ phase: Phase, for host: HostID) { phases[host] = phase }
    #endif

    func forget(_ host: HostID) {
        active[host] = nil
        ledger.forget(host)
        phases[host] = nil
        save()
    }

    private func save() {
        defaults.set(try? JSONEncoder().encode(ledger), forKey: Self.key)
    }
}

extension EnvironmentValues {
    /// Nil where there is no machine to ask: previews and the demo.
    @Entry var helperUpdates: HelperUpdates?
}

/// The words for the update row and note. The Control Center note is short on purpose: it sits
/// beside a machine's name, which may be long.
enum HelperUpdateCopy {
    /// Short on purpose: it sits under the machine's name, which may be long.
    static var available: String { settingsText("helperUpdate.note", "Helper update available") }
    static func availableOn(_ machine: String) -> String {
        settingsText("helperUpdate.noteOn", "Helper update available on \(machine)")
    }
    static var update: String { settingsText("helperUpdate.update", "Update") }
    static func updateOn(_ machine: String) -> String { settingsText("helperUpdate.updateOn", "Update the helper on \(machine)") }
    static var updating: String { settingsText("helperUpdate.updating", "Installing the helper update…") }
    static func updated(_ version: String?) -> String {
        version.map { settingsText("helperUpdate.updatedTo", "Helper updated to \($0)") }
            ?? settingsText("helperUpdate.updated", "Helper updated")
    }
    static var failed: String { settingsText("helperUpdate.failed", "The helper didn't update.") }
    static var unconfirmed: String { settingsText("helperUpdate.unconfirmed", "Couldn't confirm the helper update. It may have completed.") }
    static var check: String { settingsText("helperUpdate.check", "Check Update") }
    static var checking: String { settingsText("helperUpdate.checking", "Checking…") }
    static var tryAgain: String { settingsText("helperUpdate.tryAgain", "Try again") }
}
