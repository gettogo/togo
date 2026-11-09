#if DEBUG
import Foundation
import TogoKit

/// Scripted onboarding services for previews and tests. Each connect returns the next
/// outcome in `outcomes`; with `newKey` set, the first connect asks about that key first.
@MainActor final class FakeOnboardingServices: OnboardingServices {
    var events: [DiscoveryEvent]
    var outcomes: [ConnectOutcome]
    var newKey: HostKeyPrompt?
    private(set) var connected: [HostProfile] = []
    private(set) var trustAnswers: [Bool] = []
    /// The answers sent with each connect, nil when there were none.
    private(set) var answered: [[String]?] = []
    private(set) var forgotten: [String] = []
    private(set) var savedPasswords: [SavedPassword] = []
    var keyLabels: [String: String] = [:]

    init(events: [DiscoveryEvent] = [.hosts(OnboardingSamples.hosts)], outcomes: [ConnectOutcome] = [], newKey: HostKeyPrompt? = nil) {
        self.events = events
        self.outcomes = outcomes
        self.newKey = newKey
    }

    /// The open discovery stream, so a test can send events after the scripted ones.
    private(set) var discoveryFeed: AsyncStream<DiscoveryEvent>.Continuation?

    func discover() -> AsyncStream<DiscoveryEvent> {
        let (stream, continuation) = AsyncStream.makeStream(of: DiscoveryEvent.self)
        for event in events { continuation.yield(event) }
        discoveryFeed = continuation
        return stream
    }

    func deviceKey() async throws -> KeyInfo {
        OnboardingSamples.deviceKey
    }

    func importKey(openSSH: String, passphrase: String?, label: String) async throws -> ImportedKey {
        ImportedKey(id: "imported-1", label: label)
    }

    func connect(
        _ profile: HostProfile, password: String?, answers: [String]?,
        trustNewKey: @escaping @Sendable (HostKeyPrompt) async -> Bool
    ) async -> ConnectOutcome {
        connected.append(profile)
        answered.append(answers)
        if let prompt = newKey {
            newKey = nil
            let trusted = await trustNewKey(prompt)
            trustAnswers.append(trusted)
            if !trusted { return .cancelled }
        }
        return outcomes.isEmpty ? .unreachable : outcomes.removeFirst()
    }

    func forgetHostKey(_ hostID: String) async throws {
        forgotten.append(hostID)
    }

    func setSavedPassword(_ password: String?, for machine: UUID) throws {
        savedPasswords.append(SavedPassword(machine: machine, password: password))
    }

    func keyLabel(id: String) -> String? {
        keyLabels[id]
    }
}

/// One change to a machine's saved password; nil removed it.
struct SavedPassword: Equatable {
    var machine: UUID
    var password: String?
}

enum OnboardingSamples {
    /// A server that accepted the key and wants a one-time code, as OpenSSH's PAM driver asks.
    static let codePrompt = AuthPrompt.keyboardInteractive(
        name: "", instruction: "Duo two-factor login", prompts: [KIPrompt(text: "Verification code: ", echo: false)]
    )
    static let codeRequest = SignInRequest(codePrompt, machine: "MacBook")!

    static let hosts = [
        DiscoveredHost(id: "a", name: "Sam's MacBook Pro", host: "192.168.1.20", port: 22, tailnet: false),
        DiscoveredHost(id: "b", name: "build", host: "100.101.102.103", port: 22, tailnet: true),
        DiscoveredHost(id: "c", name: "studio", host: "192.168.1.31", port: 2222, tailnet: false),
    ]

    static let deviceKey = KeyInfo(
        id: "device-1", label: "Togo on this phone", kind: .secureEnclaveP256,
        publicKeyLine: "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBEm3d6wQ4Yp1rBq0Zr2iV8fQe0w9yX7Lk3m5JtNcPaUe9oWb1sD4hG6fK2jR8tYvXa0cE7nM1qL5zH3uB9xT4= togo@iphone",
        fingerprint: "SHA256:3kZq9mVb1yX7pLw2N8cR4tF6hJ0sD5gA1eU9oK3iQ2M", createdAt: Samples.now
    )

    static let hostKey = HostKeyPrompt(keyType: "ssh-ed25519", fingerprint: "SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s")

    static let changedKey = ChangedHostKey(
        hostID: "[192.168.1.20]:22",
        trusted: ["SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s"],
        presented: "SHA256:Xb2e8Lq0H4yT6mWv9dC1sN7rK3pZ5fJ0aG8uE2iO4Rk"
    )

    static let ready = HelperReport(
        helper: .present(version: "0.3.0"), path: "/Users/sam/.togo/bin/togo", tmux: "tmux 3.5a", mosh: true, os: "Darwin"
    )

    static let noHelper = HelperReport(helper: .missing, tmux: "tmux 3.5a", mosh: false, os: "Darwin")

    static var draft: MachineDraft {
        var draft = MachineDraft(discovered: hosts[0])
        draft.user = "sam"
        return draft
    }
}
#endif
