import Foundation
import TogoKit
import TogoTerminal
import UIKit

/// One attached session: the link keeps it attached across drops, the binding moves bytes
/// between the link and the engine, and the controller shows the engine with the user's
/// gestures, key bar, font and theme.
@MainActor final class LiveTerminal {
    let engine: SwiftTermEngine
    let controller: ThemedTerminalController
    let handle: TerminalHandle
    private let link: TerminalLink
    private let binding: TerminalBinding
    private var feed: Task<Void, Never>?
    private var keyBarConfig: KeyBarConfig

    /// `onStop` gets this terminal's identity once its tab lets go of it.
    /// `wasAsked` tells whether the machine put a sign-in question to the user, which turns a
    /// failed attach into signed out.
    init(
        connect: @escaping LinkConnect, settings: AppSettings, wasAsked: @escaping @Sendable () -> Bool = { false },
        diagnostics: DiagnosticsRecorder? = nil,
        onState: @escaping @Sendable (LinkState) -> Void = { _ in },
        onStop: @escaping @MainActor (ObjectIdentifier) -> Void
    ) {
        engine = SwiftTermEngine(settings: Self.terminalSettings(settings), theme: BuiltInThemes.all[0])
        engine.view.accessibilityLabel = WorkspaceCopy.terminal
        engine.accessibilityWords = AccessibilityWords(cursorLine: WorkspaceCopy.cursorLine, blank: WorkspaceCopy.blankLine)
        controller = ThemedTerminalController(
            terminal: TerminalViewController(engine: engine, gestureConfig: Self.gestureConfig(settings.gestures)),
            choice: Self.themeChoice(settings)
        )
        keyBarConfig = settings.keyBarConfig
        let size = engine.size
        let link = TerminalLink(
            connect: connect, path: SystemPathSource(),
            cols: size.cols > 0 ? size.cols : 80, rows: size.rows > 0 ? size.rows : 24
        )
        self.link = link
        let ticker = DisplayLinkTicker()
        ticker.recordsFrameStats = diagnostics != nil
        let controller = self.controller
        binding = TerminalBinding(
            engine: engine, ticker: ticker,
            read: { await link.read() },
            // Typing while the link is between attachments is not kept for the next one, whose
            // screen may be another prompt by then; the terminal says the typing went nowhere.
            send: { [weak controller] data in
                if await !link.send(data) { await controller?.inputDropped() }
            },
            resize: { cols, rows in await link.resize(cols: cols, rows: rows) }
        )
        let (phases, sink) = AsyncStream.makeStream(of: LinkPhase.self)
        let engine = self.engine
        let stop = Deferred()
        handle = TerminalHandle(
            engineView: controller, phases: phases,
            search: TerminalSearch(
                find: { engine.find($0, caseSensitive: false, regex: false, backwards: $1) },
                clear: { engine.clearFind() }
            ),
            onStop: { stop.action() }
        )
        let id = ObjectIdentifier(self)
        stop.action = { [weak self] in
            self?.shutDown()
            sink.finish()
            onStop(id)
        }
        installKeyBar()
        engine.onShortcut = { [weak handle] in handle?.onShortcut($0) }
        engine.onPasteImage = { [weak handle] in handle?.onPasteImage($0) }
        handle.paste = { [weak engine] in engine?.paste($0) }
        handle.pressEnter = { [weak engine] in engine?.send(KeyStroke(.enter)) }
        engine.onOpenURL = { [weak handle] in handle?.onOpenURL($0) }
        handle.links = { [weak engine] in engine?.linksOnScreen() ?? [] }
        let gestures = controller.terminal.gestures
        gestures.onSwipeSession = { [weak handle] in handle?.onSwipe($0 == .left ? 1 : -1) }
        gestures.onEdgeSwipe = { [weak handle] in handle?.onEdgeSwipe() }
        gestures.onFontSize = { [weak handle] in handle?.onFontSize(Double($0)) }
        gestures.onShowLinks = { [weak handle] in handle?.onShowLinks() }
        // Nothing can be typed until the link is live, so the keyboard waits for it.
        engine.acceptsFocus = false
        feed = Task { [weak engine, weak controller] in
            var wasLive = false
            for await state in link.states {
                onState(state)
                let phase = Self.phase(state, asked: wasAsked(), wasLive: wasLive)
                if case .live = state {
                    wasLive = true
                    controller?.inputResumed()
                }
                if let accepts = Self.acceptsFocus(phase) { engine?.acceptsFocus = accepts }
                sink.yield(phase)
            }
        }
        // A long burst of output is summed up in numbers: how it paced, never what it said.
        binding.onBurst = { [weak ticker] burst in
            guard let ticker else { return }
            diagnostics?.frames(burst, pacing: ticker.frameStats)
            ticker.resetFrameStats()
        }
        binding.start()
        Task { await link.start() }
    }

