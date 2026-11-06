import SwiftUI
import TogoKit
import UIKit

@main
struct TogoApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @Environment(\.scenePhase) private var scenePhase
    @State private var composition: AppComposition
    @State private var resumeLock: ResumeLockController

    init() {
        let composition = Self.makeComposition()
        _composition = State(initialValue: composition)
        let services = composition.services
        _resumeLock = State(initialValue: ResumeLockController(biometrics: .app) { services.settings.resumeLock })
    }

    private static func makeComposition() -> AppComposition {
        #if DEBUG
        let launch = UITestLaunch.current
        if launch.fakes {
            // Without configuring TipKit no tip shows, so no popover covers what a test taps.
            if launch.skipIntro { LaunchGate.app.skip() } else { TogoTips.configure() }
            return AppComposition(uiTest: launch)
        }
        #endif
        TogoTips.configure()
        return AppComposition(isolated: AppComposition.isRunningTests)
    }

    var body: some Scene {
        // A window opened for a tab, from "Open in New Window" or a drag, arrives with it.
        WindowGroup(for: TabID.self) { $tab in
            WindowRoot(composition: composition, opening: tab)
                .overlay {
                    if resumeLock.locked {
                        ResumeLockScreen { resumeLock.unlock() }
                    }
                }
                .privacyCover()
                .onReceive(NotificationCenter.default.publisher(for: UIApplication.protectedDataWillBecomeUnavailableNotification)) { _ in
                    resumeLock.protectedDataBecameUnavailable()
                }
                .task { composition.start() }
                .defaultAppStorage(composition.appStorage)
        }
        .commands { AppCommands() }
        // The app as a whole: connections pause only when every window is in the background.
        .onChange(of: scenePhase) { _, phase in
            resumeLock.scenePhaseChanged(to: phase)
            switch phase {
            case .active:
                composition.live?.setForeground(true)
                composition.review?.setForeground(true)
                composition.push?.becameActive()
            case .background:
                // Forwards are marked paused first; the helper connections close once they stopped.
                composition.review?.setForeground(false)
                composition.live?.setForeground(false)
            default: break
            }
        }
    }
}
