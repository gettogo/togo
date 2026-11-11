import UIKit

/// The slim row directly above the keyboard: on a Duo the four fixed keys, then the keys a
/// waiting prompt wants, then the user's shortcuts. It scrolls sideways past its width with a
/// faded trailing edge; the prompt keys lead and never scroll away first. Hide keyboard, when
/// a software keyboard is up, stays at the trailing end past a hairline, outside the scroll,
/// and a swipe down on the row hides the keyboard too. Drawn only while it has something: an
/// empty row is removed, not left blank.
@MainActor public final class InputsRowView: UIView {
    public static let height: CGFloat = 40
    static let faceInset: CGFloat = 5
    static let spacing: CGFloat = 4
    static let edgeInset: CGFloat = 8

    public var onKey: (KeyRef) -> Void = { _ in }
    public var onPrompt: (PromptKey) -> Void = { _ in }
    public var onShortcut: (KeyBarItem) -> Void = { _ in }
    /// A held key that repeats: the row sends the first press and asks the host to repeat it
    /// while `isHeld` says the finger is still down.
    public var onRepeatingKey: (KeyRef, _ isHeld: @escaping @MainActor () -> Bool) -> Void = { _, _ in }
    public var onRepeatEnded: () -> Void = {}

    public private(set) var items: [TerminalKeysPlan.RowItem] = []
    private let glass = GlassBacking(cornerRadius: 14)
    fileprivate let scroll = UIScrollView()
    private let stack = UIStackView()
    private let fade = CAGradientLayer()
    private var latchButtons: [KeyModifiers: UIButton] = [:]
    /// Hide keyboard, past the scrolling keys.
    private let hide = InputsRowView.face(title: KeyRef.hideKeyboard.title, symbol: KeyRef.hideKeyboard.symbol)
    private let hairline = UIView()
    private var scrollToEdge: NSLayoutConstraint?
    private var scrollToHairline: NSLayoutConstraint?

    public override init(frame: CGRect) {
        super.init(frame: frame)
        glass.translatesAutoresizingMaskIntoConstraints = false
        addSubview(glass)
        scroll.showsHorizontalScrollIndicator = false
        scroll.contentInsetAdjustmentBehavior = .never
        scroll.translatesAutoresizingMaskIntoConstraints = false
        addSubview(scroll)
        stack.axis = .horizontal
        stack.alignment = .center
        stack.spacing = Self.spacing
        stack.translatesAutoresizingMaskIntoConstraints = false
        scroll.addSubview(stack)
        hide.translatesAutoresizingMaskIntoConstraints = false
        hide.accessibilityLabel = KeyRef.hideKeyboard.title
        hide.accessibilityIdentifier = "terminal.keys.hide"
        hide.addAction(UIAction { [weak self] _ in self?.onKey(.hideKeyboard) }, for: .touchUpInside)
        addSubview(hide)
        hairline.translatesAutoresizingMaskIntoConstraints = false
        hairline.backgroundColor = .separator
        addSubview(hairline)
        let scrollToEdge = scroll.trailingAnchor.constraint(equalTo: trailingAnchor)
        let scrollToHairline = scroll.trailingAnchor.constraint(equalTo: hairline.leadingAnchor, constant: -Self.edgeInset)
        self.scrollToEdge = scrollToEdge
        self.scrollToHairline = scrollToHairline
        NSLayoutConstraint.activate([
            glass.leadingAnchor.constraint(equalTo: leadingAnchor),
            glass.trailingAnchor.constraint(equalTo: trailingAnchor),
            glass.topAnchor.constraint(equalTo: topAnchor),
            glass.bottomAnchor.constraint(equalTo: bottomAnchor),
            scroll.leadingAnchor.constraint(equalTo: leadingAnchor),
            scrollToEdge,
            hide.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -Self.edgeInset),
            hide.centerYAnchor.constraint(equalTo: centerYAnchor),
            hairline.trailingAnchor.constraint(equalTo: hide.leadingAnchor, constant: -Self.edgeInset),
            hairline.widthAnchor.constraint(equalToConstant: 1 / max(traitCollection.displayScale, 1)),
            hairline.heightAnchor.constraint(equalToConstant: 18),
            hairline.centerYAnchor.constraint(equalTo: centerYAnchor),
            scroll.topAnchor.constraint(equalTo: topAnchor),
            scroll.bottomAnchor.constraint(equalTo: bottomAnchor),
            stack.leadingAnchor.constraint(equalTo: scroll.contentLayoutGuide.leadingAnchor, constant: Self.edgeInset),
            stack.trailingAnchor.constraint(equalTo: scroll.contentLayoutGuide.trailingAnchor, constant: -Self.edgeInset),
            stack.topAnchor.constraint(equalTo: scroll.contentLayoutGuide.topAnchor),
            stack.bottomAnchor.constraint(equalTo: scroll.contentLayoutGuide.bottomAnchor),
            stack.heightAnchor.constraint(equalTo: scroll.frameLayoutGuide.heightAnchor),
        ])
        fade.colors = [UIColor.black.cgColor, UIColor.black.cgColor, UIColor.clear.cgColor]
        fade.startPoint = CGPoint(x: 0, y: 0.5)
        fade.endPoint = CGPoint(x: 1, y: 0.5)
        accessibilityIdentifier = "terminal.keys.row"
        // The row sits on the keyboard's top edge, so a swipe down pushes both away. Sideways
        // paging is the scroll view's; down is this.
        let swipe = UISwipeGestureRecognizer(target: self, action: #selector(swipedDown))
        swipe.direction = .down
        // The keys scroll sideways: their pan must not take a downward flick from the swipe.
        swipe.delegate = self
        scroll.isDirectionalLockEnabled = true
        addGestureRecognizer(swipe)
        showHide(false)
    }

