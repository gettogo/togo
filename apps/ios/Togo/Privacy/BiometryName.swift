import LocalAuthentication
import UIKit

/// Unlock wording from the hardware: iPhone Duo has Touch ID, so "Face ID" is never assumed.
enum BiometryName {
    static func label(for type: LABiometryType) -> String {
        switch type {
        case .touchID: "Touch ID"
        case .faceID: "Face ID"
        case .opticID: "Optic ID"
        default: "passcode"
        }
    }

    static var current: String {
        let context = LAContext()
        _ = context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil)
        return label(for: context.biometryType)
    }
}

/// "iPhone" or "iPad", for copy about what stays on this device.
enum DeviceName {
    @MainActor static var model: String { UIDevice.current.model }
    /// The SF Symbol that draws this device in diagrams.
    @MainActor static var symbol: String { model == "iPad" ? "ipad" : "iphone" }
}