    func apply(_ settings: AppSettings) {
        engine.apply(Self.terminalSettings(settings))
        controller.choice = Self.themeChoice(settings)
        controller.terminal.gestures.bindings = Self.gestureConfig(settings.gestures)
        if settings.keyBarConfig != keyBarConfig {
            keyBarConfig = settings.keyBarConfig
            installKeyBar()
        }
    }

    func setForeground(_ on: Bool) {
        let link = self.link
        Task { on ? await link.willEnterForeground() : await link.didEnterBackground() }
    }

    private func installKeyBar() {
        let bar = KeyBarView(config: keyBarConfig, engine: engine)
        bar.onCompose = { [weak handle] in handle?.onCompose() }
        bar.onDictate = { [weak handle] in handle?.onDictate() }
        bar.onHaptic = { Haptic($0).play() }
        bar.hidesCompose = handle.hidesComposeKeys
        handle.onHidesComposeKeys = { [weak bar] in bar?.hidesCompose = $0 }
    }

    private func shutDown() {
        binding.stop()
        feed?.cancel()
        let link = self.link
        Task { await link.stop() }
    }

    // MARK: Settings

    static func terminalSettings(_ settings: AppSettings) -> TerminalSettings {
        TerminalSettings(
            fontName: settings.fontName == SettingsValues.systemFonts[0] ? nil : settings.fontName,
            fontSize: CGFloat(settings.fontSize),
            cursor: CursorShape(rawValue: settings.cursor) ?? .block,
            themeID: settings.themeID,
            readNewOutput: settings.readNewOutput
        )
    }

    static func themeChoice(_ settings: AppSettings) -> ThemeChoice {
        let choices = ThemeChoice.all(imported: settings.customThemes)
        return choices.first { $0.id == settings.themeID } ?? choices[0]
    }

    /// Tap bindings are the text the tap types; an empty one gives the tap back to selection.
    static func gestureConfig(_ gestures: GestureBindings) -> GestureConfig {
        GestureConfig(
            doubleTap: gestures.doubleTap.isEmpty ? nil : Array(gestures.doubleTap.utf8),
            tripleTap: gestures.tripleTap.isEmpty ? nil : Array(gestures.tripleTap.utf8),
            twoFingerSwipe: gestures.twoFingerSwipe, edgeSwipe: gestures.edgeSwipe,
            pinchZoom: gestures.pinchZoom, touchMouse: gestures.touchMouse
        )
    }

    /// Whether the terminal may hold the keyboard in `phase`: yes while live, no once the
    /// link is down for good or not yet up. A reconnect leaves the keyboard where it was.
    static func acceptsFocus(_ phase: LinkPhase) -> Bool? {
        switch phase {
        case .live, .roaming: true
        case .waking, .connecting, .failed, .signedOut: false
        case .reconnecting: nil
        }
    }

    /// Retries before the link was ever live read as connecting: there is no screen yet to
    /// keep readable, so the paws keep walking toward the first one.
    static func phase(_ state: LinkState, asked: Bool = false, wasLive: Bool = true) -> LinkPhase {
        switch state {
        case .reconnecting where !wasLive: .connecting
        case .failed where asked: .signedOut
        case .waking(let host): .waking(host)
        case .connecting: .connecting
        case .live(let kind, let notes): .live(transport: kind.displayName, note: notes.first?.reason)
        case .roaming: .roaming
        case .reconnecting(let attempt): .reconnecting(attempt)
        case .failed(let reason): .failed(reason)
        case .ended: .failed(String(localized: "ws.link.ended", defaultValue: "This session has ended."))
        }
    }
}