    @objc private func swipedDown() {
        onKey(.hideKeyboard)
    }

    private func showHide(_ on: Bool) {
        hide.isHidden = !on
        hairline.isHidden = !on
        scrollToEdge?.isActive = !on
        scrollToHairline?.isActive = on
    }

    required init?(coder: NSCoder) {
        nil
    }

    public override var intrinsicContentSize: CGSize {
        CGSize(width: UIView.noIntrinsicMetric, height: Self.height)
    }

    public override func layoutSubviews() {
        super.layoutSubviews()
        let overflows = scroll.contentSize.width > bounds.width + 1
        if overflows {
            let edge = max(0, 1 - 28 / max(bounds.width, 1))
            fade.locations = [0, NSNumber(value: Double(edge)), 1]
            fade.frame = bounds
            scroll.layer.mask = fade
        } else {
            scroll.layer.mask = nil
        }
    }

    /// Replaces the keys. Same items leave the row, and its scroll position, as they are.
    public func show(_ items: [TerminalKeysPlan.RowItem], agent: AgentKeys, latch: ModifierLatch) {
        guard items != self.items else { return }
        self.items = items
        stack.arrangedSubviews.forEach { $0.removeFromSuperview() }
        latchButtons = [:]
        let hides = items.last == .key(.hideKeyboard)
        for item in hides ? items.dropLast() : items[...] {
            stack.addArrangedSubview(button(for: item, agent: agent))
        }
        showHide(hides)
        showLatch(latch)
        setNeedsLayout()
    }

    public func showLatch(_ latch: ModifierLatch) {
        for (modifier, button) in latchButtons {
            KeyBarButton.show(latch.state(of: modifier), on: button)
            Self.slimFace(button)
        }
    }

