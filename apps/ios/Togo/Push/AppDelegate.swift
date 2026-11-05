import UIKit

/// Forwards what only the application delegate hears: the APNs token, its failures and
/// background pushes.
@MainActor final class AppDelegate: NSObject, UIApplicationDelegate {
    /// Set by the app before the first callback can arrive.
    static var registrar: PushRegistrar?
    static var clear: ClearHandler?

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Self.registrar?.didRegister(token: deviceToken)
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: any Error) {
        Self.registrar?.didFail(error)
    }

    /// A `clear` push: the request was answered or expired, so its notification goes.
    func application(
        _ application: UIApplication, didReceiveRemoteNotification userInfo: [AnyHashable: Any]
    ) async -> UIBackgroundFetchResult {
        guard let clear = Self.clear else { return .noData }
        return await clear.didReceiveRemoteNotification(userInfo) ? .newData : .noData
    }
}
