import Observation
import SwiftUI
import TogoKit

/// Which machines have a newer helper, and the update itself. A machine is asked at most once a
/// day, on connecting, by running the helper's own `update --check` over its SSH connection; the
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
    }

    private(set) var ledger: HelperUpdateLedger
    private(set) var phases: [HostID: Phase] = [:]
    @ObservationIgnored private var checking: Set<HostID> = []
    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let helperPath: (HostID) -> String?
    @ObservationIgnored private let run: Run?
    @ObservationIgnored private let now: () -> Date
    private static let key = "helperUpdates.v1"
    private static let helperTimeout = Duration.seconds(120)

    /// `run` executes a command on the machine and gives its output; nil where there is no machine.
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

    /// Asks the machine, if it has not been asked today. Never throws: a failure is just no news.
    func checkIfDue(_ host: HostID) async {
        guard let run, ledger.isDue(host, now: now()), checking.insert(host).inserted else { return }
        defer { checking.remove(host) }
        let path = helperPath(host)
        var check: HelperUpdateCheck?
        var viaHomebrew = false
        if let output = try? await run(host, HelperUpdate.checkCommand(helperPath: path), Self.helperTimeout) {
            check = HelperUpdate.parseCheck(stdout: text(output.stdout), stderr: text(output.stderr), status: output.status)
            if check == .homebrew {
                viaHomebrew = true
                check = nil
                if let brew = try? await run(host, HelperUpdate.brewCheckCommand(helperPath: path), HelperUpdate.brewTimeout) {
                    check = HelperUpdate.parseBrewOutdated(stdout: text(brew.stdout))
                }
            }
        }
        ledger.record(check, for: host, at: now(), viaHomebrew: viaHomebrew)
        save()
    }

    /// Runs `togo update` on the machine. The helper restarts its own daemon, so the connection
    /// may drop; the app reconnects on its own. A helper that refuses because Homebrew owns it
    /// gets the Homebrew steps run for it.
    func update(_ host: HostID) async {
        guard let run, phase(host) != .updating else { return }
        phases[host] = .updating
        let path = helperPath(host)
        do {
            var output = try await run(host, HelperUpdate.updateCommand(helperPath: path), Self.helperTimeout)
            var outcome = HelperUpdate.parseUpdate(stdout: text(output.stdout), stderr: text(output.stderr), status: output.status)
            if outcome == .homebrew {
                output = try await run(host, HelperUpdate.brewUpgradeCommand(helperPath: path), HelperUpdate.brewTimeout)
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
            phases[host] = .failed(error.localizedDescription)
        }
    }

    private func text(_ data: Data) -> String { String(decoding: data, as: UTF8.self) }

    /// Puts a finished update or failure away, so its line leaves the Control Center.
    func settle(_ host: HostID) {
        if phase(host) != .updating { phases[host] = nil }
    }

    #if DEBUG
    /// Shows a phase without running anything, for previews and snapshots.
    func show(_ phase: Phase, for host: HostID) { phases[host] = phase }
    #endif

    func forget(_ host: HostID) {
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
    static var tryAgain: String { settingsText("helperUpdate.tryAgain", "Try again") }
}
