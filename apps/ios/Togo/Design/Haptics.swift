import SwiftUI
import TogoTerminal
import UIKit

/// One haptic per action, and nothing else in the app vibrates.
enum Haptic: CaseIterable, Sendable {
    case approve, deny, connect, error
    /// A pane settled on a tab: after a swipe, a chip, a pick from the sidebar or a swap of the
    /// two panes. One tick per settle.
    case tabSettle
    /// A stage, unstage or commit went through, or a command was copied.
    case done
    /// The user confirmed a discard.
    case discard
    /// Dictation started listening.
    case listen
    /// A terminal key bar modifier armed or released.
    case keyModifier
    /// A terminal key bar modifier locked.
    case keyLock
    /// A terminal key bar key such as Esc, ^C, Tab or an arrow; held repeats stay silent.
    case key

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
        case .keyModifier: .impact(flexibility: .solid, intensity: 0.5)
        case .keyLock: .impact(flexibility: .rigid, intensity: 0.9)
        case .key: .impact(flexibility: .soft, intensity: 0.3)
        }
    }

    /// For UIKit views that have no SwiftUI trigger to hang `sensoryFeedback` on, such as the
    /// terminal key bar above the keyboard.
    @MainActor func play() {
        switch self {
        case .approve: UINotificationFeedbackGenerator().notificationOccurred(.success)
        case .error: UINotificationFeedbackGenerator().notificationOccurred(.error)
        case .discard: UINotificationFeedbackGenerator().notificationOccurred(.warning)
        case .tabSettle: UISelectionFeedbackGenerator().selectionChanged()
        case .keyModifier: UIImpactFeedbackGenerator(style: .medium).impactOccurred(intensity: 0.5)
        case .deny: UIImpactFeedbackGenerator(style: .rigid).impactOccurred(intensity: 0.7)
        case .keyLock: UIImpactFeedbackGenerator(style: .rigid).impactOccurred(intensity: 0.9)
        case .connect: UIImpactFeedbackGenerator(style: .soft).impactOccurred(intensity: 0.5)
        case .done: UIImpactFeedbackGenerator(style: .soft).impactOccurred(intensity: 0.6)
        case .listen: UIImpactFeedbackGenerator(style: .soft).impactOccurred(intensity: 0.4)
        case .key: UIImpactFeedbackGenerator(style: .soft).impactOccurred(intensity: 0.3)
        }
    }
}

extension Haptic {
    init(_ key: KeyHaptic) {
        self = switch key {
        case .modifier: .keyModifier
        case .lock: .keyLock
        case .key: .key
        }
    }
}

extension View {
    func haptic(_ haptic: Haptic, trigger: some Equatable) -> some View {
        sensoryFeedback(haptic.feedback, trigger: trigger)
    }

    /// Plays when `condition` holds for the change from the old value to the new one.
    func haptic<T: Equatable>(_ haptic: Haptic, trigger: T, change condition: @escaping (T, T) -> Bool) -> some View {
        sensoryFeedback(haptic.feedback, trigger: trigger, condition: condition)
    }

    /// Plays when `condition` holds for the new value, such as a link phase turning live.
    func haptic<T: Equatable>(_ haptic: Haptic, trigger: T, when condition: @escaping (T) -> Bool) -> some View {
        sensoryFeedback(haptic.feedback, trigger: trigger) { _, new in condition(new) }
    }
}
