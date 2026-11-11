import Foundation
import ImageIO
import Testing
import TogoKit
import UIKit
import UniformTypeIdentifiers
@testable import Togo

@MainActor
struct ImageShrinkerTests {
    @Test func shrinksUnderLimit() throws {
        let png = try #require(Fixtures.noise(width: 4_000, height: 3_000).pngData())
        let encoded = try #require(ImageShrinker.fit(png))
        #expect(encoded.data.count <= 700_000)
        #expect(encoded.fileExtension == "jpg")
        #expect(UIImage(data: encoded.data) != nil)
    }

    @Test func smallImageUntouched() throws {
        let png = try #require(Fixtures.noise(width: 64, height: 48).pngData())
        let encoded = try #require(ImageShrinker.fit(png))
        #expect(encoded.fileExtension == "png")
        let image = try #require(UIImage(data: encoded.data))
        #expect(image.size == CGSize(width: 64, height: 48))
    }

    @Test func metadataIsStrippedEvenWhenNoShrinkIsNeeded() throws {
        let jpeg = try Fixtures.jpegWithLocation()
        #expect(Fixtures.properties(of: jpeg)[kCGImagePropertyGPSDictionary] != nil)
        let encoded = try #require(ImageShrinker.fit(jpeg))
        let properties = Fixtures.properties(of: encoded.data)
        #expect(properties[kCGImagePropertyGPSDictionary] == nil)
        #expect((properties[kCGImagePropertyTIFFDictionary] as? [CFString: Any])?[kCGImagePropertyTIFFMake] == nil)
        #expect((properties[kCGImagePropertyExifDictionary] as? [CFString: Any])?[kCGImagePropertyExifUserComment] == nil)
    }

    @Test func hugeImageIsNeverDecodedAtFullSize() throws {
        let png = try Fixtures.flatPNG(width: 12_000, height: 9_000)
        var decoded: [CGSize] = []
        let encoded = try #require(ImageShrinker.fit(png, onDecode: { decoded.append(CGSize(width: $0.width, height: $0.height)) }))
        #expect(encoded.data.count <= 700_000)
        #expect(!decoded.isEmpty)
        #expect(decoded.allSatisfy { max($0.width, $0.height) <= CGFloat(ImageShrinker.largestSide) })

        let preview = try #require(ImageShrinker.preview(png))
        #expect(max(preview.size.width, preview.size.height) * preview.scale <= CGFloat(ImageShrinker.annotateSide))
    }

    @Test func pathPastedAfterUpload() async throws {
        let services = FakeAppServices()
        var pasted: [String] = []
        let flow = ImagePasteFlow(support: .available, upload: { name, data in
            try await services.upload(hostID: Samples.studio, sessionID: "s-04", name: name, data: data)
        }, paste: { pasted.append($0) })
        let png = try #require(Fixtures.noise(width: 64, height: 48).pngData())

        await flow.begin(png)
        #expect(flow.isAnnotating)
        await flow.send(annotated: nil)

        guard case .upload(let host, let session, let name)? = services.calls.last else {
            Issue.record("nothing uploaded")
            return
        }
        #expect(host == Samples.studio && session == "s-04")
        #expect(name.hasSuffix(".png"))
        #expect(pasted == [shellQuote(["/tmp/togo-uploads/\(name)"]) + " "])
        // Said, then gone by itself.
        #expect(flow.phase == .sent)
        #expect(flow.phase.message == "Image sent. Its path is typed in the terminal.")
    }

