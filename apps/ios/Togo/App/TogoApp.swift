import SwiftUI
import TogoKit
import UIKit

@main
struct TogoApp: App {
    @Environment(\.scenePhase) private var scenePhase
    @State private var resumeLock = ResumeLockController(biometrics: .app)

    var body: some Scene {
        WindowGroup {
            RootView()
                .overlay {
                    if resumeLock.locked {
                        ResumeLockScreen { resumeLock.unlock() }
                    }
                }
                .privacyCover()
                .onReceive(NotificationCenter.default.publisher(for: UIApplication.protectedDataWillBecomeUnavailableNotification)) { _ in
                    resumeLock.protectedDataBecameUnavailable()
                }
        }
        .onChange(of: scenePhase) { _, phase in
            resumeLock.scenePhaseChanged(to: phase)
        }
    }
}

extension BiometricSession {
    /// The one session for the app. The resume lock and every key signature share it, so
    /// a device lock or a long background clears the context signing uses too.
    static let app = BiometricSession()
}
