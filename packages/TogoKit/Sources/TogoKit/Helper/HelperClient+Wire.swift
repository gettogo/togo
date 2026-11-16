import Foundation

extension HelperClient {
    struct OutgoingFrame: Sendable {
        let id: Int
        let bytes: Data
    }

    struct PendingCall {
        let method: String
        let continuation: CheckedContinuation<Data, any Error>
        var timer: Task<Void, Never>
    }

    /// The routing fields of an incoming message. `result` is decoded later into the type
    /// the waiting call expects, so a large capture is parsed once, not via `JSONValue`.
    struct Envelope: Decodable {
        var id: RPCID?
        var method: String?
        var error: RPCErrorObject?
    }

    struct EventNotification: Decodable {
        var params: EventEnvelope
    }

    struct SnapshotSeq: Decodable {
        var seq: Int
    }

    struct ResultEnvelope<R: Decodable>: Decodable {
        var result: R
    }
}
