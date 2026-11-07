import Foundation

/// Sticky Ctrl, Alt and Shift for the soft keyboard, which has no Ctrl or Alt and whose Shift
/// never reaches the key bar.
///
/// One tap arms a modifier for the next key; a second tap within `doubleTapWindow` locks it
/// until tapped again, and a long press locks it at once. A later second tap disarms it
/// instead, so a mistaken tap is undone by tapping again.
@MainActor public final class ModifierLatch {
    public enum State: Sendable { case off, once, locked }

    public var ctrl: State = .off { didSet { onChange() } }
    public var alt: State = .off { didSet { onChange() } }
    public var shift: State = .off { didSet { onChange() } }
    public var onChange: () -> Void = {}
    /// The system keyboard's own Shift, read when a bar key or Return is pressed. It joins the
    /// latched modifiers for keys the system keyboard cannot shift itself (Tab, arrows, Enter).
    var softShift: SoftShift = .none

    static let doubleTapWindow: TimeInterval = 0.4
    private let now: () -> Date
    private var lastTap: [Int: Date] = [:]

    public init() {
        now = Date.init
    }

    init(now: @escaping () -> Date) {
        self.now = now
    }

    func state(of modifier: KeyModifiers) -> State {
        switch modifier {
        case .ctrl: ctrl
        case .alt: alt
        case .shift: shift
        default: .off
        }
    }

    func set(_ modifier: KeyModifiers, _ state: State) {
        switch modifier {
        case .ctrl: ctrl = state
        case .alt: alt = state
        case .shift: shift = state
        default: break
        }
    }

    func tap(_ modifier: KeyModifiers) {
        let time = now()
        let last = lastTap[modifier.rawValue]
        lastTap[modifier.rawValue] = time
        let next: State = switch state(of: modifier) {
        case .off: .once
        case .once:
            if let last, time.timeIntervalSince(last) <= Self.doubleTapWindow { .locked } else { .off }
        case .locked: .off
        }
        set(modifier, next)
    }

    /// A long press: locked whatever the state was.
    func lock(_ modifier: KeyModifiers) {
        lastTap[modifier.rawValue] = nil
        set(modifier, .locked)
    }

    func tapCtrl() { tap(.ctrl) }
    func tapAlt() { tap(.alt) }
    func tapShift() { tap(.shift) }

    var modifiers: KeyModifiers {
        var mods: KeyModifiers = []
        if ctrl != .off { mods.insert(.ctrl) }
        if alt != .off { mods.insert(.alt) }
        if shift != .off { mods.insert(.shift) }
        return mods
    }

    var isActive: Bool { ctrl != .off || alt != .off || shift != .off }

    /// Applies held modifiers, and the system keyboard's Shift, to one keypress and releases
    /// the one-shot ones.
    func consume(_ stroke: KeyStroke) -> KeyStroke {
        var held = stroke
        if softShift.isShifted() {
            held.modifiers.insert(.shift)
            softShift.release()
        }
        guard isActive else { return held }
        held.modifiers.formUnion(modifiers)
        release()
        return held
    }

    /// The key a typed text stands for when a modifier changes it, or nil to let the text pass
    /// as typed. Only a single typed character takes a modifier; longer input (autocorrect,
    /// dictation, paste from the keyboard) passes through unchanged and leaves the latch armed.
    /// Return with the system keyboard shifted is Shift-Enter, the newline in agent prompts.
    func stroke(forTyped text: String) -> KeyStroke? {
        guard text.count == 1, let c = text.first else { return nil }
        let isReturn = c == "\n" || c == "\r"
        guard isActive || (isReturn && softShift.isShifted()) else { return nil }
        return consume(KeyStroke(isReturn ? .enter : .char(c)))
    }

    /// Applies held modifiers to typed bytes with legacy encoding.
    public func apply(to bytes: [UInt8]) -> [UInt8] {
        guard let text = String(bytes: bytes, encoding: .utf8), let stroke = stroke(forTyped: text) else {
            return bytes
        }
        return KeyEncoder.encode(stroke, applicationCursor: false)
    }

    private func release() {
        if ctrl == .once { ctrl = .off }
        if alt == .once { alt = .off }
        if shift == .once { shift = .off }
    }
}

/// Reads and releases the system keyboard's Shift. Injected so tests and builds without a
/// keyboard stay deterministic.
struct SoftShift: Sendable {
    var isShifted: @MainActor @Sendable () -> Bool
    var release: @MainActor @Sendable () -> Void

    static let none = SoftShift(isShifted: { false }, release: {})
}
