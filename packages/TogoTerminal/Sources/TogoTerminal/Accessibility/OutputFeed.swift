import UIKit

/// Finds the text on screen that VoiceOver has not heard and hands it to the announcer.
///
/// Rows are compared by content with the screen as it was at the last announcement (or when
/// the feed became active), not by which rows were redrawn: a scroll or a full redraw moves
/// rows without making them new. Each cleaned row counts once per time it appears, so a line
/// printed again is new as long as the screen holds more copies of it than before.
///
/// Some new text is still not read:
/// - A row that changes in place only in its counters or status glyphs (an agent's timer and
///   token count, a progress percentage, a dashboard's numbers) is ticking. Rows with its
///   `tickKey` are never read and never hold back the quiet spell.
/// - Up to two rows edited in place within a second of the user's own input are their typing
///   echoed back; VoiceOver already spoke the keys.
/// - Switching to or from the alternate screen (vim, less, htop, a full-screen agent) takes
///   the new screen as heard. Inside it, new rows are read as anywhere else.
///
/// Active only while VoiceOver runs, the terminal is focused and the setting is on. Output
/// that arrives while inactive is never read: becoming active takes the current screen as heard.
@MainActor
public final class OutputFeed {
    public let announcer: OutputAnnouncer
    /// The live screen; nil when there is none to read.
    public var screen: () -> TerminalScreen? = { nil }
    public var voiceOverRunning: () -> Bool = { false }
    public var focused: () -> Bool = { false }
    /// Told when the feed starts or stops listening, so the view reports changes only then.
    public var onActiveChange: (Bool) -> Void = { _ in }
    /// The `Read new output` setting.
    public var enabled = true {
        didSet { refresh() }
    }
    public private(set) var isActive = false

    /// How long after a keystroke a small in-place edit counts as its echo.
    static let echoWindow: Duration = .seconds(1)
    /// Rows a keystroke's echo can touch: the input row and the one it wraps onto.
    static let echoRows = 2

    private let clock: any Clock<Duration>
    /// Cleaned rows already heard, counted; nil until a baseline is taken.
    private var heard: [String: Int]?
    private var tickingKeys: Set<String> = []
    /// Cleaned rows of the previous read.
    private var last: [String] = []
    private var lastAlternate = false
    private var sinceInput: (() -> Duration)?
    private var observers: [any NSObjectProtocol] = []

    public init(announcer: OutputAnnouncer, clock: any Clock<Duration> = ContinuousClock()) {
        self.announcer = announcer
        self.clock = clock
        announcer.isActive = { [weak self] in self?.isActive ?? false }
        announcer.didAnnounce = { [weak self] in self?.markHeard() }
    }

    isolated deinit {
        observers.forEach(NotificationCenter.default.removeObserver)
    }

    /// Refreshes when VoiceOver starts or stops and when its focus moves.
    public func observeAccessibilityChanges(_ center: NotificationCenter = .default) {
        let names = [UIAccessibility.voiceOverStatusDidChangeNotification, UIAccessibility.elementFocusedNotification]
        observers = names.map { name in
            center.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.refresh() }
            }
        }
    }

    /// Re-reads the conditions; call on VoiceOver, focus and setting changes.
    public func refresh() {
        let active = enabled && voiceOverRunning() && focused()
        guard active != isActive else { return }
        isActive = active
        heard = nil
        if active, let read = screen() {
            rebaseline(read)
        }
        // Text left waiting from an earlier spell is old by now.
        if active { announcer.waiting([]) }
        onActiveChange(active)
    }

    /// The user typed, pasted or pressed a key.
    public func noteInput() {
        sinceInput = stopwatch(clock)
    }

    /// The screen was redrawn.
    public func screenChanged() {
        guard isActive, let read = screen() else { return }
        guard heard != nil, read.isAlternate == lastAlternate else {
            rebaseline(read)
            announcer.waiting([])
            return
        }
        let rows = read.rows.map(TerminalAccessibility.clean)
        guard rows != last else { return }
        absorbEcho(rows)
        announcer.waiting(fresh(rows))
        last = rows
    }

    /// Rows not heard yet, leaving out ticking ones.
    private func fresh(_ rows: [String]) -> [String] {
        var remaining = heard ?? [:]
        let previous = Set(last)
        let current = Set(rows)
        var fresh: [String] = []
        for (index, text) in rows.enumerated() where !text.isEmpty {
            if let count = remaining[text], count > 0 {
                remaining[text] = count - 1
                continue
            }
            // Ticking: the row's old text was replaced where it stood, not scrolled elsewhere,
            // by text that differs only in its counters. Numbered lines scrolling past are new.
            let before = index < last.count ? last[index] : ""
            let replaced = !before.isEmpty && !current.contains(before) && !previous.contains(text)
            // Keys cost a scan per row, so they are only made when one can matter.
            guard replaced || !tickingKeys.isEmpty else {
                fresh.append(text)
                continue
            }
            let key = TerminalAccessibility.tickKey(text)
            if tickingKeys.contains(key) { continue }
            if replaced, !text.hasPrefix(before), TerminalAccessibility.tickKey(before) == key {
                if tickingKeys.count > 256 { tickingKeys.removeAll() }
                tickingKeys.insert(key)
                continue
            }
            fresh.append(text)
        }
        return fresh
    }

    /// Right after input, a small in-place edit is the typing itself: it becomes heard.
    private func absorbEcho(_ rows: [String]) {
        guard let sinceInput, sinceInput() < Self.echoWindow, rows.count == last.count else { return }
        let edited = rows.indices.filter { rows[$0] != last[$0] }
        guard edited.count <= Self.echoRows else { return }
        for index in edited where !last[index].isEmpty && !rows[index].isEmpty {
            heard?[rows[index], default: 0] += 1
        }
    }

    private func rebaseline(_ read: TerminalScreen) {
        last = read.rows.map(TerminalAccessibility.clean)
        lastAlternate = read.isAlternate
        tickingKeys.removeAll()
        markHeard()
    }

    private func markHeard() {
        heard = last.reduce(into: [:]) { counts, text in
            if !text.isEmpty { counts[text, default: 0] += 1 }
        }
    }
}
