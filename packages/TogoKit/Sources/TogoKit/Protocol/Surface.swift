import Foundation

/// The exact vocabulary supported by the visible renderer, negotiated per lease.
public struct SurfaceOptions: Codable, Hashable, Sendable {
    public var kinds: [String]
    public var cols: Int
    public var dark: Bool
    public var reduceMotion: Bool

    public init(kinds: [String], cols: Int, dark: Bool, reduceMotion: Bool) {
        self.kinds = kinds
        self.cols = cols
        self.dark = dark
        self.reduceMotion = reduceMotion
    }
}

/// A renderer lease belongs to the HelperClient connection that opened it.
public struct SurfaceLease: Codable, Hashable, Sendable {
    public var id: String
    public var token: String
    public var generation: String
}

/// One immutable snapshot chunk. Receiving a chunk never acknowledges display.
public struct SurfaceReadResult: Codable, Sendable {
    public var generation: String
    public var revision: UInt64
    public var snapshot: String?
    public var offset: Int
    public var nextOffset: Int
    public var total: Int
    public var data: Data
    public var unchanged: Bool
    public var pending: Bool

    private enum CodingKeys: String, CodingKey {
        case generation, revision, snapshot, offset, nextOffset, total, data, unchanged, pending
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        generation = try c.decode(String.self, forKey: .generation)
        revision = try c.decode(UInt64.self, forKey: .revision)
        snapshot = try c.decodeIfPresent(String.self, forKey: .snapshot)
        offset = try c.decode(Int.self, forKey: .offset)
        nextOffset = try c.decode(Int.self, forKey: .nextOffset)
        total = try c.decode(Int.self, forKey: .total)
        unchanged = try c.decode(Bool.self, forKey: .unchanged)
        pending = try c.decode(Bool.self, forKey: .pending)
        let encoded = try c.decodeIfPresent(String.self, forKey: .data) ?? ""
        guard encoded.utf8.count <= 87_382,
              encoded.utf8.allSatisfy({ byte in
                  (65...90).contains(byte) || (97...122).contains(byte) || (48...57).contains(byte) || byte == 45 || byte == 95
              }), let decoded = Base64URL.decode(encoded), decoded.count <= 65_536 else {
            throw DecodingError.dataCorruptedError(forKey: .data, in: c, debugDescription: "invalid surface chunk")
        }
        data = decoded
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(generation, forKey: .generation)
        try c.encode(revision, forKey: .revision)
        try c.encodeIfPresent(snapshot, forKey: .snapshot)
        try c.encode(offset, forKey: .offset)
        try c.encode(nextOffset, forKey: .nextOffset)
        try c.encode(total, forKey: .total)
        if !data.isEmpty { try c.encode(Base64URL.encode(data), forKey: .data) }
        try c.encode(unchanged, forKey: .unchanged)
        try c.encode(pending, forKey: .pending)
    }
}

/// Transport acceptance alone; application is confirmed by a subsequent frame.
public struct SurfaceDispatchResult: Codable, Sendable {
    public var dispatched: Bool
}

public enum SurfaceProtocolError: Error, Equatable, Sendable {
    case invalidSnapshot
    case unsupportedContent
}
