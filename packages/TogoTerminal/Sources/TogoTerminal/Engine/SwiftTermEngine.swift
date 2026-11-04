internal import SwiftTerm
import UIKit

/// `TerminalEngine` over SwiftTerm 1.99.2. This file and `TogoTerminalView.swift` are the only
/// places that touch SwiftTerm, so the 2.0 migration stays inside this folder.
@MainActor public final class SwiftTermEngine: TerminalEngine {
    public var onInput: (Data) -> Void = { _ in }
    public var onResize: ((cols: Int, rows: Int)) -> Void = { _ in }
    public private(set) var size: (cols: Int, rows: Int)
    public var onTitle: (String) -> Void = { _ in }
    public var onOpenURL: (URL) -> Void = { _ in }
    public var onBell: () -> Void = {}
    public var onClipboardWrite: (String) -> Void = { UIPasteboard.general.string = $0 }
    /// ⌘ shortcuts other than ⌘V, which the engine pastes itself so bracketed paste applies.
    public var onShortcut: (ShortcutAction) -> Void = { _ in }
    /// Consulted for an OSC 52 read only when `TerminalSettings.allowClipboardRead` is on.
    public var onClipboardRead: () -> String? = { UIPasteboard.general.string }

    /// One OSC string larger than this is dropped by the parser. SwiftTerm's default is 65 MiB,
    /// which lets one hostile title or clipboard write pin that much memory.
    static let maximumOscBytes = 1024 * 1024
    /// Titles feed window and tab labels; a remote can send megabytes of one.
    static let maximumTitleLength = 256

    private let terminalView: TogoTerminalView
    private let bridge = DelegateBridge()
    private var settings: TerminalSettings
    private var scrollRouter = ScrollRouter()

    public var view: UIView { terminalView }

    public init(settings: TerminalSettings, theme: TerminalTheme) {
        var options = TerminalOptions.default
        options.scrollback = settings.scrollback
        options.maximumOscBytes = Self.maximumOscBytes
        options.cursorStyle = Self.cursorStyle(for: settings)
        options.enableOsc8Hyperlinks = true
        terminalView = TogoTerminalView(frame: .zero, font: Self.font(for: settings), options: options)
        self.settings = settings
        let dimensions = terminalView.terminalDimensions
        size = (dimensions.cols, dimensions.rows)

        bridge.engine = self
        terminalView.terminalDelegate = bridge
        // Implicit detection finds bare URLs in output that carries no OSC 8 markup.
        terminalView.linkReporting = .implicit
        // SwiftTerm's iOS default only opens a link under a hovering pointer, so a finger tap
        // never could. `.always` makes OSC 8 links tappable; every one still passes LinkPolicy.
        terminalView.linkHighlightMode = .always
        terminalView.isOpaque = true
        terminalView.onWheelPan = { [weak self] distance, point in self?.pan(by: distance, at: point) }
        terminalView.onWheelPanEnded = { [weak self] in self?.scrollRouter.reset() }
        terminalView.capturesWheelPan = { [weak self] in self?.mouseReporting ?? false }
        terminalView.onShortcut = { [weak self] action in self?.perform(action) }
        apply(settings)
        apply(theme)
    }

    // MARK: TerminalEngine

    public func feed(_ bytes: ArraySlice<UInt8>) {
        terminalView.feed(byteArray: bytes)
    }

    public func send(_ key: KeyStroke) {
        let applicationCursor = terminalView.terminalInputStateSnapshot()?.applicationCursor ?? false
        write(KeyEncoder.encode(key, applicationCursor: applicationCursor))
    }

    /// Bracketed when the remote enabled mode 2004. The end marker is removed from the text
    /// first, otherwise pasted content could close the bracket and run as typed input.
    public func paste(_ text: String) {
        let bracketed = terminalView.terminalInputStateSnapshot()?.bracketedPasteMode ?? false
        guard bracketed else {
            write(Array(text.utf8))
            return
        }
        let body = text.replacingOccurrences(of: "\u{1b}[201~", with: "")
        write(Array("\u{1b}[200~\(body)\u{1b}[201~".utf8))
    }

    // MARK: Appearance

    public func apply(_ settings: TerminalSettings) {
        let previous = self.settings
        self.settings = settings
        if settings.fontName != previous.fontName || settings.fontSize != terminalView.font.pointSize {
            terminalView.font = Self.font(for: settings)
        }
        if settings.scrollback != previous.scrollback {
            terminalView.changeScrollback(settings.scrollback)
        }
        terminalView.setCursorStyle(Self.cursorStyle(for: settings))
        terminalView.optionAsMetaKey = settings.optionAsMeta
    }

