// Generates the iOS color assets, Palette.swift and MotionTokens.swift from brand/tokens.json.
// Usage: swift scripts/gen-tokens.swift [--check]
// With --check nothing is written; the exit status is 1 when any generated
// file is missing or differs, so CI catches a tokens.json edit that was never
// regenerated.
import Foundation

struct Role {
    let key: String
    let asset: String
    let dark: String
    let light: String
    /// Increase Contrast values, set for the roles that have them.
    var highDark: String?
    var highLight: String?
}

func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data("gen-tokens: \(message)\n".utf8))
    exit(1)
}

let repoRoot = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
let tokensURL = repoRoot.appendingPathComponent("brand/tokens.json")
let assetsURL = repoRoot.appendingPathComponent("apps/ios/Togo/Resources/Colors.xcassets")
let paletteURL = repoRoot.appendingPathComponent("apps/ios/Togo/Design/Palette.swift")
let motionURL = repoRoot.appendingPathComponent("apps/ios/Togo/Design/MotionTokens.swift")
let checkOnly = CommandLine.arguments.dropFirst().contains("--check")

// Order fixes the order of the generated Swift, so output is stable.
// Roles that also carry Increase Contrast values in highContrast.dark and highContrast.light.
let highContrastKeys: Set<String> = ["mute", "muteDim", "line", "accent"]

let roleKeys = [
    "ink", "deepest", "raised", "line", "snow", "mute", "muteDim", "grouped",
    // Content colours for diffs and code; they never reuse the accent hue.
    "diffAddedBg", "diffAddedWord", "diffRemovedBg", "diffRemovedWord", "diffHunkBg", "gutterText",
    "syntaxKeyword", "syntaxType", "syntaxString", "syntaxNumber", "syntaxComment", "syntaxAttribute",
]

func loadRoles() -> [Role] {
    guard let data = try? Data(contentsOf: tokensURL),
          let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
          let color = root["color"] as? [String: Any],
          let light = color["light"] as? [String: String],
          let accent = color["accent"] as? [String: String]
    else { fail("cannot read color, color.light and color.accent from \(tokensURL.path)") }

    guard let high = root["highContrast"] as? [String: [String: String]],
          let highDark = high["dark"], let highLight = high["light"]
    else { fail("highContrast needs dark and light") }
    guard Set(highDark.keys) == highContrastKeys, Set(highLight.keys) == highContrastKeys else {
        fail("highContrast.dark and highContrast.light need exactly \(highContrastKeys.sorted())")
    }
    var roles: [Role] = roleKeys.map { key in
        guard let dark = color[key] as? String, let lightValue = light[key] else {
            fail("role \(key) needs a dark value and a color.light value")
        }
        return Role(
            key: key, asset: key.prefix(1).uppercased() + key.dropFirst(), dark: dark, light: lightValue,
            highDark: highDark[key], highLight: highLight[key]
        )
    }
    guard let accentDark = accent["dark"], let accentLight = accent["light"] else {
        fail("color.accent needs dark and light")
    }
    roles.append(Role(
        key: "accent", asset: "Accent", dark: accentDark, light: accentLight,
        highDark: highDark["accent"], highLight: highLight["accent"]
    ))
    // The light accent is too pale for text on the light background, so "Needs you" gets its own
    // darker value there.
    guard let accentText = color["accentText"] as? [String: String],
          let textDark = accentText["dark"], let textLight = accentText["light"]
    else { fail("color.accentText needs dark and light") }
    roles.append(Role(key: "accentText", asset: "AccentText", dark: textDark, light: textLight))
    return roles
}

func components(_ hex: String) -> (String, String, String) {
    let digits = hex.hasPrefix("#") ? String(hex.dropFirst()) : hex
    guard digits.count == 6, UInt32(digits, radix: 16) != nil else { fail("\(hex) is not #RRGGBB") }
    let chars = Array(digits.uppercased())
    return ("0x" + String(chars[0...1]), "0x" + String(chars[2...3]), "0x" + String(chars[4...5]))
}

func colorEntry(_ hex: String, dark: Bool, high: Bool = false) -> String {
    let (red, green, blue) = components(hex)
    func item(_ name: String, _ value: String) -> String {
        "{\n          \"appearance\" : \"\(name)\",\n          \"value\" : \"\(value)\"\n        }"
    }
    var appearances: [String] = []
    if dark { appearances.append(item("luminosity", "dark")) }
    if high { appearances.append(item("contrast", "high")) }
    let appearance = appearances.isEmpty ? "" : """

          "appearances" : [
            \(appearances.joined(separator: ",\n        "))
          ],
    """
    return """
        {\(appearance)
          "color" : {
            "color-space" : "srgb",
            "components" : {
              "alpha" : "1.000",
              "blue" : "\(blue)",
              "green" : "\(green)",
              "red" : "\(red)"
            }
          },
          "idiom" : "universal"
        }
    """
}

func colorset(_ role: Role) -> String {
    var entries = [colorEntry(role.light, dark: false), colorEntry(role.dark, dark: true)]
    if let highLight = role.highLight, let highDark = role.highDark {
        entries.append(colorEntry(highLight, dark: false, high: true))
        entries.append(colorEntry(highDark, dark: true, high: true))
    }
    return """
    {
      "colors" : [
    \(entries.joined(separator: ",\n"))
      ],
      "info" : {
        "author" : "xcode",
        "version" : 1
      }
    }

    """
}

