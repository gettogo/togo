import SwiftUI
import TogoTerminal
import UIKit

/// A real terminal and key bar playing a short agent exchange on a loop. The bytes are a fixture;
/// nothing is connected and the demo takes no input.
struct TerminalDemo: UIViewControllerRepresentable {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.colorScheme) private var colorScheme
    /// False draws the finished exchange at once, as Reduce Motion does.
    let playing: Bool

    /// What each step prints and how long the demo waits after it.
    static let script: [(bytes: String, pause: Duration)] = [
        ("\u{1b}[2m~/src/app\u{1b}[0m\r\n\r\n", .milliseconds(600)),
        ("\u{1b}[1m> \u{1b}[0mAdd a test for an empty password\r\n\r\n", .milliseconds(900)),
        ("  Read src/login.ts\r\n", .milliseconds(700)),
        ("  Edit test/login.test.ts \u{1b}[32m+18\u{1b}[0m\r\n\r\n", .milliseconds(900)),
        ("\u{1b}[1mRun npm test?\u{1b}[0m\r\n  \u{1b}[7m 1 Yes \u{1b}[0m   2 No\r\n", .seconds(3)),
    ]

    static var finished: String {
        script.map(\.bytes).joined()
    }

    func makeUIViewController(context: Context) -> DemoController {
        let controller = DemoController(theme: Self.theme(for: colorScheme))
        controller.setPlaying(playing && !reduceMotion)
        return controller
    }

    /// A paged TabView builds this page before it is current and keeps it after, so playback
    /// follows `playing` here rather than only when the view is made.
    func updateUIViewController(_ controller: DemoController, context: Context) {
        controller.apply(Self.theme(for: colorScheme))
        controller.setPlaying(playing && !reduceMotion)
    }

    static func dismantleUIViewController(_ controller: DemoController, coordinator: ()) {
        controller.setPlaying(false)
    }

    /// Beacon, light or dark with the system, as a session shows it by default.
    static func theme(for scheme: ColorScheme) -> TerminalTheme {
        let id = scheme == .dark ? "beacon-dark" : "beacon-light"
        return BuiltInThemes.all.first { $0.id == id } ?? BuiltInThemes.all[0]
    }

    @MainActor final class DemoController: UIViewController {
        private let engine: SwiftTermEngine
        private let keyBar: KeyBarView
        private var theme: TerminalTheme
        private var playback: Task<Void, Never>?

        /// True while the exchange loops; false while it shows the finished frame.
        var isPlaying: Bool { playback != nil }

        init(theme: TerminalTheme) {
            self.theme = theme
            engine = SwiftTermEngine(settings: TerminalSettings(), theme: theme)
            keyBar = KeyBarView(config: .standard, engine: engine)
            // The bar is shown as part of the picture, not as the keyboard's accessory.
            engine.inputAccessory = nil
            super.init(nibName: nil, bundle: nil)
            show(TerminalDemo.finished)
        }

        required init?(coder: NSCoder) {
            nil
        }

        override func viewDidLoad() {
            super.viewDidLoad()
            // The engine's view goes in bare: the full terminal controller takes focus and
            // raises the keyboard when it appears, which a picture in the guide must not do.
            let screen = engine.view
            screen.translatesAutoresizingMaskIntoConstraints = false
            keyBar.translatesAutoresizingMaskIntoConstraints = false
            view.addSubview(screen)
            view.addSubview(keyBar)
            NSLayoutConstraint.activate([
                screen.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 12),
                screen.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -12),
                screen.topAnchor.constraint(equalTo: view.topAnchor, constant: 12),
                screen.bottomAnchor.constraint(equalTo: keyBar.topAnchor, constant: -8),
                keyBar.leadingAnchor.constraint(equalTo: view.leadingAnchor),
                keyBar.trailingAnchor.constraint(equalTo: view.trailingAnchor),
                keyBar.bottomAnchor.constraint(equalTo: view.bottomAnchor),
                keyBar.heightAnchor.constraint(equalToConstant: 52),
            ])
            // The bar hides itself while a hardware keyboard is attached; here it is the subject.
            keyBar.isHidden = false
            view.backgroundColor = UIColor(Color(terminalHex: theme.background))
            view.isUserInteractionEnabled = false
            view.clipsToBounds = true
            view.layer.cornerRadius = 16
            view.layer.cornerCurve = .continuous
        }

        func apply(_ theme: TerminalTheme) {
            guard theme != self.theme else { return }
            self.theme = theme
            engine.apply(theme)
            viewIfLoaded?.backgroundColor = UIColor(Color(terminalHex: theme.background))
        }

        /// Starting replays the exchange from a clear screen; stopping leaves the finished frame.
        func setPlaying(_ on: Bool) {
            guard on != isPlaying else { return }
            playback?.cancel()
            playback = nil
            show(Self.clear)
            guard on else {
                show(TerminalDemo.finished)
                return
            }
            // Holds the controller only while feeding, never across a sleep, and ends once it is gone.
            playback = Task { [weak self] in
                while !Task.isCancelled {
                    for step in TerminalDemo.script {
                        guard self?.show(step.bytes) != nil else { return }
                        try? await Task.sleep(for: step.pause)
                        if Task.isCancelled { return }
                    }
                    guard self?.show(DemoController.clear) != nil else { return }
                }
            }
        }

        private func show(_ text: String) {
            engine.feed(ArraySlice(Array(text.utf8)))
        }

        private static let clear = "\u{1b}[2J\u{1b}[H"
    }
}

