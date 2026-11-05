import Foundation

/// Text from a repository, made safe to show. Control characters, direction controls and other
/// invisible characters appear as a visible code such as `⟨U+202E⟩`, so they cannot reorder the
/// text around them, break it onto a line of its own, or make two different names look the same.
/// A tab reads as a space. For display only: paths sent to the helper stay as they are.
enum DisplayText {
    static func clean(_ text: String) -> String {
        var shown = String.UnicodeScalarView()
        for scalar in text.unicodeScalars {
            if scalar == "\t" {
                shown.append(" ")
            } else if isHidden(scalar) {
                shown.append(contentsOf: marker(scalar).unicodeScalars)
            } else {
                shown.append(scalar)
            }
        }
        return String(shown)
    }

    /// Whether `clean` would mark anything in `text`, tabs aside.
    static func hasHidden(_ text: String) -> Bool {
        text.unicodeScalars.contains { $0 != "\t" && isHidden($0) }
    }

    /// Characters that draw nothing or change how the text around them is laid out: controls,
    /// line and paragraph separators, the direction marks, embeddings, overrides and isolates,
    /// and other format characters such as zero-width spaces. The joiners that shape writing
    /// (ZWNJ in Persian, Arabic and Indic names; ZWJ in emoji sequences) and the tags of
    /// emoji flag sequences draw as part of the text and are left alone.
    static func isHidden(_ scalar: Unicode.Scalar) -> Bool {
        if scalar.properties.isBidiControl { return true }
        switch scalar.value {
        case 0x200C, 0x200D, 0xE0020...0xE007F: return false
        default: break
        }
        switch scalar.properties.generalCategory {
        case .control, .format, .lineSeparator, .paragraphSeparator: return true
        default: return false
        }
    }

    /// `⟨U+202E⟩`: the code point, at least four hex digits.
    static func marker(_ scalar: Unicode.Scalar) -> String {
        let hex = String(scalar.value, radix: 16, uppercase: true)
        return "⟨U+" + String(repeating: "0", count: max(0, 4 - hex.count)) + hex + "⟩"
    }

    /// What VoiceOver says for a marker: the character's name when it has one.
    static func spoken(_ scalar: Unicode.Scalar) -> String {
        let name = scalar.properties.name ?? scalar.properties.nameAlias ?? "U+" + String(scalar.value, radix: 16, uppercase: true)
        return String(localized: "review.code.hiddenCharacter", defaultValue: "hidden character \(name.lowercased())")
    }
}
