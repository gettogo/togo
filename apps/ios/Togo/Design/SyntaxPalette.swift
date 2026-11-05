import SwiftUI
import TogoKit
import UIKit

/// Colours for code and diff text. They are content colours, like terminal output: muted, never
/// the accent hue, and each meets 4.5:1 on the page and on the added and removed row tints.
enum SyntaxPalette {
    static func uiColor(_ kind: TokenKind) -> UIColor {
        switch kind {
        case .plain: .togoSnow
        case .keyword: .togoSyntaxKeyword
        case .type: .togoSyntaxType
        case .string: .togoSyntaxString
        case .number: .togoSyntaxNumber
        case .comment, .hidden: .togoSyntaxComment
        case .punctuation: .togoMute
        case .attribute: .togoSyntaxAttribute
        }
    }

    static func color(_ kind: TokenKind) -> Color {
        Color(uiColor(kind))
    }
}

/// Row tints for diff lines. A tint is never the only sign of a change: rows also carry a
/// gutter glyph and spoken words.
enum DiffPalette {
    static func background(_ kind: DiffLineKind) -> UIColor? {
        switch kind {
        case .added: .togoDiffAddedBg
        case .removed: .togoDiffRemovedBg
        case .context, .noNewline: nil
        }
    }

    /// The stronger tint behind the words that changed within a line.
    static func word(_ kind: DiffLineKind) -> UIColor? {
        switch kind {
        case .added: .togoDiffAddedWord
        case .removed: .togoDiffRemovedWord
        case .context, .noNewline: nil
        }
    }

    static let hunk = UIColor.togoDiffHunkBg
    static let gutter = UIColor.togoGutterText
}
