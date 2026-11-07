import UIKit

/// The system keyboard's Shift key, which UIKit does not publish. The keyboard's in-process
/// controller answers `isShifted` and takes `setShift:`; every call is checked first, so a
/// system without them reads as unshifted and nothing else changes.
@MainActor enum SystemKeyboardShift {
    static let live = SoftShift(isShifted: { isShifted }, release: { release() })

    private static var controller: NSObject? {
        let activeInstance = NSSelectorFromString("activeInstance")
        guard let type = NSClassFromString("UIKeyboardImpl") as? NSObject.Type,
              type.responds(to: activeInstance) else { return nil }
        return type.perform(activeInstance)?.takeUnretainedValue() as? NSObject
    }

    static var isShifted: Bool {
        guard let controller, controller.responds(to: NSSelectorFromString("isShifted")) else { return false }
        return (controller.value(forKey: "shifted") as? Bool) ?? false
    }

    /// Caps Lock stays on, as it would after a typed letter.
    static func release() {
        guard let controller, controller.responds(to: NSSelectorFromString("setShift:")) else { return }
        if controller.responds(to: NSSelectorFromString("isShiftLocked")),
           (controller.value(forKey: "shiftLocked") as? Bool) == true { return }
        controller.setValue(false, forKey: "shift")
    }
}
