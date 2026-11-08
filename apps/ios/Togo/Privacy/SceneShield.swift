import SwiftUI
import UIKit

/// The app-switcher cover and the Face ID lock, in a window of their own above everything the
/// scene shows. A SwiftUI overlay on the root never draws over a full-screen cover (the
/// workspace and its terminal), a sheet or a UIKit presentation; a window above alerts does.
/// While locked the window is key, so a hardware keyboard cannot type into the terminal under it.
@MainActor
final class SceneShield {
    enum Face: Equatable { case hidden, cover, lock }

    private var window: UIWindow?
    private weak var previousKey: UIWindow?
    private var face: Face = .hidden

    /// What the shield shows for the scene's state. Pure, so the rules are tested.
    static func face(phase: ScenePhase, wasActive: Bool, locked: Bool, locksOnReturn: Bool) -> Face {
        if locked { return .lock }
        if PrivacyCoverRule.covers(phase, wasActive: wasActive) { return .cover }
        // Returning to a lock: keep covering until the lock is up.
        if phase == .active, locksOnReturn { return .cover }
        return .hidden
    }

    func show(_ face: Face, in scene: UIWindowScene?, unlock: @escaping () -> Void) {
        guard face != self.face || (face != .hidden && window?.windowScene !== scene) else { return }
        self.face = face
        guard face != .hidden, let scene else {
            hide()
            return
        }
        let window = self.window?.windowScene === scene ? self.window! : make(in: scene)
        let host = window.rootViewController as? UIHostingController<ShieldView>
        host?.rootView = ShieldView(face: face, unlock: unlock)
        window.isHidden = false
        if face == .lock, !window.isKeyWindow {
            previousKey = scene.keyWindow
            window.makeKey()
        }
    }

    private func make(in scene: UIWindowScene) -> UIWindow {
        let window = UIWindow(windowScene: scene)
        window.windowLevel = .alert + 1
        let host = UIHostingController(rootView: ShieldView(face: .cover, unlock: {}))
        host.view.backgroundColor = UIColor(Color.togoInk)
        window.rootViewController = host
        window.accessibilityViewIsModal = true
        self.window = window
        return window
    }

    private func hide() {
        guard let window, !window.isHidden else { return }
        let wasKey = window.isKeyWindow
        window.isHidden = true
        if wasKey { previousKey?.makeKey() }
        previousKey = nil
    }
}

struct ShieldView: View {
    let face: SceneShield.Face
    let unlock: () -> Void

    var body: some View {
        if face == .lock {
            ResumeLockScreen(unlock: unlock)
        } else {
            PrivacySurface()
        }
    }
}

private struct SceneShieldModifier: ViewModifier {
    @Environment(\.scenePhase) private var scenePhase
    @State private var wasActive = false
    @State private var shield = SceneShield()
    @State private var scene: UIWindowScene?
    let lock: ResumeLockController

    func body(content: Content) -> some View {
        content
            .background { SceneReader { scene = $0 }.accessibilityHidden(true) }
            .onChange(of: scenePhase, initial: true) { _, phase in
                if phase == .active { wasActive = true }
                apply()
            }
            .onChange(of: lock.locked) { apply() }
            .onChange(of: scene) { apply() }
    }

    private func apply() {
        let face = SceneShield.face(
            phase: scenePhase, wasActive: wasActive, locked: lock.locked, locksOnReturn: lock.locksOnReturn
        )
        shield.show(face, in: scene) { [lock] in lock.unlock() }
    }
}

/// Reports the window scene the view is in.
struct SceneReader: UIViewRepresentable {
    let found: (UIWindowScene?) -> Void

    func makeUIView(context: Context) -> ReaderView {
        let view = ReaderView()
        view.found = found
        view.isUserInteractionEnabled = false
        return view
    }

    func updateUIView(_ view: ReaderView, context: Context) {
        view.found = found
    }

    final class ReaderView: UIView {
        var found: ((UIWindowScene?) -> Void)?
        override func didMoveToWindow() {
            super.didMoveToWindow()
            found?(window?.windowScene)
        }
    }
}

extension View {
    /// The scene's privacy cover and Face ID lock, above every presentation (`SceneShield`).
    func sceneShield(lock: ResumeLockController) -> some View {
        modifier(SceneShieldModifier(lock: lock))
    }
}
