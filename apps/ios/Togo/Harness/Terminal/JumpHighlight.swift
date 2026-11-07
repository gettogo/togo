import UIKit

/// The band "Show in terminal" draws over the line it landed on: it shows at once, holds for
/// two seconds, then fades with the dim role. Under Reduce Motion it stays still and goes at
/// the end of the hold without a fade. A newer jump takes the band over; the older one's fade
/// then does nothing.
struct JumpHighlight: Equatable {
    static let hold: Duration = .seconds(2)

    enum Phase: Equatable {
        case hidden
        case shown(Int)
    }

    private(set) var phase: Phase = .hidden
    private var counter = 0

    /// Shows the band; the token names this showing.
    mutating func show() -> Int {
        counter += 1
        phase = .shown(counter)
        return counter
    }

    /// The hold ended for showing `token`. True when the band should go now; false when a newer
    /// showing owns it, or it already went.
    mutating func end(_ token: Int) -> Bool {
        guard phase == .shown(token) else { return false }
        phase = .hidden
        return true
    }

    /// Gone at once, as when the user scrolls or the terminal leaves the screen.
    mutating func clear() {
        phase = .hidden
    }

    var isShown: Bool { phase != .hidden }

    /// Whether the band fades out or simply goes.
    static func fades(reduceMotion: Bool) -> Bool { !reduceMotion }
}

/// The band itself: a quiet wash over the row and a hairline at its leading edge. Never the
/// beacon colour, which is kept for what needs the user.
final class JumpBandView: UIView {
    private let edge = UIView()

    override init(frame: CGRect) {
        super.init(frame: frame)
        isUserInteractionEnabled = false
        backgroundColor = UIColor.togoSnow.withAlphaComponent(0.14)
        edge.backgroundColor = UIColor.togoSnow.withAlphaComponent(0.7)
        addSubview(edge)
        isAccessibilityElement = false
    }

    required init?(coder: NSCoder) {
        nil
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        edge.frame = CGRect(x: 0, y: 0, width: 2, height: bounds.height)
    }
}