/// The session page: the terminal demo, then under Learn more the keys, gestures and shortcuts
/// it describes.
struct SessionPage: View {
    let playing: Bool
    /// Opens Learn more, for snapshots.
    var expanded = false

    @Environment(\.hardwareKeyboard) private var keyboard

    var body: some View {
        TutorialPageLayout(page: .session) {
            TerminalDemo(playing: playing)
                .frame(height: 248)
                .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(Color.togoLine))
                .accessibilityElement()
                .accessibilityLabel(String(
                    localized: "tutorial.session.demo",
                    defaultValue: "A terminal where an agent edits a test and asks to run npm test, with the key bar below it."
                ))
            TutorialMore(expanded: expanded) { details }
        }
    }

    @ViewBuilder private var details: some View {
        VStack(alignment: .leading, spacing: 20) {
            TutorialPoint(
                String(localized: "tutorial.session.keyBar.title", defaultValue: "The key bar"),
                systemImage: "keyboard",
                String(
                    localized: "tutorial.session.keyBar.detail",
                    defaultValue: "Esc, Ctrl, Tab and the arrows sit above the keyboard. Tap Ctrl once for the next key, twice to keep it on."
                )
            )
            TutorialPoint(
                String(localized: "tutorial.session.compose.title", defaultValue: "The compose bar"),
                systemImage: "text.cursor",
                String(
                    localized: "tutorial.session.compose.detail",
                    defaultValue: "Write a longer message with dictation and autocorrect, then send it to the agent in one go."
                )
            )
        }
        VStack(alignment: .leading, spacing: 14) {
            SectionHeading(String(localized: "tutorial.session.gestures", defaultValue: "Gestures"))
            gesture(
                String(localized: "tutorial.session.gesture.swipe", defaultValue: "Swipe with two fingers to move between sessions"),
                "hand.draw"
            )
            gesture(
                String(localized: "tutorial.session.gesture.pinch", defaultValue: "Pinch to make the text larger or smaller"),
                "arrow.up.left.and.arrow.down.right"
            )
            gesture(String(localized: "tutorial.session.gesture.tab", defaultValue: "Double tap to send Tab"), "hand.tap")
            gesture(
                String(localized: "tutorial.session.gesture.back", defaultValue: "Swipe in from the left edge to go back"),
                "chevron.left"
            )
        }
        if keyboard.hardwareKeyboardConnected {
            VStack(alignment: .leading, spacing: 14) {
                SectionHeading(
                    String(localized: "tutorial.session.keyboard", defaultValue: "With a hardware keyboard")
                )
                shortcut("⌘1 – ⌘9", String(localized: "tutorial.session.shortcut.sessions", defaultValue: "Switch sessions"))
                shortcut("⌘T", String(localized: "tutorial.session.shortcut.new", defaultValue: "New session"))
                shortcut("⌘K", String(localized: "tutorial.session.shortcut.controlCenter", defaultValue: "Back to your sessions"))
                shortcut("⌘W", String(localized: "tutorial.session.shortcut.detach", defaultValue: "Close the tab. The session keeps running."))
            }
        }
    }

    private func gesture(_ text: String, _ systemImage: String) -> some View {
        Label {
            Text(text)
                .font(.body)
                .foregroundStyle(Color.togoSnow)
        } icon: {
            Image(systemName: systemImage)
                .foregroundStyle(Color.togoMute)
                .frame(width: 28)
        }
    }

    private func shortcut(_ keys: String, _ action: String) -> some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                keyText(keys).frame(minWidth: 88, alignment: .leading)
                actionText(action)
            }
            VStack(alignment: .leading, spacing: 2) {
                keyText(keys)
                actionText(action)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private func keyText(_ keys: String) -> some View {
        Text(verbatim: keys)
            .font(.body.weight(.semibold))
            .foregroundStyle(Color.togoSnow)
    }

    private func actionText(_ text: String) -> some View {
        Text(text)
            .font(.body)
            .foregroundStyle(Color.togoMute)
            .fixedSize(horizontal: false, vertical: true)
    }
}