    public func apply(_ theme: TerminalTheme) {
        // Decoding and import refuse other palette sizes; a theme built in code with the wrong
        // count keeps the previous palette rather than a partial one.
        let ansi = theme.ansi.map { Self.color($0) }
        if ansi.count == 16 { terminalView.installColors(ansi) }
        let background = Self.uiColor(theme.background)
        terminalView.nativeForegroundColor = Self.uiColor(theme.foreground)
        terminalView.nativeBackgroundColor = background
        terminalView.backgroundColor = background
        terminalView.caretColor = Self.uiColor(theme.cursor)
        terminalView.selectedTextBackgroundColor = Self.uiColor(theme.selection)
    }

    /// Needs the view in a window. Full-screen TUIs redraw most rows each frame, which is the
    /// case `.perFrameAggregated` is built for. Without Metal the Core Graphics renderer stays.
    public func useMetalIfAvailable() {
        guard terminalView.window != nil, !terminalView.isUsingMetalRenderer else { return }
        terminalView.metalBufferingMode = .perFrameAggregated
        do {
            try terminalView.setUseMetal(true)
        } catch {
            terminalView.metalBufferingMode = .perRowPersistent
        }
    }

    var isUsingMetal: Bool { terminalView.isUsingMetalRenderer }

    // MARK: Search and selection

    public func find(_ text: String, caseSensitive: Bool, regex: Bool, backwards: Bool) -> Bool {
        let options = SearchOptions(caseSensitive: caseSensitive, regex: regex, wholeWord: false)
        return backwards ? terminalView.findPrevious(text, options: options) : terminalView.findNext(text, options: options)
    }

    public func clearFind() {
        terminalView.clearSearch()
    }

    public var selectedText: String? { terminalView.getSelection() }

    public func selectAll() {
        terminalView.selectAll()
    }

    /// Key bar modifiers applied to the next typed key. The key bar installs its own.
    public var latch: ModifierLatch? {
        didSet {
            terminalView.latchedInput = latch.map { latch in
                // Longer input (autocorrect, dictation) goes to SwiftTerm untouched.
                { [weak latch] text in
                    guard let latch, latch.isActive, text.count == 1 else { return nil }
                    return latch.apply(to: Array(text.utf8))
                }
            }
        }
    }

    public var inputAccessory: UIView? {
        get { terminalView.inputAccessoryView }
        set {
            terminalView.inputAccessoryView = newValue
            if terminalView.isFirstResponder { terminalView.reloadInputViews() }
        }
    }

    // MARK: Internals shared with the delegate bridge and tests

    /// The OSC 52 read policy: nothing leaves the device unless the user allowed reads.
    func clipboardForRemote() -> Data? {
        guard settings.allowClipboardRead else { return nil }
        return onClipboardRead().map { Data($0.utf8) }
    }

    var textInputView: any UITextInput { terminalView }

    var cellSize: CGSize {
        let dimensions = terminalView.terminalDimensions
        let frame = terminalView.getOptimalFrameSize()
        guard dimensions.cols > 0, dimensions.rows > 0 else { return .zero }
        return CGSize(width: frame.width / CGFloat(dimensions.cols), height: frame.height / CGFloat(dimensions.rows))
    }

    var cursorPosition: (col: Int, row: Int) {
        let cursor = terminalView.terminalStateSnapshot().cursor
        return (cursor.col, cursor.row)
    }

    var scrollPosition: Double { terminalView.scrollPosition }

    func lineText(_ row: Int) -> String {
        terminalView.visibleRowsText(row..<(row + 1)).first ?? ""
    }

    var mouseReporting: Bool {
        (terminalView.terminalInputStateSnapshot()?.mouseMode ?? .off) != .off
    }

    /// Vertical pan travel in points; positive reveals older output.
    func pan(by distance: CGFloat, at point: CGPoint) {
        let cell = cellSize
        switch scrollRouter.pan(by: distance, lineHeight: cell.height, mouseReporting: mouseReporting) {
        case .none:
            break
        case .wheel(let up, let count):
            let dimensions = terminalView.terminalDimensions
            let col = min(max(Int(point.x / cell.width), 0), dimensions.cols - 1)
            let row = min(max(Int((point.y - terminalView.bounds.minY) / cell.height), 0), dimensions.rows - 1)
            for _ in 0..<count {
                terminalView.sendMouseEvent(button: up ? 4 : 5, release: false, col: col, row: row)
            }
        case .local(let lines):
            if lines > 0 { terminalView.scrollUp(lines: lines) } else { terminalView.scrollDown(lines: -lines) }
        }
    }

