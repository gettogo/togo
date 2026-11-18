/// One completion signal while a request page is visible. Restored results do not buzz,
/// and a receipt-capable machine must confirm the answer before the page leaves.
@MainActor struct WatchAnswerFeedback {
    enum Completion: Equatable { case answered, denied }
    private var observed = false
    private var completed = false

    mutating func observe(
        _ state: WatchStore.RequestState, expectsReceipt: Bool, decision: String? = nil
    ) -> Completion? {
        guard observed else {
            observed = true
            completed = state == .answered || state == .approved || state == .denied
                || (state == .sent && !expectsReceipt)
            return nil
        }
        guard !completed else { return nil }
        switch state {
        case .answered, .approved, .denied:
            break
        case .sent where !expectsReceipt:
            break
        default:
            return nil
        }
        completed = true
        return state == .denied || decision == "deny" ? .denied : .answered
    }
}