let catalogContents = """
{
  "info" : {
    "author" : "xcode",
    "version" : 1
  }
}

"""

func palette(_ roles: [Role]) -> String {
    let swiftUI = roles.map { "    static let togo\($0.asset) = Color(.\($0.key))" }.joined(separator: "\n")
    let uiKit = roles.map { "    static let togo\($0.asset) = UIColor(resource: .\($0.key))" }.joined(separator: "\n")
    return """
    // Generated by scripts/gen-tokens.swift from brand/tokens.json. Do not edit.
    import SwiftUI
    import UIKit

    extension Color {
    \(swiftUI)
    }

    extension UIColor {
    \(uiKit)
    }

    """
}

// Order fixes the order of the generated enum cases.
let motionRoleKeys = [
    "tabSettle", "dropIn", "collapse", "shrinkIntoTab", "confirm", "fade", "pawStep", "rowGrow",
    "paneFlow", "sidebar", "chrome", "dim", "listen",
    "respond", "resize", "reveal", "conceal", "emphasis", "progress", "ambient",
]

func seconds(_ value: Any?, _ what: String) -> String {
    guard let number = value as? NSNumber, number.doubleValue > 0, number.doubleValue < 5 else {
        fail("\(what) needs a duration in seconds")
    }
    return String(format: "%.2f", number.doubleValue)
}

func motionCase(_ key: String, _ token: [String: Any]) -> String {
    let curve: String
    if let spring = token["spring"] as? [Any], spring.count == 2 {
        curve = ".spring(response: \(seconds(spring[0], "motion.\(key).spring")), damping: \(seconds(spring[1], "motion.\(key).spring")))"
    } else if let duration = token["easeOut"] {
        curve = ".easeOut(\(seconds(duration, "motion.\(key).easeOut")))"
    } else if let duration = token["easeInOut"] {
        curve = ".easeInOut(\(seconds(duration, "motion.\(key).easeInOut")))"
    } else {
        fail("motion.\(key) needs spring [response, damping], easeOut or easeInOut")
    }
    let reduced: String
    if token["reduced"] as? String == "instant" {
        reduced = ".instant"
    } else if let fade = token["reduced"] as? [String: Any], fade.count == 1, let duration = fade["fade"] {
        reduced = ".fade(\(seconds(duration, "motion.\(key).reduced.fade")))"
    } else {
        fail("motion.\(key).reduced must be {\"fade\": seconds} or \"instant\"")
    }
    return "        case .\(key): MotionSpec(curve: \(curve), reduced: \(reduced))"
}

func motionTokens() -> String {
    guard let data = try? Data(contentsOf: tokensURL),
          let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
          let motion = root["motion"] as? [String: [String: Any]]
    else { fail("cannot read motion from \(tokensURL.path)") }
    let extra = Set(motion.keys).subtracting(motionRoleKeys)
    guard extra.isEmpty else { fail("motion roles not known to the generator: \(extra.sorted())") }
    let cases = motionRoleKeys.map { key in
        guard let token = motion[key] else { fail("motion.\(key) is missing") }
        return motionCase(key, token)
    }
    let names = motionRoleKeys.joined(separator: ", ")
    return """
    // Generated by scripts/gen-tokens.swift from brand/tokens.json. Do not edit.

    /// What a change is, so the same kind of change always moves the same way.
    enum MotionRole: String, CaseIterable, Sendable {
        case \(names)
    }

    extension Motion {
        static func spec(_ role: MotionRole) -> MotionSpec {
            switch role {
    \(cases.joined(separator: "\n"))
            }
        }
    }

    """
}

let roles = loadRoles()
var outputs: [(URL, String)] = [(assetsURL.appendingPathComponent("Contents.json"), catalogContents)]
for role in roles {
    outputs.append((assetsURL.appendingPathComponent("\(role.asset).colorset/Contents.json"), colorset(role)))
}
outputs.append((paletteURL, palette(roles)))
outputs.append((motionURL, motionTokens()))

var stale: [String] = []
for (url, contents) in outputs {
    let current = try? String(contentsOf: url, encoding: .utf8)
    guard current != contents else { continue }
    if checkOnly {
        stale.append(url.path.replacingOccurrences(of: repoRoot.path + "/", with: ""))
        continue
    }
    do {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try contents.write(to: url, atomically: true, encoding: .utf8)
    } catch {
        fail("write \(url.path): \(error)")
    }
}

// A role removed from tokens.json would otherwise leave its colorset behind
// and keep compiling into the app.
let expectedSets = Set(roles.map { "\($0.asset).colorset" })
let presentSets = (try? FileManager.default.contentsOfDirectory(atPath: assetsURL.path)) ?? []
for name in presentSets.sorted() where name.hasSuffix(".colorset") && !expectedSets.contains(name) {
    stale.append("apps/ios/Togo/Resources/Colors.xcassets/\(name) (no such role, delete it)")
}

if !stale.isEmpty {
    fail("out of date with brand/tokens.json, run make tokens:\n" + stale.joined(separator: "\n"))
}
