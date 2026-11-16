import Foundation
import TogoKit

/// Sign-in callbacks ride the SSH connection behind the machine's helper channel. That is the
/// connection the multi-route connector opened (LAN, `.local`, a Tailscale name or 100.x), and
/// it exists whatever the terminal uses, Mosh included, since every helper call runs over it.
/// A callback is a direct-tcpip channel on it to the machine's loopback port: never a TCP
/// connection to the machine's address.
@MainActor final class LiveCallbackTunnel: CallbackTunnel {
    private let helpers: HelperChannels
    private let records: (HostID) -> HostRecord?
    private let review: any ReviewServices

    init(helpers: HelperChannels, records: @escaping (HostID) -> HostRecord?, review: any ReviewServices) {
        self.helpers = helpers
        self.records = records
        self.review = review
    }

    func opener(hostID: HostID, fresh: Bool) async throws -> any TCPChannelOpener {
        guard let record = records(hostID) else { throw HelperError.closed }
        // A dropped connection is replaced, on whichever route answers now.
        if fresh { await helpers.drop(record.id) }
        return try await helpers.connection(for: record)
    }

    func loopbackAddress(hostID: HostID, sessionID: String?, port: Int) async throws -> String {
        guard let sessionID, !sessionID.isEmpty else { throw CallbackTargetError.unverified }
        let subject = ReviewSubject(
            hostID: hostID, sessionID: sessionID, bib: nil, title: "", cwd: "", managed: true, capabilities: []
        )
        // The session's list says which loopback address the agent took, IPv4 or IPv6.
        let ports = try await review.ports(subject)
        return try Self.verifiedAddress(sessionID: sessionID, port: port, ports: ports)
    }

    static func verifiedAddress(sessionID: String?, port: Int, ports: [ListeningPort]) throws -> String {
        guard let sessionID, !sessionID.isEmpty,
              let target = ports.lazy.filter({ $0.port == port }).compactMap(ForwardTarget.init).first else {
            throw CallbackTargetError.unverified
        }
        return target.host
    }
}

enum CallbackTargetError: LocalizedError, Equatable {
    case unverified

    var errorDescription: String? {
        String(localized: "links.callback.unverified", defaultValue:
            "This session has no verified sign-in listener on that port. Sign in on the machine, or paste the code.")
    }
}

/// Types a pasted sign-in code into the agent. When the helper shows the agent's code prompt
/// as a request, the code answers it (pasted off the command line, then Enter); otherwise it
/// goes in as a reply. Either way the session is read back to check Enter took.
@MainActor final class LiveSignInCodeSender: SignInCodeSending {
    private let tools: any MachineTools
    private let answer: (HostID, String, Decision) async throws -> Void
    /// The open request in a session that waits for a code or key, if there is one.
    private let pendingSecret: (HostID, String) -> String?

    init(
        tools: any MachineTools, answer: @escaping (HostID, String, Decision) async throws -> Void,
        pendingSecret: @escaping (HostID, String) -> String?
    ) {
        self.tools = tools
        self.answer = answer
        self.pendingSecret = pendingSecret
    }

    func send(code: String, hostID: HostID, sessionID: String) async -> CodeSendResult {
        do {
            if let request = pendingSecret(hostID, sessionID) {
                try await answer(hostID, request, .text(code))
            } else {
                try await tools.call(hostID) { try await $0.sendText(id: sessionID, text: code, submit: true) }
            }
        } catch {
            return .failed(ServiceErrors.message(error))
        }
        try? await Task.sleep(for: .milliseconds(800))
        guard let screen = try? await tools.call(hostID, { try await $0.capture(id: sessionID, lines: 12, join: true) }) else {
            return .sent
        }
        return SignInCodeCheck.stillTyped(code, screen: screen.text) ? .notSubmitted : .sent
    }
}

/// Reading a session back after a code went in.
enum SignInCodeCheck {
    /// The code still sits on the bottom line, where the agent's input is: Enter didn't take.
    static func stillTyped(_ code: String, screen: String) -> Bool {
        let last = screen.split(whereSeparator: \.isNewline).last { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        guard let last, code.count >= 4 else { return false }
        return last.contains(code)
    }
}
