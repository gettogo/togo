import Foundation

/// Confirms display data saved on this device. It is neither an answer nor a token update.
public struct LiveContextPlaintext: Codable, Sendable {
    public var type = "live_context"
    public var deviceId: String
    public var requestId: String
    public var tokenHash: String
    public var ts: Int64
    public var sig: String
}

public struct LiveContextReceiptSender: Sendable {
    private let keys: any DeviceKeyStore
    private let pairings: PairingStore
    private let pending: PendingStore
    private let mailbox: Mailbox
    private let clock: @Sendable () -> Date

    public init(
        keys: any DeviceKeyStore, pairings: PairingStore, pending: PendingStore,
        session: URLSession? = nil, clock: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.keys = keys
        self.pairings = pairings
        self.pending = pending
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 5
        configuration.timeoutIntervalForResource = 5
        mailbox = Mailbox(session: session ?? URLSession(configuration: configuration))
        self.clock = clock
    }

    /// Read back the shared record before acknowledging it: a failed write must not silence
    /// the regular notification. Older helpers never receive this new mailbox message.
    @discardableResult
    public func send(hostID: String, requestID: String) async throws -> Bool {
        let now = clock()
        guard let pairing = pairings.pairing(hostId: hostID), pairing.liveContextReceipts == true,
              let tokenHash = pairing.apnsTokenHash, tokenHash.count == 64,
              let request = pending.request(requestID), request.hostId == hostID,
              !request.isPlaceholder, request.isOpen(at: now),
              now.timeIntervalSince(request.receivedAt) >= -NotificationServiceCore.maxSkew,
              now.timeIntervalSince(request.receivedAt) < RequestLifetime.seconds
        else { return false }
        let ts = Mailbox.milliseconds(now)
        let deviceID = try keys.deviceID()
        let bytes = Signing.liveContextBytes(hostID: hostID, deviceID: deviceID, requestID: requestID, tokenHash: tokenHash, ts: ts)
        let receipt = LiveContextPlaintext(
            deviceId: deviceID, requestId: requestID, tokenHash: tokenHash, ts: ts,
            sig: Base64URL.encode(try keys.signingKey().signature(for: bytes))
        )
        // Below the notification extension's runtime and the helper's 10-second fallback.
        try await mailbox.post(receipt, to: pairing, timeout: 5)
        return true
    }
}
