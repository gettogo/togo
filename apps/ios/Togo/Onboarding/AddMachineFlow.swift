import Foundation
import Observation
import TogoKit

/// A host key seen for the first time, waiting for the user to compare it.
struct HostKeyPrompt: Equatable, Sendable {
    var keyType: String
    var fingerprint: String
}

/// A host key that differs from the one trusted for `hostID`.
struct ChangedHostKey: Equatable, Sendable {
    var hostID: String
    var trusted: [String]
    var presented: String
}

enum DiscoveryEvent: Equatable, Sendable {
    case hosts([DiscoveredHost])
    case denied
}

enum ConnectOutcome: Equatable, Sendable {
    case ready(HelperReport)
    case hostKeyChanged(ChangedHostKey)
    case authFailed
    case cancelled
    case unreachable
    case failed(String)
    /// The machine accepted the key or password and asked for more, such as a one-time code.
    case needsAnswer(SignInRequest)
}

/// The side effects adding a machine needs. The flow never holds an SSH connection itself.
@MainActor protocol OnboardingServices: AnyObject {
    /// Single consumer: `AddMachineFlow.discover()`.
    func discover() -> AsyncStream<DiscoveryEvent>
    func deviceKey() async throws -> KeyInfo
    func importKey(openSSH: String, passphrase: String?, label: String) async throws -> ImportedKey
    /// Connects, probes the machine and disconnects. `trustNewKey` is asked before a first key
    /// is stored; a changed key is never trusted here.
    /// `answers` replies to the machine's next question, from the user's last `needsAnswer`.
    func connect(
        _ profile: HostProfile, password: String?, answers: [String]?,
        trustNewKey: @escaping @Sendable (HostKeyPrompt) async -> Bool
    ) async -> ConnectOutcome
    func forgetHostKey(_ hostID: String) async throws
    /// Keeps `password` for the machine in this device's keychain, or removes it when nil.
    func setSavedPassword(_ password: String?, for machine: UUID) throws
    /// The label of a key in this device's keychain, or nil when the key is not here.
    func keyLabel(id: String) -> String?
}

enum DiscoveryPhase: Equatable, Sendable {
    case searching
    /// Nothing answered for a while. Browsing goes on, so a machine that wakes still appears.
    case quiet
    case denied
}

enum ConnectPhase: Equatable, Sendable {
    case connecting
    case hostKeyNew(HostKeyPrompt)
    case hostKeyChanged(ChangedHostKey)
    case keyNotAuthorised(publicKey: String)
    case helper(HelperReport)
    case unreachable
    case failed(String)
    /// `error` says the last answer did not work.
    case signIn(SignInRequest, error: String?)

    var isQuestion: Bool {
        if case .hostKeyNew = self { true } else { false }
    }

    var isReady: Bool {
        if case .helper = self { true } else { false }
    }

    var isFailure: Bool {
        switch self {
        case .unreachable, .failed: true
        default: false
        }
    }
}

/// Discovery, the machine form, the first connection and the helper check, in that order.
@MainActor @Observable final class AddMachineFlow {
    enum Route: Hashable, Sendable {
        case form, connect
    }

    static let quietAfter: Duration = .seconds(6)

    var path: [Route] = []
    var hosts: [DiscoveredHost] = []
    var discovery: DiscoveryPhase = .searching
    var draft = MachineDraft()
    var showsErrors = false
    var phase: ConnectPhase = .connecting
    var importError: String?
    var saveError: String?
    private(set) var attempt = 0
    private(set) var saving = false

    private let services: any OnboardingServices
    private let save: @MainActor (HostRecord) async throws -> Void
    private let finish: @MainActor (HostRecord) -> Void
    private let id: UUID
    private var pendingTrust: CheckedContinuation<Bool, Never>?
    private var profile: HostProfile?
    /// What a successful connection showed about the machine, for the setup checklist.
    var onDetected: (SetupDetection) -> Void = { _ in }
    /// What the user typed for the machine's last question, sent with the next attempt only.
    private var answers: [String]?

    init(
        services: any OnboardingServices, id: UUID = UUID(),
        save: @escaping @MainActor (HostRecord) async throws -> Void, finish: @escaping @MainActor (HostRecord) -> Void
    ) {
        self.services = services
        self.id = id
        self.save = save
        self.finish = finish
    }

    var machineName: String {
        draft.trimmedName.isEmpty ? String(localized: "onboard.machine.fallbackName", defaultValue: "the machine") : draft.trimmedName
    }

    // MARK: Discovery

    func discover() async {
        let quiet = Task { [weak self] in
            try await Task.sleep(for: Self.quietAfter)
            guard let self, self.discovery == .searching, self.hosts.isEmpty else { return }
            self.discovery = .quiet
        }
        defer { quiet.cancel() }
        for await event in services.discover() {
            switch event {
            case .hosts(let found):
                hosts = found
                if discovery == .denied { discovery = .searching }
            case .denied:
                discovery = .denied
            }
        }
    }

