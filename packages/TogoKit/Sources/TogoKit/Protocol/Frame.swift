import Foundation

/// Protocol v1 framing: a 4-byte big-endian unsigned length, then that many bytes of JSON.
public enum Frame {
    public static let maxSize = 1_048_576
    static let headerSize = 4

    public static func encode(_ payload: Data) throws -> Data {
        guard payload.count <= maxSize else { throw FrameError.tooLarge(payload.count) }
        let length = UInt32(payload.count)
        var frame = Data(capacity: headerSize + payload.count)
        frame.append(contentsOf: [
            UInt8(truncatingIfNeeded: length >> 24),
            UInt8(truncatingIfNeeded: length >> 16),
            UInt8(truncatingIfNeeded: length >> 8),
            UInt8(truncatingIfNeeded: length),
        ])
        frame.append(payload)
        return frame
    }
}

public enum FrameError: Error, Equatable {
    case tooLarge(Int)
}

/// Reassembles frames from arbitrarily split input.
///
/// A header above `Frame.maxSize` is rejected as soon as its four bytes arrive, before
/// any payload is buffered, so a hostile peer cannot make the decoder reserve 4 GiB.
public struct FrameDecoder: Sendable {
    private var buffer = Data()

    public init() {}

    public mutating func append(_ bytes: Data) throws -> [Data] {
        buffer.append(bytes)
        var payloads: [Data] = []
        var offset = buffer.startIndex
        while buffer.endIndex - offset >= Frame.headerSize {
            let length = buffer[offset..<offset + Frame.headerSize].reduce(0) { $0 << 8 | Int($1) }
            guard length <= Frame.maxSize else { throw FrameError.tooLarge(length) }
            let payloadStart = offset + Frame.headerSize
            guard buffer.endIndex - payloadStart >= length else { break }
            payloads.append(Data(buffer[payloadStart..<payloadStart + length]))
            offset = payloadStart + length
        }
        // Compacting once per call keeps the work linear in the bytes received.
        if offset != buffer.startIndex {
            buffer = Data(buffer[offset...])
        }
        return payloads
    }
}
