import Foundation
import ImageIO
import TogoKit
import UIKit
import UniformTypeIdentifiers

/// Brings an image under the helper's upload limit, with its metadata removed.
///
/// Every image is decoded through an ImageIO thumbnail, never at full size, with its
/// orientation applied, and re-encoded from those pixels alone. So location, camera and
/// maker notes never leave the phone, even for a file small enough to send as it was.
/// PNG and GIF sources try lossless PNG first so screenshots stay sharp; then JPEG at each
/// quality in turn, halving the long side until it fits.
enum ImageShrinker {
    struct Encoded: Equatable, Sendable {
        var data: Data
        var fileExtension: String
    }

    static let limit = UploadParams.maxBytes
    static let qualities: [CGFloat] = [0.8, 0.6, 0.4]
    /// A 48 MP photo decoded whole is about 190 MB. No agent reads detail past this side,
    /// and a 700 KB JPEG cannot carry it anyway.
    static let largestSide = 4_096
    /// The annotate screen shows and renders at this size; marks need no more.
    static let annotateSide = 2_048
    /// Halving stops here: smaller is no longer worth reading, so the image counts as too large.
    /// An image already this small is still sent.
    static let smallestSide = 64

    /// `onDecode` sees every bitmap decoded, so tests can check none is full size.
    static func fit(_ data: Data, limit: Int = limit, onDecode: (CGImage) -> Void = { _ in }) -> Encoded? {
        guard let source = makeSource(data), let longSide = longSide(of: source) else { return nil }
        var side = min(longSide, largestSide)
        var tryLossless = isLossless(source)
        repeat {
            guard let image = thumbnail(source, maxSide: side) else { return nil }
            onDecode(image)
            if tryLossless, let png = encode(image, as: .png), png.count <= limit {
                return Encoded(data: png, fileExtension: "png")
            }
            tryLossless = false
            for quality in qualities {
                if let jpeg = encode(image, as: .jpeg, quality: quality), jpeg.count <= limit {
                    return Encoded(data: jpeg, fileExtension: "jpg")
                }
            }
            side /= 2
        } while side >= smallestSide
        return nil
    }

    /// An image drawn on the phone, already at most `annotateSide`, through the same steps.
    static func fit(_ image: CGImage, limit: Int = limit) -> Encoded? {
        encode(image, as: .png).flatMap { fit($0, limit: limit) }
    }

    /// A screen-sized, upright copy for the annotate screen.
    static func preview(_ data: Data) -> UIImage? {
        preview(data, maxSide: annotateSide)
    }

    /// An upright copy whose long side is at most `maxSide` pixels, decoded straight at that
    /// size: a thumbnail never holds the full picture in memory.
    static func preview(_ data: Data, maxSide: Int) -> UIImage? {
        guard let source = makeSource(data), let longSide = longSide(of: source),
              let image = thumbnail(source, maxSide: min(longSide, maxSide)) else { return nil }
        return UIImage(cgImage: image)
    }

    private static func makeSource(_ data: Data) -> CGImageSource? {
        CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary)
    }

    private static func longSide(of source: CGImageSource) -> Int? {
        guard let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
              let width = properties[kCGImagePropertyPixelWidth] as? Int,
              let height = properties[kCGImagePropertyPixelHeight] as? Int,
              width > 0, height > 0 else { return nil }
        return max(width, height)
    }

    private static func isLossless(_ source: CGImageSource) -> Bool {
        guard let type = CGImageSourceGetType(source).flatMap({ UTType($0 as String) }) else { return false }
        return type.conforms(to: .png) || type.conforms(to: .gif)
    }

    private static func thumbnail(_ source: CGImageSource, maxSide: Int) -> CGImage? {
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: maxSide,
        ]
        return CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary)
    }

    /// Only the pixels are written; no source properties are copied across.
    private static func encode(_ image: CGImage, as type: UTType, quality: CGFloat? = nil) -> Data? {
        let out = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(out, type.identifier as CFString, 1, nil) else {
            return nil
        }
        var properties: [CFString: Any] = [:]
        if let quality { properties[kCGImageDestinationLossyCompressionQuality] = quality }
        CGImageDestinationAddImage(destination, image, properties as CFDictionary)
        guard CGImageDestinationFinalize(destination) else { return nil }
        return out as Data
    }
}
