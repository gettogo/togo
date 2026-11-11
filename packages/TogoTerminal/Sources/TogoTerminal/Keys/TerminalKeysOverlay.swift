import GameController
import UIKit

/// What the host knows about the session and the window that the keys need.
public struct TerminalKeysInput: Equatable, Sendable {
    /// The helper's agent id, for Mode, Stop and the agent's own shortcuts.
    public var agent: String?
    /// The system draws the pane's bar vertically, so the controller is a bar item.
    public var verticalBar = false
    /// The session's "Keep keys on screen".
    public var keep = false
    public var promptKeys: [PromptKey] = []
    public var config = TerminalKeysConfig()
    /// Zen: the palette's Attach and Dictate step aside.
    public var hidesInputTools = false

    public init() {}
}

/// The terminal's keys, installed on the terminal controller's view: the column beside the
/// grid, the row above the keyboard, and the keys both send. Nothing here is an input
/// accessory: the row and column are pinned to the keyboard layout guide, so they ride a
/// floating keyboard and stay under a hardware one, where an accessory would vanish.
@MainActor public final class TerminalKeysOverlay {
    public let latch = ModifierLatch()
    public let column = KeyColumnView()
    public let row = InputsRowView()
    public var input = TerminalKeysInput() {
        didSet { if input != oldValue { refresh() } }
    }
    public private(set) var plan = TerminalKeysPlan(column: nil, row: [], controllerInBar: false)
    public var onHaptic: (KeyHaptic) -> Void = { _ in }
    /// Opens the palette from `anchor`; `edit` for the hold.
    public var onPalette: (_ anchor: UIView, _ edit: Bool) -> Void = { _, _ in }
    public var onPrompt: (PromptKey) -> Void = { _ in }
    public var onAttach: () -> Void = {}
    public var onDictate: () -> Void = {}
    /// The situation the plan was derived from, for the host's tests and the dock.
    public private(set) var situation = TerminalKeysSituation()

    private weak var engine: SwiftTermEngine?
    private weak var terminal: TerminalViewController?
    private weak var host: UIView?
    private let presence: KeyboardPresence
    private let rate: RepeatRate
    private let repeater: ChordRepeater
    #if DEBUG
    private var touchTrace: TerminalKeyTouchTrace?
    #endif
    private var baseMargins = UIEdgeInsets.zero
    private var columnAboveRow: NSLayoutConstraint?
    private var columnAboveKeyboard: NSLayoutConstraint?
    private var observers: [any NSObjectProtocol] = []
    private var traits: (any UITraitChangeRegistration)?

    public init(engine: SwiftTermEngine, terminal: TerminalViewController, host: UIView, presence: KeyboardPresence = .shared) {
        self.engine = engine
        self.terminal = terminal
        self.host = host
        self.presence = presence
        let rate = RepeatRate()
        self.rate = rate
        repeater = ChordRepeater(timing: { count in rate.delay(beforeRepeat: count) })
        baseMargins = terminal.gridMargins
        repeater.send = { [weak engine] bytes in engine?.write(bytes) }
        latch.softShift = SystemKeyboardShift.live
        latch.onChange = { [weak self] in self?.latchChanged() }
        engine.latch = latch
        engine.inputAccessory = nil
        install()
        wire()
        observe()
        refresh()
    }

    isolated deinit {
        #if DEBUG
        if let touchTrace { touchTrace.view?.removeGestureRecognizer(touchTrace) }
        #endif
        let center = NotificationCenter.default
        for observer in observers { center.removeObserver(observer) }
    }

    // MARK: Layout

