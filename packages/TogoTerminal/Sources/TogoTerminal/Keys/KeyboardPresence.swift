import GameController
import Observation
import UIKit

/// Tracks whether a hardware keyboard is attached. The key bar steps aside for it, and the app
/// reads it from the environment so shortcut hints appear only when a keyboard can use them.
@MainActor @Observable public final class KeyboardPresence {
    /// The one instance the app shares. Views read it as `\.hardwareKeyboard`.
    public static let shared = KeyboardPresence()

    public private(set) var hardwareKeyboardConnected: Bool
    @ObservationIgnored public var onChange: (Bool) -> Void = { _ in }

    @ObservationIgnored private let isConnected: @MainActor () -> Bool
    @ObservationIgnored private var observers: [any NSObjectProtocol] = []
    @ObservationIgnored private let center: NotificationCenter

    /// `isConnected` is the source of truth, asked at start and after every connect or disconnect
    /// notification. It defaults to `GCKeyboard`; tests pass their own.
    public init(
        center: NotificationCenter = .default,
        isConnected: @escaping @MainActor () -> Bool = { GCKeyboard.coalesced != nil }
    ) {
        self.center = center
        self.isConnected = isConnected
        hardwareKeyboardConnected = isConnected()
        for name in [Notification.Name.GCKeyboardDidConnect, .GCKeyboardDidDisconnect] {
            observers.append(center.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.refresh() }
            })
        }
    }

    isolated deinit {
        observers.forEach(center.removeObserver)
    }

    func refresh(connected: Bool? = nil) {
        let now = connected ?? isConnected()
        guard now != hardwareKeyboardConnected else { return }
        hardwareKeyboardConnected = now
        onChange(now)
    }
}
