import UIKit

/// The row of terminal keys above the system keyboard.
///
/// Installs itself as the engine's input accessory and its latch as the engine's latch, so a
/// latched Ctrl applies to the next key typed on the system keyboard as well as on the bar.
@MainActor public final class KeyBarView: UIInputView {
    static let rowHeight: CGFloat = 48

    let config: KeyBarConfig
    let latch = ModifierLatch()
    let presence = KeyboardPresence()
    private(set) var isShowingDPad = false
    /// Called by a `.compose` item. The bar has no compose UI of its own; the host shows it.
    public var onCompose: (() -> Void)?

    private weak var engine: SwiftTermEngine?
    private let stack = UIStackView()
    private let dpad = DPadView()
    private var buttons: [UUID: UIButton] = [:]
    private let haptics = UISelectionFeedbackGenerator()

    public init(config: KeyBarConfig, engine: SwiftTermEngine) {
        self.config = config
        self.engine = engine
        super.init(frame: CGRect(x: 0, y: 0, width: 320, height: Self.rowHeight), inputViewStyle: .keyboard)
        allowsSelfSizing = true
        // Past this size the words no longer fit a bar that shares the screen with a terminal.
        maximumContentSizeCategory = .accessibilityMedium
        buildRow()
        latch.onChange = { [weak self] in self?.refreshLatchButtons() }
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

    func perform(_ item: KeyBarItem) {
        guard let engine else { return }
        switch item.action {
        case .steps(let steps):
            if steps.count == 1, case .stroke(let stroke) = steps[0] {
                send(stroke)
            } else {
                engine.run(steps)
            }
        case .ctrlLatch:
            latch.tapCtrl()
            haptics.selectionChanged()
        case .altLatch:
            latch.tapAlt()
            haptics.selectionChanged()
        case .paste:
            engine.pasteFromClipboard()
        case .hideKeyboard:
            engine.endEditing()
        case .dpad:
            isShowingDPad.toggle()
            refreshDPad()
        case .compose:
            onCompose?()
        }
    }

    func button(for item: KeyBarItem) -> UIButton? {
        buttons[item.id]
    }

    private func send(_ stroke: KeyStroke) {
        engine?.send(latch.consume(stroke))
    }

    private func buildRow() {
        let scroll = UIScrollView()
        scroll.showsHorizontalScrollIndicator = false
        scroll.alwaysBounceHorizontal = true
        scroll.translatesAutoresizingMaskIntoConstraints = false
        stack.axis = .horizontal
        stack.alignment = .center
        stack.spacing = 4
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(scroll)
        scroll.addSubview(stack)
        NSLayoutConstraint.activate([
            scroll.leadingAnchor.constraint(equalTo: safeAreaLayoutGuide.leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: safeAreaLayoutGuide.trailingAnchor),
            scroll.topAnchor.constraint(equalTo: topAnchor),
            scroll.bottomAnchor.constraint(equalTo: bottomAnchor),
            stack.leadingAnchor.constraint(equalTo: scroll.contentLayoutGuide.leadingAnchor, constant: 8),
            stack.trailingAnchor.constraint(equalTo: scroll.contentLayoutGuide.trailingAnchor, constant: -8),
            stack.topAnchor.constraint(equalTo: scroll.contentLayoutGuide.topAnchor),
            stack.bottomAnchor.constraint(equalTo: scroll.contentLayoutGuide.bottomAnchor),
            stack.heightAnchor.constraint(equalTo: scroll.frameLayoutGuide.heightAnchor),
        ])
        for item in config.items {
            let button = KeyBarButton.make(title: item.title, symbol: item.symbol)
            button.addAction(UIAction { [weak self] _ in self?.perform(item) }, for: .touchUpInside)
            buttons[item.id] = button
            stack.addArrangedSubview(button)
            addLockActions(for: item, to: button)
        }
        if let firstArrow = config.items.firstIndex(where: isArrow) {
            stack.insertArrangedSubview(dpad, at: firstArrow)
        }
        dpad.isHidden = true
        refreshLatchButtons()
    }

    /// Locking takes two quick taps, which VoiceOver, Switch Control and Voice Control cannot
    /// produce, so lock and unlock are also offered as named actions.
    private func addLockActions(for item: KeyBarItem, to button: UIButton) {
        let set: (ModifierLatch.State) -> Void
        switch item.action {
        case .ctrlLatch: set = { [weak self] in self?.latch.ctrl = $0 }
        case .altLatch: set = { [weak self] in self?.latch.alt = $0 }
        default: return
        }
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
    }

    private func refreshVisibility() {
        isHidden = presence.hardwareKeyboardConnected
        invalidateIntrinsicContentSize()
    }

    private func refreshLatchButtons() {
        for item in config.items {
            let state: ModifierLatch.State
            switch item.action {
            case .ctrlLatch: state = latch.ctrl
            case .altLatch: state = latch.alt
            default: continue
            }
            buttons[item.id].map { KeyBarButton.show(state, on: $0) }
        }
    }
}

@MainActor enum KeyBarButton {
    static func make(title: String, symbol: String?) -> UIButton {
        var configuration = UIButton.Configuration.gray()
        configuration.cornerStyle = .medium
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 6, leading: 12, bottom: 6, trailing: 12)
        if let symbol {
            configuration.image = UIImage(systemName: symbol)
            configuration.preferredSymbolConfigurationForImage = UIImage.SymbolConfiguration(textStyle: .callout)
        } else {
            configuration.title = title
            configuration.titleTextAttributesTransformer = UIConfigurationTextAttributesTransformer { attributes in
                var attributes = attributes
                attributes.font = UIFont.preferredFont(forTextStyle: .callout)
                return attributes
            }
        }
        let button = UIButton(configuration: configuration)
        button.accessibilityLabel = title
        button.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            button.widthAnchor.constraint(greaterThanOrEqualToConstant: 44),
            button.heightAnchor.constraint(greaterThanOrEqualToConstant: 44),
        ])
        return button
    }

    /// Armed is tinted; locked is filled, so the two read apart without colour alone.
    static func show(_ state: ModifierLatch.State, on button: UIButton) {
        let title = button.configuration?.title
        var configuration: UIButton.Configuration = switch state {
        case .off: .gray()
        case .once: .tinted()
        case .locked: .filled()
        }
        configuration.cornerStyle = .medium
        configuration.contentInsets = button.configuration?.contentInsets ?? .zero
        configuration.title = title
        configuration.image = button.configuration?.image
        configuration.preferredSymbolConfigurationForImage = button.configuration?.preferredSymbolConfigurationForImage
        configuration.titleTextAttributesTransformer = button.configuration?.titleTextAttributesTransformer
        button.configuration = configuration
        button.isSelected = state != .off
        button.accessibilityValue = switch state {
        case .off: "Off"
        case .once: "On for next key"
        case .locked: "Locked"
        }
    }
}
