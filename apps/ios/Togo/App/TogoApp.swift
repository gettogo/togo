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
            if launch.skipIntro { LaunchGate.app.skip() } else { PerfSignposts.app.afterInteractive { TogoTips.configure() } }
            return AppComposition(uiTest: launch)
        }
        #endif
        // TipKit opens its datastore as it is configured; the first screen does not wait for it.
        PerfSignposts.app.afterInteractive { TogoTips.configure() }
        return PerfSignposts.measure("composition") { AppComposition(isolated: AppComposition.isRunningTests) }
    }

    var body: some Scene {
        // A window opened for a tab, from "Open in New Window" or a drag, arrives with it.
        WindowGroup(for: TabID.self) { $tab in
            WindowRoot(composition: composition, opening: tab)
                // In a window of its own above the workspace cover, sheets and alerts.
                .sceneShield(lock: resumeLock)
                .environment(\.machineTools, composition.machineTools)
                .environment(\.agentLibrary, composition.agentLibrary)
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
                // At a cold launch Live Activities and widgets wait for the first screen too.
                if let push = composition.push {
                    PerfSignposts.app.afterInteractive { push.becameActive() }
                }
            case .background:
                // Forwards are marked paused first; the helper connections close once they stopped.
                composition.review?.setForeground(false)
                composition.live?.setForeground(false)
            default: break
            }
        }
    }
}
