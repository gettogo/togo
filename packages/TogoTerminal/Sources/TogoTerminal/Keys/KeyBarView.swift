import UIKit

/// The row of terminal keys above the system keyboard.
///
/// Installs itself as the engine's input accessory and its latch as the engine's latch, so a
/// latched Ctrl applies to the next key typed on the system keyboard as well as on the bar.
@MainActor public final class KeyBarView: UIInputView {
    /// Each key fills the row's height as a 44 point target and draws a compact face inside
    /// it, so the bar reads slim while every key is as easy to hit as a system control.
    static let rowHeight: CGFloat = 44
    /// The gap between faces. Each key also takes touches in half the gap on either side, so
    /// a key's target is its width plus this.
    static let keySpacing: CGFloat = 4
    static let rowInset: CGFloat = 6
    /// With the shared gap, the narrowest key still takes a 44 point wide touch.
    static let minimumKeyWidth: CGFloat = 44 - keySpacing

    let config: KeyBarConfig
    let latch = ModifierLatch()
    let presence = KeyboardPresence()
    private(set) var isShowingDPad = false
    /// Called by a `.compose` item. The bar has no compose UI of its own; the host shows it.
    public var onCompose: (() -> Void)?
    /// Called by a `.dictate` item. The host opens its compose bar and listens there.
    public var onDictate: (() -> Void)?
    /// Hides the Compose and Dictate keys while the host keeps its compose bar away, as in a
    /// mode that shows the terminal alone.
    public var hidesCompose = false {
        didSet { refreshComposeKeys() }
    }

    private weak var engine: SwiftTermEngine?
    private let scroll = UIScrollView()
    /// The keys in order, the pad among them where the first arrow is.
    private var keys: [UIView] = []
    private let dpad = DPadView()
    private var buttons: [UUID: UIButton] = [:]
    /// Played through the host's haptics, so the app keeps one place that vibrates.
    public var onHaptic: ((KeyHaptic) -> Void)?
    /// Held arrows, Tab, Back Tab and Backspace repeat like the system keyboard.
    let repeater = ChordRepeater(timing: KeyRepeatTiming.delay(beforeRepeat:))
    static let lockPressDuration: TimeInterval = 0.45

    public init(config: KeyBarConfig, engine: SwiftTermEngine) {
        self.config = config
        self.engine = engine
        // A plain input view: the keyboard style draws a black backdrop behind the row on
        // current systems. The row takes the terminal's own background instead, so the bar
        // reads as the terminal's last strip above the keyboard.
        super.init(frame: CGRect(x: 0, y: 0, width: 320, height: Self.rowHeight), inputViewStyle: .default)
        allowsSelfSizing = true
        autoresizingMask = [.flexibleWidth]
        backgroundColor = engine.backgroundColor
        // Past this size the words no longer fit a bar that shares the screen with a terminal.
        maximumContentSizeCategory = .accessibilityMedium
        buildRow()
        latch.onChange = { [weak self] in self?.refreshLatchButtons() }
        NotificationCenter.default.addObserver(
            self, selector: #selector(differentiateWithoutColorChanged),
            name: UIAccessibility.differentiateWithoutColorDidChangeNotification, object: nil
        )
        latch.softShift = SystemKeyboardShift.live
        repeater.send = { [weak engine] bytes in engine?.write(bytes) }
        presence.onChange = { [weak self] _ in self?.refreshVisibility() }
        dpad.onStroke = { [weak self] in self?.send($0) }
        engine.latch = latch
        engine.inputAccessory = self
        refreshVisibility()
    }

    required init?(coder: NSCoder) {
        nil
    }

    public override var intrinsicContentSize: CGSize {
        guard !presence.hardwareKeyboardConnected else { return CGSize(width: UIView.noIntrinsicMetric, height: 0) }
        let height = isShowingDPad ? DPadView.side * 3 + 8 : Self.rowHeight
        return CGSize(width: UIView.noIntrinsicMetric, height: height)
    }

    /// The theme can change while the keyboard is up.
    public override func layoutSubviews() {
        super.layoutSubviews()
        if let color = engine?.backgroundColor, backgroundColor != color { backgroundColor = color }
        layoutKeys()
    }