    private func install() {
        guard let host else { return }
        row.translatesAutoresizingMaskIntoConstraints = false
        column.translatesAutoresizingMaskIntoConstraints = false
        host.addSubview(row)
        host.addSubview(column)
        let guide = host.keyboardLayoutGuide
        let safe = host.safeAreaLayoutGuide
        let rowLeading = row.leadingAnchor.constraint(equalTo: guide.leadingAnchor, constant: InputsRowView.edgeInset)
        rowLeading.priority = .defaultHigh
        let rowTrailing = row.trailingAnchor.constraint(equalTo: guide.trailingAnchor, constant: -InputsRowView.edgeInset)
        rowTrailing.priority = .defaultHigh
        let columnBesideKeyboard = column.trailingAnchor.constraint(equalTo: guide.trailingAnchor)
        columnBesideKeyboard.priority = .defaultHigh
        let aboveRow = column.bottomAnchor.constraint(equalTo: row.topAnchor, constant: -12)
        let aboveKeyboard = column.bottomAnchor.constraint(equalTo: guide.topAnchor, constant: -12)
        columnAboveRow = aboveRow
        columnAboveKeyboard = aboveKeyboard
        NSLayoutConstraint.activate([
            row.bottomAnchor.constraint(equalTo: guide.topAnchor, constant: -6),
            row.heightAnchor.constraint(equalToConstant: InputsRowView.height),
            row.leadingAnchor.constraint(greaterThanOrEqualTo: safe.leadingAnchor, constant: InputsRowView.edgeInset),
            row.trailingAnchor.constraint(lessThanOrEqualTo: safe.trailingAnchor, constant: -InputsRowView.edgeInset),
            rowLeading, rowTrailing,
            column.trailingAnchor.constraint(lessThanOrEqualTo: safe.trailingAnchor, constant: -8),
            columnBesideKeyboard,
            column.topAnchor.constraint(greaterThanOrEqualTo: safe.topAnchor, constant: 8),
            aboveKeyboard,
        ])
    }

    private func wire() {
        column.onKey = { [weak self] in self?.press($0) }
        row.onKey = { [weak self] in self?.press($0) }
        row.onPrompt = { [weak self] in self?.onPrompt($0) }
        row.onShortcut = { [weak self] in self?.perform($0) }
        row.onRepeatingKey = { [weak self] ref, isHeld in self?.pressRepeating(ref, isHeld: isHeld) }
        row.onRepeatEnded = { [weak self] in self?.repeater.stop() }
        attach(column.controller)
    }

    /// Wires a controller face to this terminal: the column's own, or the bar's capsule while
    /// this terminal is the focused one.
    public func attach(_ face: ControllerFace) {
        face.flicks = input.config.controller
        face.onArrow = { [weak self, weak face] direction in
            guard let self, let face else { return }
            rate.flick = true
            rate.fast = face.isFast
            pressRepeating(direction.stroke, key: direction.rawValue.hashValue) { [weak self, weak face] in
                guard let self, let face else { return false }
                rate.fast = face.isFast
                return face.isTouching
            }
        }
        face.onFlickEnded = { [weak self] in self?.repeater.stop() }
        face.onTap = { [weak self, weak face] in
            guard let self, let face else { return }
            onHaptic(.modifier)
            onPalette(face, false)
        }
        face.onHoldBegan = { [weak self] in self?.onHaptic(.modifier) }
        face.onHoldEnded = { [weak self, weak face] in
            guard let self, let face else { return }
            onPalette(face, true)
        }
    }

