import CoreGraphics
import Foundation

public enum CursorShape: String, Codable, Sendable {
    case block, underline, bar
}

public struct TerminalSettings: Codable, Equatable, Sendable {
    /// Nil means SF Mono, the system monospaced font.
    public var fontName: String?
    public var fontSize: CGFloat
    public var cursor: CursorShape
    public var cursorBlinks: Bool
    public var scrollback: Int
    public var optionAsMeta: Bool
    public var themeID: String
    /// OSC 52 reads hand the remote host the local clipboard, so they stay off unless the user opts in.
    public var allowClipboardRead: Bool
    /// VoiceOver speaks new output once it settles.
    public var readNewOutput: Bool

    public init(
        fontName: String? = nil,
        fontSize: CGFloat = 13,
        cursor: CursorShape = .block,
        cursorBlinks: Bool = true,
        scrollback: Int = 10_000,
        optionAsMeta: Bool = true,
        themeID: String = "beacon-dark",
        allowClipboardRead: Bool = false,
        readNewOutput: Bool = true
    ) {
        self.fontName = fontName
        self.fontSize = fontSize
        self.cursor = cursor
        self.cursorBlinks = cursorBlinks
        self.scrollback = scrollback
        self.optionAsMeta = optionAsMeta
        self.themeID = themeID
        self.allowClipboardRead = allowClipboardRead
        self.readNewOutput = readNewOutput
    }

    private enum CodingKeys: String, CodingKey {
        case fontName, fontSize, cursor, cursorBlinks, scrollback, optionAsMeta, themeID, allowClipboardRead, readNewOutput
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let d = TerminalSettings()
        fontName = try c.decodeIfPresent(String.self, forKey: .fontName) ?? d.fontName
        fontSize = try c.decodeIfPresent(CGFloat.self, forKey: .fontSize) ?? d.fontSize
        cursor = try c.decodeIfPresent(CursorShape.self, forKey: .cursor) ?? d.cursor
        cursorBlinks = try c.decodeIfPresent(Bool.self, forKey: .cursorBlinks) ?? d.cursorBlinks
        scrollback = try c.decodeIfPresent(Int.self, forKey: .scrollback) ?? d.scrollback
        optionAsMeta = try c.decodeIfPresent(Bool.self, forKey: .optionAsMeta) ?? d.optionAsMeta
        themeID = try c.decodeIfPresent(String.self, forKey: .themeID) ?? d.themeID
        allowClipboardRead = try c.decodeIfPresent(Bool.self, forKey: .allowClipboardRead) ?? d.allowClipboardRead
        readNewOutput = try c.decodeIfPresent(Bool.self, forKey: .readNewOutput) ?? d.readNewOutput
    }
}
