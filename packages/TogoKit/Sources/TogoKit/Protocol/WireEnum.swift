/// A string enum the helper sends. A value this client does not know decodes to
/// `.unknown(value)` and encodes back unchanged, so a newer helper adding a value
/// never fails the message that carries it.
public protocol WireEnum: Codable, Hashable, Sendable {
    static var known: [Self] { get }
    static func unknown(_ value: String) -> Self
    var rawValue: String { get }
}

extension WireEnum {
    public init(rawValue: String) {
        self = Self.known.first { $0.rawValue == rawValue } ?? .unknown(rawValue)
    }

    public init(from decoder: any Decoder) throws {
        self.init(rawValue: try decoder.singleValueContainer().decode(String.self))
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }
}