    /// Keys that fit the width share it out, every key a little wider. Keys that do not are
    /// cut into pages of whole keys, each page filling the width, and the row pages between
    /// them, so a key is never left half on screen when the row comes to rest.
    private func layoutKeys() {
        scroll.frame = bounds
        let shown = keys.filter { !$0.isHidden }
        let natural = shown.map(naturalWidth)
        let pages = KeyRowLayout.pages(
            widths: natural, available: bounds.width - Self.rowInset * 2, spacing: Self.keySpacing
        )
        for (index, page) in pages.enumerated() {
            var x = CGFloat(index) * bounds.width + Self.rowInset
            for slot in page {
                let key = shown[slot.index]
                let height = key === dpad ? DPadView.side * 3 : min(bounds.height, Self.rowHeight)
                key.frame = CGRect(x: x, y: (bounds.height - height) / 2, width: slot.width, height: height)
                x += slot.width + Self.keySpacing
            }
        }
        let width = bounds.width * CGFloat(max(1, pages.count))
        if scroll.contentSize.width != width || scroll.contentSize.height != bounds.height {
            scroll.contentSize = CGSize(width: width, height: bounds.height)
        }
        scroll.isPagingEnabled = pages.count > 1
        scroll.alwaysBounceHorizontal = pages.count > 1
        // A rotation or fold can leave the row between pages; land on the nearest whole one.
        if pages.count > 1, bounds.width > 0, !scroll.isDragging, !scroll.isDecelerating {
            let page = (scroll.contentOffset.x / bounds.width).rounded()
            let x = min(max(0, page), CGFloat(pages.count - 1)) * bounds.width
            if scroll.contentOffset.x != x { scroll.contentOffset.x = x }
        } else if pages.count <= 1, scroll.contentOffset.x != 0 {
            scroll.contentOffset.x = 0
        }
    }

    private func naturalWidth(_ key: UIView) -> CGFloat {
        if key === dpad { return DPadView.side * 3 }
        let size = key.sizeThatFits(CGSize(width: CGFloat.greatestFiniteMagnitude, height: bounds.height))
        return max(Self.minimumKeyWidth, ceil(size.width))
    }

    func perform(_ item: KeyBarItem) {
        guard let engine else { return }
        switch item.action {
        case .steps(let steps):
            if steps.count == 1, case .stroke(let stroke) = steps[0] {
                send(stroke)
            } else {
                engine.run(steps)
            }
        case .ctrlLatch, .altLatch, .shiftLatch:
            guard let modifier = Self.modifier(for: item.action) else { return }
            latch.tap(modifier)
            onHaptic?(latch.state(of: modifier) == .locked ? .lock : .modifier)
        case .paste:
            engine.pasteFromClipboard()
        case .hideKeyboard:
            engine.endEditing()
        case .dpad:
            isShowingDPad.toggle()
            refreshDPad()
        case .compose:
            onCompose?()
        case .dictate:
            onDictate?()
        }
    }

    func button(for item: KeyBarItem) -> UIButton? {
        buttons[item.id]
    }

    private func send(_ stroke: KeyStroke) {
        engine?.send(latch.consume(stroke))
    }

    static func modifier(for action: KeyAction) -> KeyModifiers? {
        switch action {
        case .ctrlLatch: .ctrl
        case .altLatch: .alt
        case .shiftLatch: .shift
        default: nil
        }
    }

    /// A key that repeats while held: a lone cursor, Tab or delete key, whatever its modifiers.
    static func repeatingStroke(_ item: KeyBarItem) -> KeyStroke? {
        guard case .steps(let steps) = item.action, steps.count == 1, case .stroke(let stroke) = steps[0] else {
            return nil
        }
        let repeating: [TerminalKey] = [.up, .down, .left, .right, .tab, .backspace, .delete, .pageUp, .pageDown]
        return repeating.contains(stroke.key) ? stroke : nil
    }

    /// Sends the key at touch down, as the system keyboard does, and repeats it until the
    /// finger lifts. The first press takes any latched modifiers; the repeats send the same.
    func press(_ item: KeyBarItem, stroke: KeyStroke, isHeld: @escaping @MainActor () -> Bool) {
        guard let engine else { return }
        let bytes = engine.bytes(for: latch.consume(stroke))
        engine.write(bytes)
        onHaptic?(.key)
        repeater.start(key: item.id.hashValue, bytes: bytes, isHeld: isHeld)
    }