    func choose(_ host: DiscoveredHost) {
        draft = MachineDraft(discovered: host)
        showsErrors = false
        path = [.form]
    }

    func enterAddress() {
        draft = MachineDraft()
        showsErrors = false
        path = [.form]
    }

    func importKey(_ text: String, label: String) async {
        importError = nil
        let passphrase = draft.passphrase.isEmpty ? nil : draft.passphrase
        do {
            draft.importedKey = try await services.importKey(openSSH: text, passphrase: passphrase, label: label)
        } catch {
            importError = String(
                localized: "onboard.form.error.import",
                defaultValue: "That file isn't a key Togo can read. If it has a passphrase, type it first."
            )
        }
    }

    // MARK: Connecting

    /// Shows field errors in place when the form is not ready; otherwise moves to the
    /// connection step, which starts the attempt.
    func connect() {
        showsErrors = true
        guard draft.isValid else { return }
        if path.last != .connect { path.append(.connect) }
        retry()
    }

    func retry() {
        phase = .connecting
        attempt += 1
    }

    func answer(_ values: [String]) {
        answers = values
        retry()
    }

    /// Runs one attempt. The connection step calls it from a task tied to `attempt`, so
    /// leaving the step cancels it.
    func runAttempt() async {
        phase = .connecting
        var deviceKey: KeyInfo?
        if draft.auth == .deviceKey {
            do {
                deviceKey = try await services.deviceKey()
            } catch {
                phase = .failed(String(
                    localized: "onboard.connect.noDeviceKey", defaultValue: "Togo couldn't open this phone's key."
                ))
                return
            }
        }
        guard let profile = draft.profile(id: id, deviceKeyID: deviceKey?.id) else { return }
        self.profile = profile
        let password = draft.auth == .password ? draft.password : nil
        let given = answers
        answers = nil
        let outcome = await services.connect(profile, password: password, answers: given) { [weak self] prompt in
            await self?.askTrust(prompt) ?? false
        }
        guard !Task.isCancelled else { return }
        switch outcome {
        case .ready(let report):
            phase = .helper(report)
            onDetected(SetupDetection(keyAccepted: draft.auth != .password, report: report))
        case .hostKeyChanged(let change):
            phase = .hostKeyChanged(change)
        case .needsAnswer(let request):
            phase = .signIn(request, error: given == nil ? nil : Self.answerRefused)
        case .authFailed:
            if let deviceKey {
                phase = .keyNotAuthorised(publicKey: deviceKey.publicKeyLine)
            } else {
                phase = .failed(String(
                    localized: "onboard.connect.authFailed",
                    defaultValue: "\(machineName) did not accept the login. Check the user name and the key or password."
                ))
            }
        case .cancelled:
            if path.last == .connect { path.removeLast() }
        case .unreachable:
            phase = .unreachable
        case .failed(let message):
            phase = .failed(message)
        }
    }

    func answerTrust(_ trusted: Bool) {
        pendingTrust?.resume(returning: trusted)
        pendingTrust = nil
        if trusted { phase = .connecting }
    }

    /// Removes the trusted key so the next attempt asks about the new one, with its
    /// fingerprint on screen. Nothing is trusted without that check.
    func forgetChangedKey() async {
        guard case .hostKeyChanged(let change) = phase else { return }
        do {
            try await services.forgetHostKey(change.hostID)
            retry()
        } catch {
            phase = .failed(error.localizedDescription)
        }
    }

    /// Leaving the flow answers a pending question with no.
    func cancel() {
        answerTrust(false)
    }

    private func askTrust(_ prompt: HostKeyPrompt) async -> Bool {
        pendingTrust?.resume(returning: false)
        phase = .hostKeyNew(prompt)
        return await withCheckedContinuation { pendingTrust = $0 }
    }

    static let answerRefused = String(
        localized: "onboard.signIn.refused", defaultValue: "That answer didn't work. Check it and try again."
    )

    // MARK: Saving

    /// An empty field while editing keeps whatever password is saved; anything else replaces it.
    private func savePassword(for machine: UUID) throws {
        guard draft.auth == .password, draft.savePassword else {
            try services.setSavedPassword(nil, for: machine)
            return
        }
        guard !draft.password.isEmpty else { return }
        try services.setSavedPassword(draft.password, for: machine)
    }

    func save(withHelper: Bool) async {
        guard let profile, !saving else { return }
        var report: HelperReport?
        if case .helper(let found) = phase { report = found }
        let record = HostRecord(
            id: profile.id, profile: profile, symbol: draft.symbol.rawValue,
            helperPath: withHelper ? report?.path : nil, noHelper: !withHelper, wake: draft.wake
        )
        saving = true
        saveError = nil
        defer { saving = false }
        do {
            // Stored before the record, so the machine's first sign-in finds it.
            try savePassword(for: profile.id)
            try await save(record)
            finish(record)
        } catch {
            saveError = String(
                localized: "onboard.save.failed", defaultValue: "Couldn't save \(machineName). Try again."
            )
        }
    }
}
