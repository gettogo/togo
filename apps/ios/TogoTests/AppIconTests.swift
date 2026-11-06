import Foundation
import Testing
import UIKit
@testable import Togo

struct AppIconTests {
    /// The Home Screen, Settings, Spotlight and notifications all read the icon by this name.
    @Test func appNamesItsIcon() throws {
        let icons = try #require(Bundle.main.infoDictionary?["CFBundleIcons"] as? [String: Any])
        let primary = try #require(icons["CFBundlePrimaryIcon"] as? [String: Any])
        #expect(primary["CFBundleIconName"] as? String == "AppIcon")
    }

    /// The launch screen draws the mark the launch overlay starts from.
    @Test func launchScreenShowsTheMark() throws {
        let launch = try #require(Bundle.main.infoDictionary?["UILaunchScreen"] as? [String: Any])
        #expect(launch["UIImageName"] as? String == "LaunchMark")
        #expect(UIImage(named: "LaunchMark") != nil)
    }
}