    @Test func noUploadCapabilityExplains() async throws {
        var uploads = 0
        var pasted: [String] = []
        let flow = ImagePasteFlow(support: .unsupported, upload: { _, _ in
            uploads += 1
            return ""
        }, paste: { pasted.append($0) })

        await flow.begin(try #require(Fixtures.noise(width: 8, height: 8).pngData()))

        #expect(flow.phase == .unsupported)
        #expect(flow.phase.message == "This machine's helper can't receive images yet.")
        #expect(uploads == 0 && pasted.isEmpty)
    }

    @Test func uploadFailureKeepsTheImageForRetry() async throws {
        let services = FakeAppServices()
        services.failure = HelperError.timeout(method: "fs.upload")
        var pasted: [String] = []
        let flow = ImagePasteFlow(support: .available, upload: { name, data in
            try await services.upload(hostID: Samples.studio, sessionID: "s-04", name: name, data: data)
        }, paste: { pasted.append($0) })
        await flow.begin(try #require(Fixtures.noise(width: 8, height: 8).pngData()))
        await flow.send(annotated: nil)
        guard case .failed = flow.phase else {
            Issue.record("expected failure, got \(flow.phase)")
            return
        }

        services.failure = nil
        await flow.retry()
        #expect(pasted.count == 1)
        #expect(flow.phase == .sent)
        #expect(!flow.canRetry)
    }

    @Test func failedPhotoLoadOffersRetry() async throws {
        let png = try #require(Fixtures.noise(width: 8, height: 8).pngData())
        var attempts = 0
        let flow = ImagePasteFlow(support: .available, upload: { _, _ in "" }, paste: { _ in })
        await flow.load {
            attempts += 1
            if attempts == 1 { throw CocoaError(.fileReadUnknown) }
            return png
        }
        #expect(flow.phase == .loadFailed)
        #expect(flow.phase.message == "Couldn't load that photo.")
        await flow.retry()
        #expect(attempts == 2)
        #expect(flow.isAnnotating)
    }

    /// The Attach key opens the picker only where a picture can go; elsewhere it says why.
    @Test func attachExplainsWhereAPictureCannotGo() {
        let none = ImagePasteFlow(support: .none, upload: { _, _ in "" }, paste: { _ in })
        #expect(!none.picksPhotos)
        none.explainUnsupported()
        #expect(none.phase == .noHelper)
        let old = ImagePasteFlow(support: .unsupported, upload: { _, _ in "" }, paste: { _ in })
        #expect(!old.picksPhotos)
        old.explainUnsupported()
        #expect(old.phase == .unsupported)
        let ready = ImagePasteFlow(support: .available, upload: { _, _ in "" }, paste: { _ in })
        #expect(ready.picksPhotos)
        ready.explainUnsupported()
        #expect(ready.phase == .idle)
    }
}

enum Fixtures {
    static func properties(of data: Data) -> [CFString: Any] {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil) else { return [:] }
        return CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any] ?? [:]
    }

    /// A small photo-like JPEG carrying the metadata a phone camera writes.
    static func jpegWithLocation() throws -> Data {
        let image = try #require(noise(width: 120, height: 90).cgImage)
        let out = NSMutableData()
        let destination = try #require(CGImageDestinationCreateWithData(out, UTType.jpeg.identifier as CFString, 1, nil))
        let properties: [CFString: Any] = [
            kCGImagePropertyGPSDictionary: [
                kCGImagePropertyGPSLatitude: 51.5, kCGImagePropertyGPSLatitudeRef: "N",
                kCGImagePropertyGPSLongitude: 0.12, kCGImagePropertyGPSLongitudeRef: "W",
            ],
            kCGImagePropertyTIFFDictionary: [kCGImagePropertyTIFFMake: "Apple", kCGImagePropertyTIFFModel: "iPhone"],
            kCGImagePropertyExifDictionary: [kCGImagePropertyExifUserComment: "at home"],
        ]
        CGImageDestinationAddImage(destination, image, properties as CFDictionary)
        #expect(CGImageDestinationFinalize(destination))
        return out as Data
    }

    /// A single-colour greyscale PNG: tiny on disk, enormous once decoded.
    static func flatPNG(width: Int, height: Int) throws -> Data {
        let context = try #require(CGContext(
            data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
            space: CGColorSpaceCreateDeviceGray(), bitmapInfo: CGImageAlphaInfo.none.rawValue
        ))
        context.setFillColor(gray: 0.5, alpha: 1)
        context.fill(CGRect(x: 0, y: 0, width: width, height: height))
        let image = try #require(context.makeImage())
        let out = NSMutableData()
        let destination = try #require(CGImageDestinationCreateWithData(out, UTType.png.identifier as CFString, 1, nil))
        CGImageDestinationAddImage(destination, image, nil)
        #expect(CGImageDestinationFinalize(destination))
        return out as Data
    }

    /// Random pixels, which JPEG cannot compress well, so size limits are actually exercised.
    static func noise(width: Int, height: Int) -> UIImage {
        var generator = SystemRandomNumberGenerator()
        var bytes = [UInt8](repeating: 255, count: width * height * 4)
        for i in stride(from: 0, to: bytes.count, by: 4) {
            let value = generator.next() as UInt32
            bytes[i] = UInt8(truncatingIfNeeded: value)
            bytes[i + 1] = UInt8(truncatingIfNeeded: value >> 8)
            bytes[i + 2] = UInt8(truncatingIfNeeded: value >> 16)
        }
        let provider = CGDataProvider(data: Data(bytes) as CFData)!
        let image = CGImage(
            width: width, height: height, bitsPerComponent: 8, bitsPerPixel: 32, bytesPerRow: width * 4,
            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGBitmapInfo(rawValue: CGImageAlphaInfo.noneSkipLast.rawValue),
            provider: provider, decode: nil, shouldInterpolate: false, intent: .defaultIntent
        )!
        return UIImage(cgImage: image)
    }
}
