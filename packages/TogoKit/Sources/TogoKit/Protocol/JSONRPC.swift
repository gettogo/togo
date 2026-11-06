import Foundation

public enum RPCID: Codable, Hashable, Sendable {
    case int(Int)
    case string(String)
    case null

    public init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Int.self) {
            self = .int(value)
        } else {
            self = .string(try container.decode(String.self))
        }
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .int(let value): try container.encode(value)
        case .string(let value): try container.encode(value)
        case .null: try container.encodeNil()
        }
    }
}

/// An outgoing call. A nil `id` encodes no `id` key, which makes it a notification.
public struct RPCRequest<P: Encodable & Sendable>: Encodable, Sendable {
    public let jsonrpc = "2.0"
    public var id: Int?
    public var method: String
    public var params: P

    public init(id: Int?, method: String, params: P) {
        self.id = id
        self.method = method
        self.params = params
    }
}

public struct RPCErrorObject: Codable, Error, Equatable, Sendable {
    public var code: Int
    public var message: String
    public var data: JSONValue?

    public init(code: Int, message: String, data: JSONValue? = nil) {
        self.code = code
        self.message = message
        self.data = data
    }

    /// The word the helper names in `data.reason` for a conflict or refusal, nil when absent.
    public var reason: String? {
        if case .object(let fields)? = data, case .string(let reason)? = fields["reason"] { return reason }
        return nil
    }
}

/// Any message from the helper: a response (`id` with `result` or `error`) or a
/// notification (`method`, no `id`). The caller decodes `result` into the type the
/// pending request expects.
public struct RPCIncoming: Decodable, Sendable {
    /// `.null` when the helper could not read the request id (parse error); nil when absent.
    public var id: RPCID?
    public var method: String?
    public var result: JSONValue?
    public var error: RPCErrorObject?

    public init(id: RPCID? = nil, method: String? = nil, result: JSONValue? = nil, error: RPCErrorObject? = nil) {
        self.id = id
        self.method = method
        self.result = result
        self.error = error
    }

    private enum CodingKeys: String, CodingKey {
        case id, method, result, error
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        // decodeIfPresent would fold an explicit null id into "absent".
        id = container.contains(.id) ? try container.decode(RPCID.self, forKey: .id) : nil
        method = try container.decodeIfPresent(String.self, forKey: .method)
        result = try container.decodeIfPresent(JSONValue.self, forKey: .result)
        error = try container.decodeIfPresent(RPCErrorObject.self, forKey: .error)
    }
}

/// Error codes from protocol/v1/schema.json (`$defs/Error`).
public enum RPCErrorCode {
    public static let parse = -32700
    public static let invalidRequest = -32600
    public static let methodNotFound = -32601
    public static let invalidParams = -32602
    public static let `internal` = -32603
    public static let notFound = -32001
    public static let notReady = -32002
    public static let invalidPath = -32003
    public static let tmuxUnavailable = -32004
    public static let unsupportedVersion = -32005
    public static let conflict = -32006
    public static let notManaged = -32007
    public static let refused = -32008
}
