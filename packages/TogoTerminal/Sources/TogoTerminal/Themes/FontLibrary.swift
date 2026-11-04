import CoreText
import Foundation
import Synchronization

public enum FontInstallError: Error, Equatable, Sendable {
    case unsupportedType(String)
    case notAFont
    case notMonospaced(String)
    case copyFailed(String)
    case registrationFailed(String)
    case unsafeName(String)
}

/// User-imported terminal fonts, kept in Application Support/Fonts.
///
/// Registration uses process scope, so it lasts until the app exits; `installedFonts()`
/// registers the stored files again, which makes calling it at launch enough to restore them.
public enum FontLibrary {
    /// Collections are accepted because iOS ships Menlo and other system monospaced faces as
    /// `.ttc`; the first face in the file is the one validated and named.
    private static let acceptedExtensions: Set<String> = ["ttf", "otf", "ttc"]

    /// Validates, copies and registers a font. Returns its PostScript name.
    @discardableResult
    public static func install(fontAt url: URL) throws -> String {
        let ext = url.pathExtension.lowercased()
        guard acceptedExtensions.contains(ext) else { throw FontInstallError.unsupportedType(ext) }
        let name = try monospacedPostScriptName(at: url)

        let destination = try destination(forPostScriptName: name, extension: ext, in: try fontsDirectory())
        do {
            if FileManager.default.fileExists(atPath: destination.path) {
                _ = try FileManager.default.replaceItemAt(destination, withItemAt: try stagedCopy(of: url))
            } else {
                try FileManager.default.copyItem(at: url, to: destination)
            }
        } catch {
            throw FontInstallError.copyFailed("\(name): \(error.localizedDescription)")
        }
        try register(destination)
        return name
    }

    public static func installedFonts() -> [String] {
        guard let directory = try? fontsDirectory(),
              let files = try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)
        else { return [] }
        return files
            .filter { acceptedExtensions.contains($0.pathExtension.lowercased()) }
            .compactMap { file in
                guard let name = try? monospacedPostScriptName(at: file), (try? register(file)) != nil else {
                    return nil
                }
                return name
            }
            .sorted()
    }

    /// A terminal grid needs every glyph on the same advance; comparing "i" with "M" catches
    /// proportional fonts without walking the whole glyph table.
    static func monospacedPostScriptName(at url: URL) throws -> String {
        guard let descriptors = CTFontManagerCreateFontDescriptorsFromURL(url as CFURL) as? [CTFontDescriptor],
              let descriptor = descriptors.first else {
            throw FontInstallError.notAFont
        }
        let font = CTFontCreateWithFontDescriptor(descriptor, 12, nil)
        let name = CTFontCopyPostScriptName(font) as String
        var characters: [UniChar] = Array("iM".utf16)
        var glyphs = [CGGlyph](repeating: 0, count: 2)
        guard CTFontGetGlyphsForCharacters(font, &characters, &glyphs, 2) else {
            throw FontInstallError.notMonospaced(name)
        }
        var advances = [CGSize](repeating: .zero, count: 2)
        CTFontGetAdvancesForGlyphs(font, .horizontal, glyphs, &advances, 2)
        guard abs(advances[0].width - advances[1].width) < 0.001 else {
            throw FontInstallError.notMonospaced(name)
        }
        return name
    }

    /// The PostScript name comes from the imported file's name table, so it is untrusted: a
    /// crafted name with `/` or `..` would otherwise place the copy outside `Fonts/`.
    /// Characters outside `[A-Za-z0-9._-]` become `_`; a slash, an empty name, `.` and `..`
    /// are refused, and the resolved path must still sit directly inside `directory`.
    static func destination(forPostScriptName name: String, extension ext: String, in directory: URL) throws -> URL {
        guard !name.contains("/") else { throw FontInstallError.unsafeName(name) }
        let safe = String(name.unicodeScalars.map { scalar -> Character in
            let c = Character(scalar)
            return c.isASCII && (c.isLetter || c.isNumber || "._-".contains(c)) ? c : "_"
        })
        guard !safe.isEmpty, safe != ".", safe != ".." else { throw FontInstallError.unsafeName(name) }
        let destination = directory.appending(path: "\(safe).\(ext)").standardizedFileURL
        guard destination.deletingLastPathComponent().path == directory.standardizedFileURL.path else {
            throw FontInstallError.unsafeName(name)
        }
        return destination
    }

    private static func fontsDirectory() throws -> URL {
        let support = try FileManager.default.url(
            for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
        let directory = support.appending(path: "Fonts", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        return directory
    }

    /// `replaceItemAt` consumes its source, so the original import stays untouched.
    private static func stagedCopy(of url: URL) throws -> URL {
        let staged = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
            .appendingPathExtension(url.pathExtension)
        try FileManager.default.copyItem(at: url, to: staged)
        return staged
    }

    /// Registration errors that still leave the PostScript name usable: the file was registered
    /// earlier in this process, or a face with the same name is already available (the system
    /// copy of Menlo, for one).
    private static let harmlessErrors: Set<Int> = [
        CTFontManagerError.alreadyRegistered.rawValue,
        CTFontManagerError.duplicatedName.rawValue,
    ]

    /// For process scope Core Text calls the handler before returning, so the result is read
    /// straight after the call.
    private static func register(_ url: URL) throws {
        let outcome = RegistrationOutcome()
        CTFontManagerRegisterFontURLs([url] as CFArray, .process, true) { errors, done in
            for case let error as CFError in errors as [AnyObject]
            where !harmlessErrors.contains(CFErrorGetCode(error)) {
                outcome.record("\(CFErrorCopyDescription(error) as String) (\(CFErrorGetCode(error)))")
            }
            if done { outcome.finish() }
            return true
        }
        switch outcome.result {
        case .some(nil): return
        case .some(let message?): throw FontInstallError.registrationFailed("\(url.lastPathComponent): \(message)")
        case .none: throw FontInstallError.registrationFailed("\(url.lastPathComponent): no result")
        }
    }
}

/// Collects what the registration handler reports. The handler may run on another thread.
private final class RegistrationOutcome: Sendable {
    private struct State {
        var failure: String?
        var finished = false
    }

    private let state = Mutex(State())

    func record(_ message: String) {
        state.withLock { $0.failure = $0.failure ?? message }
    }

    func finish() {
        state.withLock { $0.finished = true }
    }

    /// Nil until the handler finishes; then the first failure, or nil for success.
    var result: String?? {
        state.withLock { $0.finished ? .some($0.failure) : .none }
    }
}
