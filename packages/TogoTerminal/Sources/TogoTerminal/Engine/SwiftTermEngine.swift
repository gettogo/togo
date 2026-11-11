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
    /// An image pasted with ⌘V or the key bar's Paste key, when the host can take one. Nil
    /// leaves image-only pasteboards alone.
    public var onPasteImage: ((Data) -> Void)?

    /// One OSC string larger than this is dropped by the parser. SwiftTerm's default is 65 MiB,
    /// which lets one hostile title or clipboard write pin that much memory.
    static let maximumOscBytes = 1024 * 1024
    /// Titles feed window and tab labels; a remote can send megabytes of one.
    static let maximumTitleLength = 256

    let terminalView: TogoTerminalView
    /// Speaks new output while VoiceOver runs, this terminal is focused and the setting is on.
    let outputFeed = OutputFeed(announcer: OutputAnnouncer(clock: ContinuousClock()))
    /// The words VoiceOver hears around terminal text; the app sets its localized ones.
    public var accessibilityWords = AccessibilityWords.english
    /// The screen is read at most this often while the feed listens; the quiet spell is 1.5 s.
    static let screenReadInterval: Duration = .milliseconds(250)
    private var screenReadPending = false
    /// A screen read off the main thread, handed to the feed when it asks.
    private var readScreen: TerminalScreen?
    private var cursorLineCache: (value: String?, age: () -> Duration)?
    private let bridge = DelegateBridge()
    private(set) var settings: TerminalSettings
    private var scrollRouter = ScrollRouter()
    private var modifyKeys = ModifyKeysScanner()

    public var view: UIView { terminalView }
    /// The theme background, for margins around the grid. SwiftTerm paints it on the view's
    /// layer; setting the view's `backgroundColor` as well would fight it for that layer.
    public private(set) var backgroundColor: UIColor = .black

    public init(settings: TerminalSettings, theme: TerminalTheme) {
        var options = TerminalOptions.default
        options.scrollback = settings.scrollback
        options.maximumOscBytes = Self.maximumOscBytes
        options.cursorStyle = Self.cursorStyle(for: settings)
        options.enableOsc8Hyperlinks = true
        terminalView = TogoTerminalView(frame: .zero, font: Self.font(for: settings), options: options)
        terminalView.cursorBlinkRequested = settings.cursorBlinks
        terminalView.cursorShape = Self.cursorStyle(for: settings)
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
        terminalView.onWheelPan = { [weak self] distance, point in self?.pan(by: distance, at: point) }
        terminalView.onWheelPanEnded = { [weak self] in self?.scrollRouter.reset() }
        terminalView.capturesWheelPan = { [weak self] in self?.mouseReporting ?? false }
        terminalView.onShortcut = { [weak self] action in self?.perform(action) }
        terminalView.onPaste = { [weak self] text in self?.paste(text) }
        // The edit menu's Paste with no text on the pasteboard: a picture, or a lone link.
        terminalView.onPasteWithoutText = { [weak self] in self?.pasteFromClipboard() }
        terminalView.onCopy = { [weak self] text in self?.onClipboardWrite(text) }
        terminalView.extendedKeyMode = { [weak self] in self?.modifyKeys.mode ?? .legacy }
        terminalView.cursorLine = { [weak self] in self?.cursorLine() }
        terminalView.floatingRowHeight = { [weak self] in self?.cellSize.height ?? 0 }
        terminalView.onFloatingRows = { [weak self] rows in
            guard let self else { return }
            let key = KeyStroke(rows < 0 ? .up : .down)
            write(Array(repeating: bytes(for: key), count: abs(rows)).flatMap { $0 })
        }
        outputFeed.screen = { [weak self] in self?.readScreen ?? self?.liveScreen() }
        outputFeed.voiceOverRunning = { UIAccessibility.isVoiceOverRunning }
        outputFeed.focused = { [weak self] in self?.terminalView.hasFocus ?? false }
        outputFeed.onActiveChange = { [weak self] in self?.terminalView.notifyUpdateChanges = $0 }
        outputFeed.announcer.announce = { text in
            // Queued, so VoiceOver finishes what it is saying and SwiftTerm's layout notices do not cut it off.
            let queued = NSAttributedString(string: text, attributes: [.accessibilitySpeechQueueAnnouncement: true])
            UIAccessibility.post(notification: .announcement, argument: queued)
        }
        terminalView.onFocusChange = { [weak self] in self?.outputFeed.refresh() }
        terminalView.onUserInput = { [weak self] in self?.outputFeed.noteInput() }
        outputFeed.observeAccessibilityChanges()
        apply(settings)
        apply(theme)
    }

    // MARK: TerminalEngine

    public func feed(_ bytes: ArraySlice<UInt8>) {
        modifyKeys.scan(bytes)
        terminalView.feed(byteArray: bytes)
    }

    public func send(_ key: KeyStroke) {
        write(bytes(for: key))
    }

    /// Kitty flags win over `modifyOtherKeys` when a remote has set both.
    var extendedKeyMode: ExtendedKeyMode {
        let kitty = !(terminalView.terminalInputStateSnapshot()?.keyboardEnhancementFlags.isEmpty ?? true)
        return kitty ? .kitty : modifyKeys.mode
    }

    /// Bracketed when the remote enabled mode 2004. Either way the text passes
    /// `PasteText.terminal` first, so it cannot close the bracket or carry its own control
    /// sequences, and its line endings reach the pty as LF. A long paste goes out in
    /// `PasteText.chunkSize` writes, in order, one after another on this turn of the main
    /// actor, so nothing typed can land between them.
    public func paste(_ text: String) {
        let bracketed = terminalView.terminalInputStateSnapshot()?.bracketedPasteMode ?? false
        for chunk in PasteText.chunks(PasteText.payload(text, bracketed: bracketed)) {
            write(Array(chunk))
        }
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
        terminalView.cursorBlinkRequested = settings.cursorBlinks
        terminalView.cursorShape = Self.cursorStyle(for: settings)
        terminalView.optionAsMetaKey = settings.optionAsMeta
        outputFeed.enabled = settings.readNewOutput
    }

    public func apply(_ theme: TerminalTheme) {
        // Decoding and import refuse other palette sizes; a theme built in code with the wrong
        // count keeps the previous palette rather than a partial one.
        let ansi = theme.ansi.map { Self.color($0) }
        if ansi.count == 16 { terminalView.installColors(ansi) }
        let background = Self.uiColor(theme.background)
        terminalView.nativeForegroundColor = Self.uiColor(theme.foreground)
        terminalView.nativeBackgroundColor = background
        backgroundColor = background
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

    public var isSelecting: Bool { terminalView.selectionActive }

    /// Copies through `onClipboardWrite` and ends the selection. Rows the emulator wrapped
    /// come out as one line; only real line breaks in the output stay.
    public func copySelection() {
        guard isSelecting else { return }
        terminalView.copy(nil)
    }

    public func selectAll() {
        terminalView.selectAll()
    }

    /// Key bar modifiers applied to the next typed key. The key bar installs its own.
    public var latch: ModifierLatch? {
        didSet {
            terminalView.latchedInput = latch.map { latch in
                // Longer input (autocorrect, dictation) goes to SwiftTerm untouched.
                { [weak latch, weak self] text in
                    guard let self, let stroke = latch?.stroke(forTyped: text) else { return nil }
                    return bytes(for: stroke)
                }
            }
            terminalView.latchedBackspace = latch.map { latch in
                { [weak latch, weak self] in
                    guard let self, let latch, latch.isActive else { return nil }
                    return bytes(for: latch.consume(KeyStroke(.backspace)))
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

    /// The live screen, or nil while scrolled back: then the rows shown are not the live ones.
    func liveScreen() -> TerminalScreen? {
        guard let input = terminalView.terminalInputStateSnapshot(), !isScrolledBack(input) else { return nil }
        return TerminalScreen(rows: terminalView.visibleRowsText(0..<input.dimensions.rows), isAlternate: input.isAlternateBuffer)
    }

    private func isScrolledBack(_ input: TerminalInputStateSnapshot) -> Bool {
        !input.isAlternateBuffer && terminalView.canScroll && terminalView.scrollPosition < 1
    }

    /// Rows changed. One read follows `screenReadInterval` later, whatever the frame rate, and
    /// never inside the delegate call or a frame. The rows are copied off the main thread
    /// (SwiftTerm's `visibleRowsText` takes its own lock); only the comparison runs here.
    func screenChanged() {
        guard !screenReadPending else { return }
        screenReadPending = true
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: Self.screenReadInterval)
            guard let self else { return }
            defer { screenReadPending = false }
            guard outputFeed.isActive,
                  let input = terminalView.terminalInputStateSnapshot(), !isScrolledBack(input) else { return }
            let source = RowSource(view: terminalView)
            let rows = await Task.detached(priority: .utility) {
                source.view.visibleRowsText(0..<input.dimensions.rows)
            }.value
            readScreen = TerminalScreen(rows: rows, isAlternate: input.isAlternateBuffer)
            outputFeed.screenChanged()
            readScreen = nil
        }
    }

    /// "Cursor line, …", or nil while the cursor is hidden or the view is scrolled back.
    /// VoiceOver asks after every frame, so one read serves for `screenReadInterval`.
    func cursorLine() -> String? {
        if let cache = cursorLineCache, cache.age() < Self.screenReadInterval { return cache.value }
        var value: String?
        if !terminalView.cursorHidden.load(ordering: .relaxed),
           let input = terminalView.terminalInputStateSnapshot(), !isScrolledBack(input) {
            let state = terminalView.terminalStateSnapshot()
            let row = state.visibleRows.first { $0.row == state.cursor.row }?.text
            value = TerminalAccessibility.cursorLine(row, words: accessibilityWords)
        }
        cursorLineCache = (value, stopwatch(ContinuousClock()))
        return value
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
            let target = self.cell(at: point)
            for _ in 0..<count {
                terminalView.sendMouseEvent(button: up ? 4 : 5, release: false, col: target.col, row: target.row)
            }
        case .local(let lines):
            if lines > 0 { terminalView.scrollUp(lines: lines) } else { terminalView.scrollDown(lines: -lines) }
        }
    }

    /// The smallest grid SwiftTerm keeps. Its view reports the counts it measured from the
    /// frame, which are 0 for a frame narrower than a cell or shorter than a row, while the
    /// emulator itself never goes below these; the remote gets what the emulator holds.
    nonisolated static let minimumCols = 2
    nonisolated static let minimumRows = 1

    func reportSize(cols: Int, rows: Int) {
        let cols = max(cols, Self.minimumCols)
        let rows = max(rows, Self.minimumRows)
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
        pasteFromClipboard()
    }

    /// Text pastes as text. A pasteboard holding only an image hands its bytes to
    /// `onPasteImage`, keeping the original encoding when the pasteboard has one.
    /// A pasteboard holding only a link (some apps' Copy Link) pastes the link's exact text.
    public func pasteFromClipboard(_ board: UIPasteboard = .general) {
        if let text = board.string ?? board.url?.absoluteString {
            paste(text)
            return
        }
        guard let onPasteImage, board.hasImages else { return }
        let encoded = ["public.png", "public.jpeg", "public.heic"].lazy.compactMap { board.data(forPasteboardType: $0) }.first
        if let data = encoded ?? board.image?.pngData() {
            onPasteImage(data)
        }
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


    /// SwiftTerm's input queue keeps engine writes ordered with keyboard and IME input.
    /// Every engine write is the user's: key bar keys, pastes, gestures and typed text.
    func write(_ bytes: [UInt8]) {
        guard !bytes.isEmpty else { return }
        outputFeed.noteInput()
        terminalView.send(data: bytes[...])
    }

    // MARK: Gesture support

    func cell(at point: CGPoint) -> GridCell {
        let cell = cellSize
        let dimensions = terminalView.terminalDimensions
        guard cell.width > 0, cell.height > 0 else { return GridCell(col: 0, row: 0) }
        let col = min(max(Int(point.x / cell.width), 0), dimensions.cols - 1)
        let row = min(max(Int((point.y - terminalView.bounds.minY) / cell.height), 0), dimensions.rows - 1)
        return GridCell(col: col, row: row)
    }

    /// Reads the logical line under the point, joining rows the emulator wrapped, so a URL
    /// broken across rows is found whole.
    func bareLink(at point: CGPoint) -> URL? {
        let target = cell(at: point)
        let rows = terminalView.terminalStateSnapshot().visibleRows
        guard let index = rows.firstIndex(where: { $0.row == target.row }) else { return nil }
        var first = index
        while first > 0, rows[first].isWrapped { first -= 1 }
        var last = index
        while last + 1 < rows.count, rows[last + 1].isWrapped { last += 1 }
        var line = ""
        var offset = 0
        for i in first...last {
            if i == index {
                // Wide glyphs fill two cells but are one character; their second cell is 0 wide.
                offset = line.count + rows[i].cellWidths.prefix(target.col).count { $0 != 0 }
            }
            line += rows[i].text
        }
        return BareLink.url(in: line, at: offset)
    }

    // MARK: Conversions

    static func font(for settings: TerminalSettings) -> UIFont {
        if let name = settings.fontName, let font = UIFont(name: name, size: settings.fontSize) {
            return font
        }
        return UIFont.monospacedSystemFont(ofSize: settings.fontSize, weight: .regular)
    }

    private static func cursorStyle(for settings: TerminalSettings) -> CursorStyle {
        switch (settings.cursor, settings.cursorBlinks) {
        // Always steady: the terminal view adds its own one second step while it has focus.
        case (.block, _): .steadyBlock
        case (.underline, _): .steadyUnderline
        case (.bar, _): .steadyBar
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

/// Carries the view to a background read. Only `visibleRowsText`, which SwiftTerm makes safe
/// from any thread, is called there.
private struct RowSource: @unchecked Sendable {
    let view: TogoTerminalView
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

    func rangeChanged(source: TerminalView, startY: Int, endY: Int) {
        engine?.screenChanged()
    }
}
