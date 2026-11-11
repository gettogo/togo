import UIKit

/// The slim column at the terminal's trailing edge while a software keyboard types: Esc, Ctrl,
/// the agent's Mode and Stop, and the controller at the foot, nearest the keyboard. Where the
/// system draws a vertical bar the controller moves there and this column is not shown.
@MainActor public final class KeyColumnView: UIView {
    public static let width: CGFloat = 40
    static let faceSide: CGFloat = 34
    static let faceHeight: CGFloat = 36
    static let padding: CGFloat = 3
    static let spacing: CGFloat = 2

    public let controller = ControllerFace()
    public var onKey: (KeyRef) -> Void = { _ in }
    public private(set) var keys: [KeyRef] = []

    private let glass = GlassBacking(cornerRadius: 22)
    private let stack = UIStackView()
    private var buttons: [KeyRef: UIButton] = [:]

    public override init(frame: CGRect) {
        super.init(frame: frame)
        glass.translatesAutoresizingMaskIntoConstraints = false
        addSubview(glass)
        stack.axis = .vertical
        stack.alignment = .center
        stack.spacing = Self.spacing
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        NSLayoutConstraint.activate([
            glass.leadingAnchor.constraint(equalTo: leadingAnchor),
            glass.trailingAnchor.constraint(equalTo: trailingAnchor),
            glass.topAnchor.constraint(equalTo: topAnchor),
            glass.bottomAnchor.constraint(equalTo: bottomAnchor),
            stack.leadingAnchor.constraint(equalTo: leadingAnchor, constant: Self.padding),
            stack.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -Self.padding),
            stack.topAnchor.constraint(equalTo: topAnchor, constant: Self.padding),
            stack.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -Self.padding),
            widthAnchor.constraint(equalToConstant: Self.width),
        ])
        controller.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            controller.widthAnchor.constraint(equalToConstant: Self.faceSide),
            controller.heightAnchor.constraint(equalToConstant: 40),
        ])
        accessibilityIdentifier = "terminal.keys.column"
    }

    required init?(coder: NSCoder) {
        nil
    }

    /// Replaces the keys above the controller. The same keys leave the column as it is.
    public func show(_ keys: [KeyRef], latch: ModifierLatch) {
        guard keys != self.keys || stack.arrangedSubviews.isEmpty else { return }
        self.keys = keys
        stack.arrangedSubviews.forEach { $0.removeFromSuperview() }
        buttons = [:]
        for ref in keys {
            let button = Self.face(ref)
            button.addAction(UIAction { [weak self] _ in self?.onKey(ref) }, for: .touchUpInside)
            buttons[ref] = button
            stack.addArrangedSubview(button)
        }
        stack.addArrangedSubview(controller)
        showLatch(latch)
    }

    public func showLatch(_ latch: ModifierLatch) {
        for (ref, button) in buttons {
            guard let modifier = ref.latch else { continue }
            KeyBarButton.show(latch.state(of: modifier), on: button)
            Self.fit(button)
        }
    }

    /// A 34 by 36 point face; the touch target runs to the column's edge.
    private static func face(_ ref: KeyRef) -> UIButton {
        let button = KeyBarButton.make(title: ref.title, symbol: ref.symbol, slim: true)
        button.accessibilityLabel = KeyBarButton.spokenTitle(ref.title)
        (button as? KeyTargetButton)?.horizontalHitOutset = padding
        fit(button)
        return button
    }

    private static func fit(_ button: UIButton) {
        guard var configuration = button.configuration else { return }
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 2, leading: 2, bottom: 2, trailing: 2)
        configuration.background.backgroundInsets = .zero
        configuration.titleTextAttributesTransformer = UIConfigurationTextAttributesTransformer { attributes in
            var attributes = attributes
            attributes.font = UIFont.preferredFont(forTextStyle: .caption1)
            return attributes
        }
        configuration.preferredSymbolConfigurationForImage = UIImage.SymbolConfiguration(textStyle: .footnote)
        button.configuration = configuration
        button.translatesAutoresizingMaskIntoConstraints = false
        button.setContentHuggingPriority(.required, for: .vertical)
        NSLayoutConstraint.activate([
            button.widthAnchor.constraint(equalToConstant: faceSide),
            button.heightAnchor.constraint(equalToConstant: faceHeight),
        ])
    }
}
