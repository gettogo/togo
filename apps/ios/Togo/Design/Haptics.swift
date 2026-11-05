import SwiftUI

/// One haptic per action, and nothing else in the app vibrates.
enum Haptic: CaseIterable, Sendable {
    case approve, deny, connect, error, tabSettle
    /// A stage, unstage or commit went through.
    case done
    /// The user confirmed a discard.
    case discard
    /// Dictation started listening.
    case listen

    var feedback: SensoryFeedback {
        switch self {
        case .approve: .success
        case .deny: .impact(flexibility: .rigid, intensity: 0.7)
        case .connect: .impact(flexibility: .soft, intensity: 0.5)
        case .error: .error
        case .tabSettle: .selection
        case .done: .impact(flexibility: .soft, intensity: 0.6)
        case .discard: .warning
        case .listen: .impact(flexibility: .soft, intensity: 0.4)
        }
    }
}

extension View {
    func haptic(_ haptic: Haptic, trigger: some Equatable) -> some View {
        sensoryFeedback(haptic.feedback, trigger: trigger)
    }

    /// Plays when `condition` holds for the new value, such as a link phase turning live.
    func haptic<T: Equatable>(_ haptic: Haptic, trigger: T, when condition: @escaping (T) -> Bool) -> some View {
        sensoryFeedback(haptic.feedback, trigger: trigger) { _, new in condition(new) }
    }
}
