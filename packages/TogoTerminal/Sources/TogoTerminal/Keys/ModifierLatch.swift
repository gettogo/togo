import Foundation

/// Sticky Ctrl and Alt for the soft keyboard, which has neither.
///
/// One tap arms a modifier for the next key; a second tap within `doubleTapWindow` locks it
/// until tapped again. A later second tap disarms it instead, so a mistaken tap is undone by
/// tapping again.
@MainActor public final class ModifierLatch {
    public enum State: Sendable { case off, once, locked }

    public var ctrl: State = .off { didSet { onChange() } }
    public var alt: State = .off { didSet { onChange() } }
    public var onChange: () -> Void = {}

    static let doubleTapWindow: TimeInterval = 0.4
    private let now: () -> Date
    private var lastTap: (ctrl: Date?, alt: Date?) = (nil, nil)

    public init() {
        now = Date.init
    }

    init(now: @escaping () -> Date) {
        self.now = now
    }

    func tapCtrl() {
        ctrl = next(ctrl, lastTap: &lastTap.ctrl)
    }

    func tapAlt() {
        alt = next(alt, lastTap: &lastTap.alt)
    }

    private func next(_ state: State, lastTap: inout Date?) -> State {
        let time = now()
        defer { lastTap = time }
        switch state {
        case .off: return .once
        case .once:
            guard let last = lastTap, time.timeIntervalSince(last) <= Self.doubleTapWindow else { return .off }
            return .locked
        case .locked: return .off
        }
    }

    var modifiers: KeyModifiers {
        var mods: KeyModifiers = []
        if ctrl != .off { mods.insert(.ctrl) }
        if alt != .off { mods.insert(.alt) }
        return mods
    }

    var isActive: Bool { ctrl != .off || alt != .off }

    /// Applies held modifiers to one keypress and releases the one-shot ones.
    func consume(_ stroke: KeyStroke) -> KeyStroke {
        guard isActive else { return stroke }
        var held = stroke
        held.modifiers.formUnion(modifiers)
        release()
        return held
    }

    /// Applies held modifiers to typed bytes. Only a single typed character takes a modifier;
    /// longer input (autocorrect, dictation, paste from the keyboard) passes through unchanged
    /// and leaves the latch armed for the next key.
    public func apply(to bytes: [UInt8]) -> [UInt8] {
        guard isActive, let text = String(bytes: bytes, encoding: .utf8), text.count == 1, let c = text.first else {
            return bytes
        }
        let key: TerminalKey = (c == "\n" || c == "\r") ? .enter : .char(c)
        return KeyEncoder.encode(consume(KeyStroke(key)), applicationCursor: false)
    }

    private func release() {
        if ctrl == .once { ctrl = .off }
        if alt == .once { alt = .off }
    }
}
