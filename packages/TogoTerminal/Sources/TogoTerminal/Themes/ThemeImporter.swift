import Foundation

public enum ThemeImportError: Error, Equatable, Sendable {
    case unreadable(String)
    case missingColor(String)
    case invalidColor(String)
}

/// Reads themes people already have: iTerm2 `.itermcolors`, Ghostty theme files and
/// Xresources. The format comes from the filename; anything that is not iTerm2 or
/// Xresources is read as Ghostty, whose theme files usually have no extension.
public enum ThemeImporter {
    public static func load(_ data: Data, filename: String) throws -> TerminalTheme {
        let url = URL(fileURLWithPath: filename)
        let ext = url.pathExtension.lowercased()
        let base = url.deletingPathExtension().lastPathComponent
        let colors: ParsedColors
        if ext == "itermcolors" {
            colors = try parseITerm(data)
        } else if ext == "xresources" || ext == "xrdb" || base.lowercased() == ".xresources" {
            colors = try parseXresources(try text(data, filename))
        } else {
            colors = try parseGhostty(try text(data, filename))
        }
        return try colors.theme(id: "imported-\(slug(base))", name: base)
    }

    private struct ParsedColors {
        var ansi: [Int: RGB] = [:]
        var foreground: RGB?
        var background: RGB?
        var cursor: RGB?
        var selection: RGB?

        func theme(id: String, name: String) throws -> TerminalTheme {
            var palette: [String] = []
            for index in 0..<16 {
                guard let color = ansi[index] else { throw ThemeImportError.missingColor("ansi \(index)") }
                palette.append(color.hex)
            }
            guard let foreground else { throw ThemeImportError.missingColor("foreground") }
            guard let background else { throw ThemeImportError.missingColor("background") }
            return TerminalTheme(
                id: id,
                name: name,
                ansi: palette,
                foreground: foreground.hex,
                background: background.hex,
                cursor: (cursor ?? foreground).hex,
                selection: (selection ?? ansi[8] ?? foreground).hex
            )
        }
    }

    private static func text(_ data: Data, _ filename: String) throws -> String {
        guard let text = String(data: data, encoding: .utf8) else {
            throw ThemeImportError.unreadable(filename)
        }
        return text
    }

    private static func slug(_ name: String) -> String {
        let allowed = name.lowercased().map { $0.isLetter || $0.isNumber ? $0 : "-" }
        return String(allowed).split(separator: "-").joined(separator: "-")
    }

    private static func color(_ value: String, _ key: String) throws -> RGB {
        guard let rgb = RGB(hex: value) else { throw ThemeImportError.invalidColor(key) }
        return rgb
    }

    // MARK: iTerm2

    private static func parseITerm(_ data: Data) throws -> ParsedColors {
        let plist = try? PropertyListSerialization.propertyList(from: data, format: nil)
        guard let root = plist as? [String: Any] else { throw ThemeImportError.unreadable("itermcolors") }
        func component(_ key: String) throws -> RGB? {
            guard let entry = root[key] as? [String: Any] else { return nil }
            func channel(_ name: String) throws -> UInt8 {
                guard let value = (entry["\(name) Component"] as? NSNumber)?.doubleValue,
                      (0...1).contains(value) else {
                    throw ThemeImportError.invalidColor(key)
                }
                return UInt8((value * 255).rounded())
            }
            return RGB(red: try channel("Red"), green: try channel("Green"), blue: try channel("Blue"))
        }
        var colors = ParsedColors()
        for index in 0..<16 {
            colors.ansi[index] = try component("Ansi \(index) Color")
        }
        colors.foreground = try component("Foreground Color")
        colors.background = try component("Background Color")
        colors.cursor = try component("Cursor Color")
        colors.selection = try component("Selection Color")
        return colors
    }

    // MARK: Ghostty

    private static func parseGhostty(_ text: String) throws -> ParsedColors {
        var colors = ParsedColors()
        for line in text.split(whereSeparator: \.isNewline) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.isEmpty || trimmed.hasPrefix("#") { continue }
            let parts = trimmed.split(separator: "=", maxSplits: 1).map { $0.trimmingCharacters(in: .whitespaces) }
            guard parts.count == 2 else { continue }
            switch parts[0] {
            case "palette":
                let entry = parts[1].split(separator: "=", maxSplits: 1).map { $0.trimmingCharacters(in: .whitespaces) }
                guard entry.count == 2, let index = Int(entry[0]), (0...255).contains(index) else {
                    throw ThemeImportError.invalidColor("palette \(parts[1])")
                }
                // 16 to 255 are the extended palette; they are validated but the engine
                // derives that range itself, so only the ANSI 16 are kept.
                let rgb = try color(entry[1], "palette \(index)")
                if index < 16 { colors.ansi[index] = rgb }
            case "foreground": colors.foreground = try color(parts[1], "foreground")
            case "background": colors.background = try color(parts[1], "background")
            case "cursor-color": colors.cursor = try color(parts[1], "cursor-color")
            case "selection-background": colors.selection = try color(parts[1], "selection-background")
            default: continue
            }
        }
        return colors
    }

    // MARK: Xresources

    /// Accepts `*.color4:`, `*color4:` and class-scoped forms such as `URxvt.color4:`.
    private static func parseXresources(_ text: String) throws -> ParsedColors {
        var colors = ParsedColors()
        for line in text.split(whereSeparator: \.isNewline) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.isEmpty || trimmed.hasPrefix("!") || trimmed.hasPrefix("#") { continue }
            let parts = trimmed.split(separator: ":", maxSplits: 1).map { $0.trimmingCharacters(in: .whitespaces) }
            guard parts.count == 2 else { continue }
            let resource = parts[0].split(whereSeparator: { $0 == "." || $0 == "*" }).last.map(String.init) ?? ""
            switch resource {
            case "foreground": colors.foreground = try color(parts[1], resource)
            case "background": colors.background = try color(parts[1], resource)
            case "cursorColor": colors.cursor = try color(parts[1], resource)
            default:
                guard resource.hasPrefix("color"), let index = Int(resource.dropFirst(5)) else { continue }
                guard (0...255).contains(index) else { throw ThemeImportError.invalidColor(resource) }
                let rgb = try color(parts[1], resource)
                if index < 16 { colors.ansi[index] = rgb }
            }
        }
        return colors
    }
}
