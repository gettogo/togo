import CoreGraphics

/// Turns vertical pan travel into whole-line scroll steps and picks where they go.
///
/// Mosh carries no history and tmux keeps its own, so when the remote reports the mouse
/// (tmux with `mouse on` does) scrolling becomes wheel events and tmux copy-mode supplies the
/// history, over SSH and Mosh alike. Without mouse reporting the local scrollback is all there
/// is, so the pan scrolls it.
struct ScrollRouter {
    enum Action: Equatable {
        case none
        /// `up` reveals older output: SGR button 64. Down is button 65.
        case wheel(up: Bool, count: Int)
        /// Positive lines scroll toward older output.
        case local(lines: Int)
    }

    private var residue: CGFloat = 0

    /// Positive `distance` is a finger moving down the screen, which reveals older output.
    mutating func pan(by distance: CGFloat, lineHeight: CGFloat, mouseReporting: Bool) -> Action {
        guard lineHeight > 0 else { return .none }
        residue += distance
        // The tolerance keeps an exact multiple of the line height from losing a line to rounding.
        let raw = residue / lineHeight
        let lines = Int(raw + (raw >= 0 ? 1e-6 : -1e-6))
        guard lines != 0 else { return .none }
        residue -= CGFloat(lines) * lineHeight
        if mouseReporting {
            return .wheel(up: lines > 0, count: abs(lines))
        }
        return .local(lines: lines)
    }

    mutating func reset() {
        residue = 0
    }
}
