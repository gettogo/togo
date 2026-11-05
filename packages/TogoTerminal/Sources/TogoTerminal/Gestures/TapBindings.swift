/// What a double or triple tap sends. A nil sequence leaves that tap to the terminal, which
/// selects a word or a line with it.
struct TapBindings: Equatable, Sendable {
    var doubleTap: [UInt8]?
    var tripleTap: [UInt8]?

    /// Nil while text is selected: a tap then adjusts or clears the selection, and keys sent
    /// at that moment would reach the remote unseen.
    func sequence(forTaps count: Int, isSelecting: Bool) -> [UInt8]? {
        guard !isSelecting else { return nil }
        switch count {
        case 2: return doubleTap.flatMap { $0.isEmpty ? nil : $0 }
        case 3: return tripleTap.flatMap { $0.isEmpty ? nil : $0 }
        default: return nil
        }
    }
}
