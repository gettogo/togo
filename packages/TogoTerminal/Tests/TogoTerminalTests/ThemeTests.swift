import CoreText
import Foundation
import Testing
@testable import TogoTerminal

private let testsDirectory = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
private let repoRoot = testsDirectory
    .deletingLastPathComponent().deletingLastPathComponent()
    .deletingLastPathComponent().deletingLastPathComponent()

private func fixture(_ name: String) throws -> Data {
    try Data(contentsOf: testsDirectory.appending(path: "Fixtures/\(name)"))
}

/// WCAG 2 relative luminance and contrast, the same formula the brand checks use.
private func luminance(_ hex: String) -> Double {
    let value = Int(hex.dropFirst(), radix: 16) ?? 0
    func channel(_ shift: Int) -> Double {
        let c = Double((value >> shift) & 0xFF) / 255
        return c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4)
    }
    return 0.2126 * channel(16) + 0.7152 * channel(8) + 0.0722 * channel(0)
}

private func contrast(_ a: String, _ b: String) -> Double {
    let (la, lb) = (luminance(a), luminance(b))
    return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

private struct Tokens: Decodable {
    struct Accent: Decodable { let dark: String; let light: String }
    struct Colors: Decodable { let ink: String; let snow: String; let accent: Accent }
    let color: Colors
}

struct ThemeTests {
    @Test(arguments: ["sample.itermcolors", "sample.ghostty", "sample.Xresources"])
    func importersParseSamples(_ name: String) throws {
        let theme = try ThemeImporter.load(try fixture(name), filename: name)
        #expect(theme.ansi.count == 16)
        #expect(theme.ansi[0] == "#1D1F21")
        #expect(theme.ansi[4] == "#81A2BE")
        #expect(theme.ansi[15] == "#EAEAEA")
        #expect(theme.foreground == "#C5C8C6")
        #expect(theme.background == "#1D1F21")
        #expect(theme.cursor == "#AEAFAD")
        #expect(theme.name == "sample")
    }

    @Test(arguments: ["junk.itermcolors", "junk.ghostty", "junk.Xresources"])
    func importersRejectGarbage(_ name: String) {
        #expect(throws: ThemeImportError.self) {
            try ThemeImporter.load(Data("not a theme \u{0}\u{1}".utf8), filename: name)
        }
    }

    @Test func togoThemesMatchTokens() throws {
        let tokens = try JSONDecoder().decode(
            Tokens.self, from: Data(contentsOf: repoRoot.appending(path: "brand/tokens.json")))
        let dark = try #require(BuiltInThemes.all.first { $0.id == "togo-dark" })
        let light = try #require(BuiltInThemes.all.first { $0.id == "togo-light" })
        #expect(BuiltInThemes.all.map(\.id) == ["togo-dark", "togo-light", "plain"])

        #expect(dark.background == tokens.color.ink)
        #expect(dark.foreground == tokens.color.snow)
        #expect(light.background == tokens.color.snow)
        #expect(light.foreground == tokens.color.ink)

        let accents = [tokens.color.accent.dark, tokens.color.accent.light].map { $0.uppercased() }
        for theme in [dark, light] {
            #expect(theme.ansi.count == 16)
            #expect(contrast(theme.foreground, theme.background) >= 7)
            for index in Array(1...6) + Array(9...14) {
                let ratio = contrast(theme.ansi[index], theme.background)
                #expect(ratio >= 4.5, "\(theme.id) ansi \(index) is \(ratio):1")
            }
            for color in theme.ansi {
                #expect(!accents.contains(color.uppercased()), "\(theme.id) uses the accent")
            }
        }
    }

    @Test func ghosttyWithFifteenColoursIsRejected() throws {
        let text = String(decoding: try fixture("sample.ghostty"), as: UTF8.self)
            .split(separator: "\n").filter { !$0.hasPrefix("palette = 15=") }.joined(separator: "\n")
        #expect(throws: ThemeImportError.missingColor("ansi 15")) {
            try ThemeImporter.load(Data(text.utf8), filename: "short.ghostty")
        }
    }

    @Test func paletteIndexOutOfRangeIsRejected() {
        #expect(throws: ThemeImportError.self) {
            try ThemeImporter.load(Data("palette = 300=#000000\n".utf8), filename: "bad.ghostty")
        }
    }

    @Test(arguments: [15, 17])
    func decodingRejectsWrongPaletteSize(_ count: Int) throws {
        var theme = BuiltInThemes.all[0]
        theme.ansi = Array(repeating: "#000000", count: count)
        let data = try JSONEncoder().encode(theme)
        #expect(throws: DecodingError.self) { try JSONDecoder().decode(TerminalTheme.self, from: data) }
    }

    @Test func decodingRejectsMalformedColour() throws {
        var theme = BuiltInThemes.all[0]
        theme.foreground = "red"
        let data = try JSONEncoder().encode(theme)
        #expect(throws: DecodingError.self) { try JSONDecoder().decode(TerminalTheme.self, from: data) }
    }

    @Test func themeCodableRoundTrip() throws {
        let theme = BuiltInThemes.all[0]
        let decoded = try JSONDecoder().decode(TerminalTheme.self, from: JSONEncoder().encode(theme))
        #expect(decoded == theme)
    }

    @Test func settingsDecodeMissingKeysAsDefaults() throws {
        let settings = try JSONDecoder().decode(TerminalSettings.self, from: Data(#"{"fontSize":15}"#.utf8))
        var expected = TerminalSettings()
        expected.fontSize = 15
        #expect(settings == expected)
        #expect(expected.scrollback == 10_000)
        #expect(expected.themeID == "togo-dark")
        #expect(!expected.allowClipboardRead)
        #expect(expected.readNewOutput)
    }

    @Test func fontInstallRejectsProportional() throws {
        let scratch = FileManager.default.temporaryDirectory.appending(path: "togo-font-\(UUID().uuidString).ttf")
        try Data("definitely not a font".utf8).write(to: scratch)
        #expect(throws: FontInstallError.self) { try FontLibrary.install(fontAt: scratch) }

        #expect(throws: FontInstallError.self) { try FontLibrary.install(fontAt: try systemFontURL("Helvetica")) }

        let name = try FontLibrary.install(fontAt: try systemFontURL("Menlo-Regular"))
        #expect(name.hasPrefix("Menlo"))
        #expect(FontLibrary.installedFonts().contains(name))
    }

    @Test(arguments: ["../evil", "a/b", "/etc/passwd", "..", ".", ""])
    func fontDestinationRejectsUnsafeNames(_ name: String) {
        let directory = FileManager.default.temporaryDirectory.appending(path: "Fonts")
        #expect(throws: FontInstallError.unsafeName(name)) {
            try FontLibrary.destination(forPostScriptName: name, extension: "ttf", in: directory)
        }
    }

    @Test func fontDestinationReplacesOddCharacters() throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "Fonts")
        let url = try FontLibrary.destination(forPostScriptName: "Iosevka Term~Bold\u{0}é", extension: "otf", in: directory)
        #expect(url.lastPathComponent == "Iosevka_Term_Bold__.otf")
        #expect(url.deletingLastPathComponent().path == directory.standardizedFileURL.path)
        let plain = try FontLibrary.destination(forPostScriptName: "JetBrainsMono-Regular", extension: "ttf", in: directory)
        #expect(plain.lastPathComponent == "JetBrainsMono-Regular.ttf")
    }

    private func systemFontURL(_ name: String) throws -> URL {
        let font = CTFontCreateWithName(name as CFString, 12, nil)
        let url = CTFontCopyAttribute(font, kCTFontURLAttribute) as? URL
        return try #require(url, "no file URL for \(name)")
    }
}