    private func button(for item: TerminalKeysPlan.RowItem, agent: AgentKeys) -> UIButton {
        switch item {
        case .key(let ref):
            let button = Self.face(title: ref.title, symbol: ref.symbol)
            button.accessibilityLabel = KeyBarButton.spokenTitle(ref.title)
            if let modifier = ref.latch {
                latchButtons[modifier] = button
                button.addAction(UIAction { [weak self] _ in self?.onKey(ref) }, for: .touchUpInside)
            } else if ref.repeats {
                button.addAction(UIAction { [weak self, weak button] _ in
                    self?.onRepeatingKey(ref) { button?.isTracking ?? false }
                }, for: .touchDown)
                button.addAction(UIAction { [weak self] _ in self?.onRepeatEnded() },
                                 for: [.touchUpInside, .touchUpOutside, .touchCancel])
            } else {
                button.addAction(UIAction { [weak self] _ in self?.onKey(ref) }, for: .touchUpInside)
            }
            return button
        case .prompt(let key):
            let button = Self.face(title: key.label, symbol: key.symbol, filled: key.filled)
            button.accessibilityLabel = key.label
            button.accessibilityIdentifier = "terminal.keys.prompt.\(key.id)"
            button.addAction(UIAction { [weak self] _ in self?.onPrompt(key) }, for: .touchUpInside)
            return button
        case .shortcut(let shortcut):
            let button = Self.face(title: shortcut.title, symbol: shortcut.symbol)
            button.addAction(UIAction { [weak self] _ in self?.onShortcut(shortcut) }, for: .touchUpInside)
            return button
        }
    }

    /// A 30 point face inside the row's 40, a 44 point target including the gap beside it.
    static func face(title: String, symbol: String?, filled: Bool = false) -> UIButton {
        let button = KeyBarButton.make(title: title, symbol: symbol, slim: true)
        (button as? KeyTargetButton)?.horizontalHitOutset = spacing / 2
        if filled, var configuration = button.configuration {
            var filledConfiguration = UIButton.Configuration.filled()
            filledConfiguration.cornerStyle = .medium
            filledConfiguration.contentInsets = configuration.contentInsets
            filledConfiguration.title = configuration.title
            filledConfiguration.image = configuration.image
            filledConfiguration.titleTextAttributesTransformer = configuration.titleTextAttributesTransformer
            configuration = filledConfiguration
            button.configuration = configuration
        }
        slimFace(button)
        return button
    }

    private static func slimFace(_ button: UIButton) {
        guard var configuration = button.configuration else { return }
        configuration.background.backgroundInsets = NSDirectionalEdgeInsets(
            top: faceInset, leading: 0, bottom: faceInset, trailing: 0
        )
        button.configuration = configuration
        if !button.constraints.contains(where: { $0.firstAttribute == .height }) {
            button.heightAnchor.constraint(equalToConstant: height).isActive = true
        }
    }
}

extension InputsRowView: UIGestureRecognizerDelegate {
    public func gestureRecognizer(
        _ gestureRecognizer: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer
    ) -> Bool {
        other === scroll.panGestureRecognizer
    }
}

/// Glass under a floating control, where the system offers it; a blurred material elsewhere.
/// Reduce Transparency gets a solid fill.
@MainActor final class GlassBacking: UIView {
    private let effect: UIVisualEffectView

    init(cornerRadius: CGFloat) {
        let glass: UIVisualEffect = UIAccessibility.isReduceTransparencyEnabled
            ? UIBlurEffect(style: .systemThickMaterial)
            : UIGlassEffect()
        effect = UIVisualEffectView(effect: glass)
        super.init(frame: .zero)
        isUserInteractionEnabled = false
        effect.translatesAutoresizingMaskIntoConstraints = false
        effect.layer.cornerRadius = cornerRadius
        effect.layer.cornerCurve = .continuous
        effect.clipsToBounds = true
        addSubview(effect)
        NSLayoutConstraint.activate([
            effect.leadingAnchor.constraint(equalTo: leadingAnchor),
            effect.trailingAnchor.constraint(equalTo: trailingAnchor),
            effect.topAnchor.constraint(equalTo: topAnchor),
            effect.bottomAnchor.constraint(equalTo: bottomAnchor),
        ])
    }

    required init?(coder: NSCoder) {
        nil
    }
}
