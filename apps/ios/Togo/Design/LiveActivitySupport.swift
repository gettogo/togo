import ActivityKit
import SwiftUI

private struct LiveActivitiesAvailableKey: EnvironmentKey {
    static var defaultValue: Bool { ActivityAuthorizationInfo().areActivitiesEnabled }
}

extension EnvironmentValues {
    /// Whether this device can show Live Activities at all. An iPad has none, so settings and
    /// tutorial lines that promise them stay out of sight there.
    var liveActivitiesAvailable: Bool {
        get { self[LiveActivitiesAvailableKey.self] }
        set { self[LiveActivitiesAvailableKey.self] = newValue }
    }
}
