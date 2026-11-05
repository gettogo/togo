import Foundation
import Testing
import UIKit
@testable import Togo

struct TokensTests {
    private static let roles: [(key: String, color: UIColor)] = [
        ("ink", .togoInk), ("deepest", .togoDeepest), ("raised", .togoRaised), ("line", .togoLine),
        ("snow", .togoSnow), ("mute", .togoMute), ("muteDim", .togoMuteDim), ("grouped", .togoGrouped),
        ("diffAddedBg", .togoDiffAddedBg), ("diffAddedWord", .togoDiffAddedWord),
        ("diffRemovedBg", .togoDiffRemovedBg), ("diffRemovedWord", .togoDiffRemovedWord),
        ("diffHunkBg", .togoDiffHunkBg), ("gutterText", .togoGutterText),
        ("syntaxKeyword", .togoSyntaxKeyword), ("syntaxType", .togoSyntaxType),
        ("syntaxString", .togoSyntaxString), ("syntaxNumber", .togoSyntaxNumber),
        ("syntaxComment", .togoSyntaxComment), ("syntaxAttribute", .togoSyntaxAttribute),
    ]

    private static let diffBackgrounds: [UIColor] = [
        .togoDiffAddedBg, .togoDiffAddedWord, .togoDiffRemovedBg, .togoDiffRemovedWord, .togoDiffHunkBg,
    ]
    private static let syntaxColours: [UIColor] = [
        .togoSyntaxKeyword, .togoSyntaxType, .togoSyntaxString, .togoSyntaxNumber, .togoSyntaxComment,
        .togoSyntaxAttribute,
    ]

    @Test func paletteMatchesTokensFile() throws {
        let color = try Self.tokenColors()
        let light = try #require(color["light"] as? [String: String])
        for role in Self.roles {
            let dark = try #require(color[role.key] as? String, "missing dark \(role.key)")
            let lightValue = try #require(light[role.key], "missing light \(role.key)")
            #expect(Self.hex(role.color, .dark) == dark.uppercased(), "dark \(role.key)")
            #expect(Self.hex(role.color, .light) == lightValue.uppercased(), "light \(role.key)")
        }
        #expect(Self.hex(.togoAccent, .dark) == "#9FD0F5")
        #expect(Self.hex(.togoAccent, .light) == "#3D8FD1")
        let accentText = try #require(color["accentText"] as? [String: String])
        #expect(Self.hex(.togoAccentText, .dark) == accentText["dark"]?.uppercased())
        #expect(Self.hex(.togoAccentText, .light) == accentText["light"]?.uppercased())
    }

    @Test(arguments: [UIUserInterfaceStyle.dark, .light])
    func snowAndMuteMeetContrastOnInk(style: UIUserInterfaceStyle) {
        #expect(Self.contrast(.togoSnow, .togoInk, style) >= 4.5)
        #expect(Self.contrast(.togoMute, .togoInk, style) >= 4.5)
        #expect(Self.contrast(.togoAccentText, .togoInk, style) >= 4.5)
        #expect(Self.contrast(.togoAccentText, .togoRaised, style) >= 4.5)
    }

    @Test(arguments: [UIUserInterfaceStyle.dark, .light])
    func textMeetsContrastOnGroupedCards(style: UIUserInterfaceStyle) {
        #expect(Self.contrast(.togoSnow, .togoGrouped, style) >= 4.5)
        #expect(Self.contrast(.togoMute, .togoGrouped, style) >= 4.5)
    }

    /// Grouped cards sit on the ink page; in dark they need a visible step or the groups blur
    /// into the background.
    @Test func groupedCardsStandOffThePageInDark() {
        #expect(Self.contrast(.togoGrouped, .togoInk, .dark) >= 1.1)
    }

    @Test(arguments: [UIUserInterfaceStyle.dark, .light])
    func diffAndSyntaxColoursMeetContrast(style: UIUserInterfaceStyle) {
        for background in Self.diffBackgrounds {
            #expect(Self.contrast(background, .togoGutterText, style) >= 4.5, "gutter on \(Self.hex(background, style))")
            #expect(Self.contrast(background, .togoSnow, style) >= 4.5, "snow on \(Self.hex(background, style))")
        }
        for colour in Self.syntaxColours + [.togoMute] {
            let name = Self.hex(colour, style)
            #expect(Self.contrast(colour, .togoInk, style) >= 4.5, "\(name) on ink")
            for background in Self.diffBackgrounds {
                #expect(Self.contrast(colour, background, style) >= 4.5, "\(name) on \(Self.hex(background, style))")
            }
        }
    }

    @Test(arguments: [UIUserInterfaceStyle.dark, .light])
    func noSyntaxColourNearAccentHue(style: UIUserInterfaceStyle) {
        let accents = [UIColor.togoAccent, .togoAccentText].map { Self.hue($0, style) }
        for colour in Self.syntaxColours + Self.diffBackgrounds + [.togoGutterText] {
            let hue = Self.hue(colour, style)
            for accent in accents {
                let gap = min(abs(hue - accent), 360 - abs(hue - accent))
                #expect(gap > 20, "\(Self.hex(colour, style)) is \(Int(gap)) degrees from the accent")
            }
        }
    }

    private static func hue(_ color: UIColor, _ style: UIUserInterfaceStyle) -> CGFloat {
        var hue: CGFloat = 0, saturation: CGFloat = 0, brightness: CGFloat = 0, alpha: CGFloat = 0
        color.resolvedColor(with: UITraitCollection(userInterfaceStyle: style))
            .getHue(&hue, saturation: &saturation, brightness: &brightness, alpha: &alpha)
        return hue * 360
    }

    private static func tokenColors() throws -> [String: Any] {
        // apps/ios/TogoTests/TokensTests.swift -> repo root, four levels up.
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("brand/tokens.json")
        let root = try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any]
        return try #require(root?["color"] as? [String: Any])
    }

    private static func rgb(_ color: UIColor, _ style: UIUserInterfaceStyle) -> [CGFloat] {
        let resolved = color.resolvedColor(with: UITraitCollection(userInterfaceStyle: style))
        var red: CGFloat = 0, green: CGFloat = 0, blue: CGFloat = 0, alpha: CGFloat = 0
        resolved.getRed(&red, green: &green, blue: &blue, alpha: &alpha)
        return [red, green, blue]
    }

    private static func hex(_ color: UIColor, _ style: UIUserInterfaceStyle) -> String {
        "#" + rgb(color, style).map { String(format: "%02X", Int(($0 * 255).rounded())) }.joined()
    }

    // WCAG 2.x relative luminance and contrast ratio.
    private static func contrast(_ a: UIColor, _ b: UIColor, _ style: UIUserInterfaceStyle) -> CGFloat {
        func luminance(_ color: UIColor) -> CGFloat {
            let linear = rgb(color, style).map { $0 <= 0.04045 ? $0 / 12.92 : pow(($0 + 0.055) / 1.055, 2.4) }
            return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2]
        }
        let (la, lb) = (luminance(a), luminance(b))
        return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
    }
}
