import SwiftUI
import TogoKit

/// One line of flags for the agent, in SF Mono. The text is split into separate arguments by
/// `ArgsParser`; nothing typed here is ever read by a shell.
struct FlagsField: View {
    @Binding var text: String
    var enabled = true
    /// Why the field is off, shown under it.
    var footer: String?
    var error: String?
    /// Off inside a grouped list, whose row already draws the field's edge.
    var underlined = true

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            TextField(NewSessionCopy.flags, text: $text, axis: .vertical)
                .font(.system(.body, design: .monospaced))
                .lineLimit(1...4)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.done)
                .onChange(of: text) {
                    let plain = Self.plain(text)
                    if plain != text { text = plain }
                }
                .padding(.vertical, 10)
                .frame(minHeight: 44)
                .overlay(alignment: .bottom) { if underlined { Color.togoLine.frame(height: 1) } }
                .disabled(!enabled)
                .accessibilityLabel(NewSessionCopy.flags)
            if let error {
                InlineError(message: error)
            } else if let footer {
                Text(footer)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .fixedSize(horizontal: false, vertical: true)
                    .transition(.opacity)
            }
        }
        .resizesOnRows(error != nil)
    }

    /// Undoes smart punctuation from the keyboard or a pasted document where it would change
    /// a flag: a dash or minus sign that starts a word turns back into hyphens (an em or en dash
    /// into `--`), curly double quotes into straight ones, and curly single quotes into straight
    /// ones unless they sit between two letters, as an apostrophe does. Line breaks and tabs
    /// become spaces. Everything else stays exactly as typed.
    static func plain(_ text: String) -> String {
        let characters = Array(text)
        var result = ""
        var wordStart = true
        for (index, character) in characters.enumerated() {
            var replacement = String(character)
            switch character {
            case "\n", "\r", "\r\n", "\t":
                replacement = " "
            case "\u{201C}", "\u{201D}":
                replacement = "\""
            case "\u{2018}", "\u{2019}":
                let before = index > 0 ? characters[index - 1] : nil
                let after = index + 1 < characters.count ? characters[index + 1] : nil
                if before?.isLetter != true || after?.isLetter != true { replacement = "'" }
            case "\u{2014}", "\u{2013}":
                if wordStart { replacement = "--" }
            case "\u{2010}", "\u{2011}", "\u{2212}":
                if wordStart { replacement = "-" }
            default:
                break
            }
            result += replacement
            // A run of dashes at the start of a word is still the start of that word.
            wordStart = replacement == " " || (wordStart && replacement.allSatisfy { $0 == "-" })
        }
        return result
    }

    /// Why `line` cannot start an agent, or nil when it can. Beyond what `ArgsParser` refuses,
    /// the helper takes no empty argument, and invisible characters would let a command read
    /// differently from what runs.
    static func problem(_ line: String) -> String? {
        let args: [String]
        do {
            args = try ArgsParser.split(line)
        } catch {
            return NewSessionCopy.flagsError(error)
        }
        if args.contains("") { return NewSessionCopy.flagsEmpty }
        let invisible: Set<Unicode.GeneralCategory> = [.format, .lineSeparator, .paragraphSeparator]
        if line.unicodeScalars.contains(where: { invisible.contains($0.properties.generalCategory) }) {
            return NewSessionCopy.flagsInvisible
        }
        return nil
    }
}

extension NewSessionCopy {
    static func flagsError(_ error: ArgsError) -> String {
        switch error {
        case .unbalancedQuote: flagsUnbalanced
        case .controlCharacter: flagsControl
        case .tooLong: flagsTooLong
        }
    }
}
