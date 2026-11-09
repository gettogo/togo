import CryptoKit
import Foundation

public protocol AnswerSending: Sendable {
    func send(requestId: String, decision: String, pairing: Pairing) async throws
}

public enum AnswerError: Error, Equatable {
    /// The relay path cannot carry this decision; it needs the app with a live connection.
    case notAllowed(String)
    /// The relay answered with this HTTP status.
    case relay(Int)
    case offline
}

/// Signs an answer, seals it for the host and posts it to the host's relay mailbox.
///
/// Only `approve`, `deny` and `option:<n>` leave the device this way, and an option only when
/// `pending` knows the request and the option's role is not `approve_always`. The helper drops
/// anything else, so refusing here keeps the user from waiting on an answer that never lands.
public struct RelayAnswerSender: AnswerSending {
    private let keys: any DeviceKeyStore
    private let mailbox: Mailbox
    private let pending: PendingStore?
    private let clock: @Sendable () -> Date

    public init(
        keys: any DeviceKeyStore, session: URLSession = .shared, pending: PendingStore? = nil,
        clock: @escaping @Sendable () -> Date
    ) {
        self.keys = keys
        mailbox = Mailbox(session: session)
        self.pending = pending
        self.clock = clock
    }

    public func send(requestId: String, decision: String, pairing: Pairing) async throws {
        try check(decision, requestId: requestId)
        let ts = Mailbox.milliseconds(clock())
        let message = Signing.approvalBytes(hostID: pairing.hostId, requestID: requestId, decision: decision, ts: ts)
        let answer = AnswerPlaintext(
            requestId: requestId, decision: decision, ts: ts, deviceId: try keys.deviceID(),
            sig: Base64URL.encode(try keys.signingKey().signature(for: message))
        )
        try await mailbox.post(answer, to: pairing)
    }

    private func check(_ decision: String, requestId: String) throws {
        if decision == "approve" || decision == "deny" { return }
        guard decision.hasPrefix("option:"), let n = Int(decision.dropFirst("option:".count)), n > 0,
              "option:\(n)" == decision,
              let option = pending?.request(requestId)?.options.first(where: { $0.n == n }),
              option.role != "approve_always"
        else { throw AnswerError.notAllowed(decision) }
    }
}

/// Seals new push tokens and the command text choice for the host, so it can reach this device
/// after a token changes and stops sending command text without waiting for SSH.
public struct RelayRegisterSender: Sendable {
    private let keys: any DeviceKeyStore
    private let mailbox: Mailbox
    private let clock: @Sendable () -> Date

    public init(keys: any DeviceKeyStore, session: URLSession = .shared, clock: @escaping @Sendable () -> Date) {
        self.keys = keys
        mailbox = Mailbox(session: session)
        self.clock = clock
    }

    /// `liveActivityEnded` reports the update token of an activity that ended on this device.
    public func send(
        _ tokens: (apns: String?, liveActivity: String?, liveActivityStart: String?), liveActivityCommand: Bool,
        liveActivityEnded: String? = nil, pairing: Pairing
    ) async throws {
        let ts = Mailbox.milliseconds(clock())
        let deviceID = try keys.deviceID()
        let message = Signing.registerBytes(
            hostID: pairing.hostId, deviceID: deviceID, apnsToken: tokens.apns, liveActivityToken: tokens.liveActivity,
            liveActivityStartToken: tokens.liveActivityStart, liveActivityEnded: liveActivityEnded,
            liveActivityCommand: liveActivityCommand, ts: ts
        )
        let register = RegisterPlaintext(
            deviceId: deviceID, apnsToken: tokens.apns, liveActivityToken: tokens.liveActivity,
            liveActivityStartToken: tokens.liveActivityStart, liveActivityEnded: liveActivityEnded,
            liveActivityCommand: liveActivityCommand, ts: ts,
            sig: Base64URL.encode(try keys.signingKey().signature(for: message))
        )
        try await mailbox.post(register, to: pairing)
    }
}

/// `POST <relay>/v1/mailbox/<hostId>` with `{"ciphertext": <envelope>}`; the relay answers 202.
struct Mailbox: Sendable {
    let session: URLSession

    static func milliseconds(_ date: Date) -> Int64 {
        Int64((date.timeIntervalSince1970 * 1000).rounded(.down))
    }

    func post(_ plaintext: some Encodable, to pairing: Pairing) async throws {
        guard let hostKeyData = Base64URL.decode(pairing.hostKey),
              let hostKey = try? Curve25519.KeyAgreement.PublicKey(rawRepresentation: hostKeyData)
        else { throw SealError.badKey }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes]
        let envelope = try Seal.seal(
            to: hostKey, try encoder.encode(plaintext), label: Labels.answer, aad: Data(pairing.hostId.utf8)
        )

        var request = URLRequest(url: pairing.relayURL.appending(path: "v1/mailbox/\(pairing.hostId)"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "content-type")
        request.httpBody = try JSONEncoder().encode(["ciphertext": envelope])

        let response: URLResponse
        do {
            (_, response) = try await session.data(for: request)
        } catch {
            throw AnswerError.offline
        }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 202 else { throw AnswerError.relay(status) }
    }
}