    private func observe() {
        let center = NotificationCenter.default
        for name in [
            UIResponder.keyboardWillChangeFrameNotification, UIResponder.keyboardWillHideNotification,
            UIResponder.keyboardDidShowNotification, TerminalFocus.didChange,
            Notification.Name.GCKeyboardDidConnect, .GCKeyboardDidDisconnect,
        ] {
            observers.append(center.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                // The responder and the guide settle after the notification.
                Task { @MainActor in self?.refresh() }
            })
        }
        traits = host?.registerForTraitChanges([UITraitVerticalSizeClass.self]) { [weak self] (_: UIView, _) in
            self?.refresh()
        }
    }

    /// A software keyboard is up and this terminal holds it.
    private var typing: Bool {
        guard let engine, let host, engine.view.isFirstResponder, !presence.hardwareKeyboardConnected else { return false }
        return host.keyboardLayoutGuide.layoutFrame.height > 1
    }

    /// Derives the plan again from what the system reports now.
    public func refresh() {
        guard let host, let terminal else { return }
        #if DEBUG
        if let window = host.window {
            let trace = touchTrace ?? TerminalKeyTouchTrace(row: row, column: column)
            touchTrace = trace
            if trace.view !== window { window.addGestureRecognizer(trace) }
        }
        #endif
        var s = TerminalKeysSituation()
        s.verticalBar = input.verticalBar
        s.compactHeight = host.traitCollection.verticalSizeClass == .compact
        s.hardwareKeyboard = presence.hardwareKeyboardConnected
        s.typing = typing
        s.keep = input.keep
        s.promptKeys = input.promptKeys
        s.shortcuts = input.config.shortcuts(for: input.agent)
        s.config = input.config
        s.hasMode = AgentKeys.keys(for: input.agent).mode != nil
        situation = s
        let next = TerminalKeysPlan.plan(s)
        column.controller.flicks = input.config.controller
        guard next != plan || column.isHidden != (next.column == nil) || row.isHidden != next.row.isEmpty else { return }
        plan = next
        let agent = AgentKeys.keys(for: input.agent)
        row.show(next.row, agent: agent, latch: latch)
        row.isHidden = next.row.isEmpty
        if let plannedColumn = next.column {
            column.show(plannedColumn.keys, latch: latch)
        }
        column.isHidden = next.column == nil
        columnAboveRow?.isActive = !next.row.isEmpty
        columnAboveKeyboard?.isActive = next.row.isEmpty
        var margins = baseMargins
        if next.column != nil { margins.right += KeyColumnView.width + 8 }
        if !next.row.isEmpty { margins.bottom += InputsRowView.height + 6 }
        terminal.gridMargins = margins
        host.setNeedsLayout()
    }

    // MARK: Keys

    /// The key `ref` sends for this session, with the agent's Mode and Stop filled in.
    public func stroke(for ref: KeyRef) -> KeyStroke? {
        let agent = AgentKeys.keys(for: input.agent)
        switch ref {
        case .mode: return agent.mode
        case .stop: return agent.stop
        default: return ref.stroke
        }
    }

    /// A tap on a key: latches toggle, tools act, every other key sends.
    public func press(_ ref: KeyRef) {
        #if DEBUG
        TerminalKeyTouchTrace.action(ref)
        #endif
        if let modifier = ref.latch {
            latch.tap(modifier)
            onHaptic(latch.state(of: modifier) == .locked ? .lock : .modifier)
            return
        }
        switch ref {
        case .hideKeyboard:
            repeater.stop()
            if let engine { engine.endEditing() } else { host?.window?.endEditing(true) }
        case .paste:
            engine?.pasteFromClipboard()
        case .attach:
            onAttach()
        case .dictate:
            onDictate()
        default:
            guard let stroke = stroke(for: ref) else { return }
            send(stroke)
            onHaptic(.key)
        }
    }

    /// Locks a modifier outright, for a long press and for VoiceOver's named action.
    public func lock(_ modifier: KeyModifiers, _ on: Bool) {
        if on { latch.lock(modifier) } else { latch.set(modifier, .off) }
        onHaptic(on ? .lock : .modifier)
    }

    public func perform(_ item: KeyBarItem) {
        guard let engine else { return }
        switch item.action {
        case .steps(let steps):
            if steps.count == 1, case .stroke(let stroke) = steps[0] {
                send(stroke)
            } else {
                engine.run(steps)
            }
            onHaptic(.key)
        case .ctrlLatch: press(.ctrl)
        case .altLatch: press(.alt)
        case .shiftLatch: press(.shift)
        case .paste: press(.paste)
        case .hideKeyboard: press(.hideKeyboard)
        case .compose, .attach: press(.attach)
        case .dictate: press(.dictate)
        case .dpad: break
        }
    }

    private func pressRepeating(_ ref: KeyRef, isHeld: @escaping @MainActor () -> Bool) {
        guard let stroke = stroke(for: ref) else { return }
        rate.flick = false
        pressRepeating(stroke, key: ref.hashValue, isHeld: isHeld)
    }

    /// Sends at touch down and repeats until the finger lifts; the first press takes the
    /// latched modifiers, the repeats send the same bytes.
    private func pressRepeating(_ stroke: KeyStroke, key: Int, isHeld: @escaping @MainActor () -> Bool) {
        guard let engine else { return }
        let bytes = engine.bytes(for: latch.consume(stroke))
        engine.write(bytes)
        onHaptic(.key)
        repeater.start(key: key, bytes: bytes, isHeld: isHeld)
    }

    private func send(_ stroke: KeyStroke) {
        engine?.send(latch.consume(stroke))
    }

    private func latchChanged() {
        column.showLatch(latch)
        row.showLatch(latch)
    }
}

/// How the held key repeats: `ArrowRepeat` for a flick, with its fast rate once the finger is
/// far from touchdown; the keyboard's own timing for a held row key.
@MainActor final class RepeatRate {
    var flick = false
    var fast = false

    func delay(beforeRepeat count: Int) -> TimeInterval {
        flick ? ArrowRepeat.delay(beforeRepeat: count, fast: fast) : KeyRepeatTiming.delay(beforeRepeat: count)
    }
}

/// What a key press feels like; the host plays it.
public enum KeyHaptic: Sendable {
    /// A modifier armed or released, the controller held, a prompt's keys arriving.
    case modifier
    /// A modifier locked.
    case lock
    /// A terminal key such as Esc, Tab or an arrow. Repeats are silent.
    case key
}