    private func addActions(for item: KeyBarItem, to button: UIButton) {
        if let stroke = Self.repeatingStroke(item) {
            button.addAction(UIAction { [weak self, weak button] _ in
                self?.press(item, stroke: stroke, isHeld: { button?.isTracking ?? false })
            }, for: .touchDown)
            button.addAction(UIAction { [weak self] _ in self?.repeater.stop() },
                             for: [.touchUpInside, .touchUpOutside, .touchCancel])
            return
        }
        button.addAction(UIAction { [weak self] _ in
            guard let self else { return }
            if case .steps = item.action { onHaptic?(.key) }
            perform(item)
        }, for: .touchUpInside)
        guard let modifier = Self.modifier(for: item.action) else { return }
        let hold = UILongPressGestureRecognizer(target: self, action: #selector(lockHeld(_:)))
        hold.minimumPressDuration = Self.lockPressDuration
        hold.name = String(modifier.rawValue)
        button.addGestureRecognizer(hold)
    }

    /// A long press on a modifier locks it, the quicker way for a run of chords.
    @objc private func lockHeld(_ gesture: UILongPressGestureRecognizer) {
        guard gesture.state == .began, let raw = gesture.name.flatMap(Int.init) else { return }
        latch.lock(KeyModifiers(rawValue: raw))
        onHaptic?(.lock)
    }

    private func buildRow() {
        scroll.showsHorizontalScrollIndicator = false
        scroll.backgroundColor = .clear
        scroll.contentInsetAdjustmentBehavior = .never
        scroll.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        addSubview(scroll)
        for item in config.items {
            let button = KeyBarButton.make(title: item.title, symbol: item.symbol, slim: true)
            (button as? KeyTargetButton)?.horizontalHitOutset = Self.keySpacing / 2
            button.accessibilityLabel = KeyBarButton.spokenTitle(item.title)
            addActions(for: item, to: button)
            buttons[item.id] = button
            keys.append(button)
            scroll.addSubview(button)
            addLockActions(for: item, to: button)
        }
        if let firstArrow = config.items.firstIndex(where: isArrow) {
            keys.insert(dpad, at: firstArrow)
            scroll.addSubview(dpad)
        }
        dpad.isHidden = true
        refreshLatchButtons()
    }

    /// Locking takes two quick taps, which VoiceOver, Switch Control and Voice Control cannot
    /// produce, so lock and unlock are also offered as named actions.
    private func addLockActions(for item: KeyBarItem, to button: UIButton) {
        guard let modifier = Self.modifier(for: item.action) else { return }
        let set: (ModifierLatch.State) -> Void = { [weak self] in self?.latch.set(modifier, $0) }
        button.accessibilityCustomActions = [
            UIAccessibilityCustomAction(name: "Lock") { _ in set(.locked); return true },
            UIAccessibilityCustomAction(name: "Unlock") { _ in set(.off); return true },
        ]
    }

    private func isArrow(_ item: KeyBarItem) -> Bool {
        guard case .steps(let steps) = item.action, steps.count == 1, case .stroke(let stroke) = steps[0],
              stroke.modifiers.isEmpty else { return false }
        return [.up, .down, .left, .right].contains(stroke.key)
    }

    private func refreshDPad() {
        let hasArrows = config.items.contains(where: isArrow)
        dpad.isHidden = !(isShowingDPad && hasArrows)
        for item in config.items where isArrow(item) {
            buttons[item.id]?.isHidden = isShowingDPad
        }
        invalidateIntrinsicContentSize()
        setNeedsLayout()
    }

    private func refreshComposeKeys() {
        for item in config.items where item.action == .compose || item.action == .dictate {
            buttons[item.id]?.isHidden = hidesCompose
        }
        setNeedsLayout()
    }

    private func refreshVisibility() {
        isHidden = presence.hardwareKeyboardConnected
        invalidateIntrinsicContentSize()
    }

    @objc private func differentiateWithoutColorChanged() {
        refreshLatchButtons()
    }

    private func refreshLatchButtons() {
        for item in config.items {
            guard let modifier = Self.modifier(for: item.action) else { continue }
            buttons[item.id].map { KeyBarButton.show(latch.state(of: modifier), on: $0) }
        }
    }
}

/// What a key bar press feels like; the host plays it.
public enum KeyHaptic: Sendable {
    /// A modifier armed or released.
    case modifier
    /// A modifier locked.
    case lock
    /// A terminal key such as Esc, ^C, Tab or an arrow. Repeats are silent.
    case key
}
