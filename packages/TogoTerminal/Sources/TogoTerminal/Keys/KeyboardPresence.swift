import GameController
import UIKit

/// Tracks whether a hardware keyboard is attached, so the key bar can step aside.
@MainActor public final class KeyboardPresence {
    public private(set) var hardwareKeyboardConnected: Bool
    public var onChange: (Bool) -> Void = { _ in }

    private var observers: [any NSObjectProtocol] = []

    public init() {
        hardwareKeyboardConnected = GCKeyboard.coalesced != nil
        let center = NotificationCenter.default
        for name in [Notification.Name.GCKeyboardDidConnect, .GCKeyboardDidDisconnect] {
            observers.append(center.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.refresh() }
            })
        }
    }

    isolated deinit {
        observers.forEach(NotificationCenter.default.removeObserver)
    }

    func refresh(connected: Bool = GCKeyboard.coalesced != nil) {
        guard connected != hardwareKeyboardConnected else { return }
        hardwareKeyboardConnected = connected
        onChange(connected)
    }
}