    func reportSize(cols: Int, rows: Int) {
        guard cols != size.cols || rows != size.rows else { return }
        size = (cols, rows)
        onResize(size)
    }

    func receivedTitle(_ title: String) {
        onTitle(String(title.prefix(Self.maximumTitleLength)))
    }

    func requestedLink(_ link: String) {
        guard let url = URL(string: link), LinkPolicy.allows(url) else { return }
        onOpenURL(url)
    }

    func perform(_ action: ShortcutAction) {
        guard action == .paste else { return onShortcut(action) }
        if let text = UIPasteboard.general.string { paste(text) }
    }

    func handleChord(
        characters: String, ignoring: String, modifiers: UIKeyModifierFlags, keyCode: Int,
        isHeld: @escaping @MainActor () -> Bool
    ) -> Bool {
        terminalView.handleChord(
            characters: characters, charactersIgnoringModifiers: ignoring, modifiers: modifiers, keyCode: keyCode,
            isHeld: isHeld)
    }

    var chordRepeater: ChordRepeater { terminalView.chordRepeater }

    /// Typed text, sent as if keyed: never bracketed, unlike `paste`.
    func sendText(_ text: String) {
        write(Array(text.utf8))
    }

    func endEditing() {
        _ = terminalView.resignFirstResponder()
    }

    /// SwiftTerm's input queue keeps engine writes ordered with keyboard and IME input.
    private func write(_ bytes: [UInt8]) {
        guard !bytes.isEmpty else { return }
        terminalView.send(data: bytes[...])
    }

    // MARK: Conversions

    private static func font(for settings: TerminalSettings) -> UIFont {
        if let name = settings.fontName, let font = UIFont(name: name, size: settings.fontSize) {
            return font
        }
        return UIFont.monospacedSystemFont(ofSize: settings.fontSize, weight: .regular)
    }

    private static func cursorStyle(for settings: TerminalSettings) -> CursorStyle {
        switch (settings.cursor, settings.cursorBlinks) {
        case (.block, true): .blinkBlock
        case (.block, false): .steadyBlock
        case (.underline, true): .blinkUnderline
        case (.underline, false): .steadyUnderline
        case (.bar, true): .blinkBar
        case (.bar, false): .steadyBar
        }
    }

    private static func rgb(_ hex: String) -> RGB {
        RGB(hex: hex) ?? RGB(red: 0, green: 0, blue: 0)
    }

    private static func color(_ hex: String) -> SwiftTerm.Color {
        let c = rgb(hex)
        return SwiftTerm.Color(red8: UInt16(c.red), green8: UInt16(c.green), blue8: UInt16(c.blue))
    }

    private static func uiColor(_ hex: String) -> UIColor {
        let c = rgb(hex)
        return UIColor(red: CGFloat(c.red) / 255, green: CGFloat(c.green) / 255, blue: CGFloat(c.blue) / 255, alpha: 1)
    }
}

/// Receives SwiftTerm's delegate calls so `SwiftTermEngine`'s public type does not expose a
/// SwiftTerm conformance.
@MainActor private final class DelegateBridge: TerminalViewDelegate {
    weak var engine: SwiftTermEngine?

    func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
        engine?.reportSize(cols: newCols, rows: newRows)
    }

    func setTerminalTitle(source: TerminalView, title: String) {
        engine?.receivedTitle(title)
    }

    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}

    func send(source: TerminalView, data: ArraySlice<UInt8>) {
        engine?.onInput(Data(data))
    }

    func scrolled(source: TerminalView, position: Double) {}

    func requestOpenLink(source: TerminalView, link: String, params: [String: String]) {
        engine?.requestedLink(link)
    }

    func bell(source: TerminalView) {
        engine?.onBell()
    }

    func clipboardCopy(source: TerminalView, content: Data) {
        guard let text = String(data: content, encoding: .utf8) else { return }
        engine?.onClipboardWrite(text)
    }

    func clipboardRead(source: TerminalView) -> Data? {
        engine?.clipboardForRemote()
    }

    func rangeChanged(source: TerminalView, startY: Int, endY: Int) {}
}