/// The handle is made before the terminal it stops, so its stop action is filled in afterwards.
@MainActor private final class Deferred {
    var action: () -> Void = {}
}

/// Holds the terminal controller and gives the engine the dark or light build of the theme
/// that matches the appearance, now and whenever it changes.
final class ThemedTerminalController: UIViewController, TerminalRefitting {
    let terminal: TerminalViewController
    var choice: ThemeChoice {
        didSet { applyTheme() }
    }

    init(terminal: TerminalViewController, choice: ThemeChoice) {
        self.terminal = terminal
        self.choice = choice
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) {
        nil
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        addChild(terminal)
        terminal.view.frame = view.bounds
        terminal.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(terminal.view)
        terminal.didMove(toParent: self)
        registerForTraitChanges([UITraitUserInterfaceStyle.self]) { (self: Self, _) in self.applyTheme() }
    }

    override func viewIsAppearing(_ animated: Bool) {
        super.viewIsAppearing(animated)
        applyTheme()
    }

    /// Set outright rather than left to the autoresizing mask, which only adds the change in
    /// size to whatever frame the terminal had when it moved here.
    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        if terminal.view.frame != view.bounds { terminal.view.frame = view.bounds }
    }

    func refit() {
        guard isViewLoaded else { return }
        view.setNeedsLayout()
        view.layoutIfNeeded()
        terminal.refit()
    }

    func holdFit(_ on: Bool) {
        terminal.holdsFit = on
    }

    // MARK: Input paused

    /// How long the notice stays after the last keystroke that went nowhere.
    static let pausedNoticeTime: Duration = .seconds(2.5)
    private var pausedNotice: UILabel?
    private var pausedHide: Task<Void, Never>?

    /// A keystroke went nowhere because the link is reconnecting. The notice shows over the
    /// terminal where the person is typing, with a haptic and a VoiceOver announcement once per
    /// burst, and goes when the link is live again or the typing stops.
    func inputDropped() {
        guard isViewLoaded else { return }
        let notice = pausedNotice ?? makePausedNotice()
        if notice.isHidden {
            notice.isHidden = false
            Haptic.error.play()
            UIAccessibility.post(notification: .announcement, argument: WorkspaceCopy.typingPaused)
        }
        pausedHide?.cancel()
        pausedHide = Task { [weak self] in
            try? await Task.sleep(for: Self.pausedNoticeTime)
            guard !Task.isCancelled else { return }
            self?.inputResumed()
        }
    }

    func inputResumed() {
        pausedHide?.cancel()
        pausedHide = nil
        pausedNotice?.isHidden = true
    }

    private func makePausedNotice() -> UILabel {
        let label = PaddedLabel()
        label.text = WorkspaceCopy.typingPaused
        label.font = .preferredFont(forTextStyle: .footnote)
        label.adjustsFontForContentSizeCategory = true
        label.textColor = .togoSnow
        label.backgroundColor = .togoRaised
        label.layer.cornerRadius = 8
        label.layer.cornerCurve = .continuous
        label.clipsToBounds = true
        label.isUserInteractionEnabled = false
        label.isHidden = true
        label.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(label)
        NSLayoutConstraint.activate([
            label.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 8),
            label.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            label.widthAnchor.constraint(lessThanOrEqualTo: view.widthAnchor, constant: -32),
        ])
        pausedNotice = label
        return label
    }

    private func applyTheme() {
        let dark = traitCollection.userInterfaceStyle != .light
        terminal.engine.apply(dark ? choice.dark : choice.light)
        view.backgroundColor = terminal.engine.view.backgroundColor
    }
}

/// A label with room around its text, for the paused-typing notice.
private final class PaddedLabel: UILabel {
    private let insets = UIEdgeInsets(top: 6, left: 12, bottom: 6, right: 12)

    override func drawText(in rect: CGRect) {
        super.drawText(in: rect.inset(by: insets))
    }

    override var intrinsicContentSize: CGSize {
        let size = super.intrinsicContentSize
        return CGSize(width: size.width + insets.left + insets.right, height: size.height + insets.top + insets.bottom)
    }
}
