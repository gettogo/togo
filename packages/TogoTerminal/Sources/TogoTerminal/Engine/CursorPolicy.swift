import Foundation

/// When the cursor blinks. A focused terminal shows a solid block that steps on and off once a
/// second; an unfocused one is a hollow block and never moves.
enum CursorPolicy {
    /// One full on and off cycle, in seconds. The step is hard: no fade between the two.
    static let period: TimeInterval = 1

    /// True when the cursor should blink. The user's setting asks for it, and neither Reduce
    /// Motion nor "Prefer Non-Blinking Cursor" takes it away. Only a focused terminal blinks.
    static func blinks(
        requested: Bool, focused: Bool, reduceMotion: Bool, prefersNonBlinking: Bool
    ) -> Bool {
        requested && focused && !reduceMotion && !prefersNonBlinking
    }
}
