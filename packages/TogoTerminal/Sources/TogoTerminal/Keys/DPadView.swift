import UIKit

/// A 3 by 3 pad: arrows on the cross, Esc and Delete in the top corners, where a thumb
/// reaches them without leaving the pad.
@MainActor final class DPadView: UIView {
    static let side: CGFloat = 44
    var onStroke: (KeyStroke) -> Void = { _ in }

    private static let layout: [[(String, String?, TerminalKey)?]] = [
        [("Esc", nil, .escape), ("Up", "arrow.up", .up), ("Delete", "delete.left", .backspace)],
        [("Left", "arrow.left", .left), nil, ("Right", "arrow.right", .right)],
        [nil, ("Down", "arrow.down", .down), nil],
    ]

    override init(frame: CGRect) {
        super.init(frame: frame)
        let rows = UIStackView(arrangedSubviews: Self.layout.map { row in
            let stack = UIStackView(arrangedSubviews: row.map(cell))
            stack.distribution = .fillEqually
            return stack
        })
        rows.axis = .vertical
        rows.distribution = .fillEqually
        rows.translatesAutoresizingMaskIntoConstraints = false
        addSubview(rows)
        NSLayoutConstraint.activate([
            rows.leadingAnchor.constraint(equalTo: leadingAnchor),
            rows.trailingAnchor.constraint(equalTo: trailingAnchor),
            rows.topAnchor.constraint(equalTo: topAnchor),
            rows.bottomAnchor.constraint(equalTo: bottomAnchor),
            rows.widthAnchor.constraint(equalToConstant: Self.side * 3),
            rows.heightAnchor.constraint(equalToConstant: Self.side * 3),
        ])
    }

    required init?(coder: NSCoder) {
        nil
    }

    private func cell(_ spec: (String, String?, TerminalKey)?) -> UIView {
        guard let (title, symbol, key) = spec else { return UIView() }
        let button = KeyBarButton.make(title: title, symbol: symbol)
        button.addAction(UIAction { [weak self] _ in self?.onStroke(KeyStroke(key)) }, for: .touchUpInside)
        return button
    }
}
