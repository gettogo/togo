import Foundation
import Testing
import TogoKit

struct FrameTests {
    @Test func roundTripSmallAndMax() throws {
        let small = Data("{}".utf8)
        let max = Data(repeating: 0x61, count: Frame.maxSize)
        var decoder = FrameDecoder()
        let encoded = try Frame.encode(small) + Frame.encode(max)
        #expect(try decoder.append(encoded) == [small, max])
    }

    @Test func encodeWritesBigEndianLengthHeader() throws {
        let encoded = try Frame.encode(Data("{}".utf8))
        #expect(Array(encoded) == [0, 0, 0, 2, 0x7B, 0x7D])
    }

    @Test func encodeTooLargeThrows() {
        #expect(throws: FrameError.tooLarge(1_048_577)) {
            try Frame.encode(Data(count: Frame.maxSize + 1))
        }
    }

    @Test func decoderSplitsAcrossChunks() throws {
        let first = Data(#"{"id":1}"#.utf8)
        let second = Data(#"{"id":2}"#.utf8)
        let stream = try Frame.encode(first) + Frame.encode(second)
        var decoder = FrameDecoder()
        var payloads: [Data] = []
        for byte in stream {
            payloads += try decoder.append(Data([byte]))
        }
        #expect(payloads == [first, second])
    }

    @Test func decoderAcceptsEmptyPayload() throws {
        var decoder = FrameDecoder()
        #expect(try decoder.append(Data([0, 0, 0, 0])) == [Data()])
    }

    @Test func decoderRejectsHugeHeader() {
        var decoder = FrameDecoder()
        #expect(throws: FrameError.tooLarge(1_048_577)) {
            try decoder.append(Data([0x00, 0x10, 0x00, 0x01]))
        }
    }

    @Test func decoderRejectsMaximumUInt32Header() {
        var decoder = FrameDecoder()
        #expect(throws: FrameError.tooLarge(4_294_967_295)) {
            try decoder.append(Data([0xFF, 0xFF, 0xFF, 0xFF]))
        }
    }
}
