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
        onStop: @escaping @MainActor (ObjectIdentifier) -> Void
    ) {
        engine = SwiftTermEngine(settings: Self.terminalSettings(settings), theme: BuiltInThemes.all[0])
        engine.view.accessibilityLabel = WorkspaceCopy.terminal
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
        binding = TerminalBinding(
            engine: engine, ticker: DisplayLinkTicker(),
            read: { await link.read() },
            send: { await link.send($0) },
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
        let gestures = controller.terminal.gestures
        gestures.onSwipeSession = { [weak handle] in handle?.onSwipe($0 == .left ? 1 : -1) }
        gestures.onEdgeSwipe = { [weak handle] in handle?.onEdgeSwipe() }
        gestures.onFontSize = { [weak handle] in handle?.onFontSize(Double($0)) }
        feed = Task {
            for await state in link.states {
                sink.yield(Self.phase(state, asked: wasAsked()))
            }
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
            themeID: settings.themeID
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

    static func phase(_ state: LinkState, asked: Bool = false) -> LinkPhase {
        switch state {
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
final class ThemedTerminalController: UIViewController {
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

    private func applyTheme() {
        let dark = traitCollection.userInterfaceStyle != .light
        terminal.engine.apply(dark ? choice.dark : choice.light)
        view.backgroundColor = terminal.engine.view.backgroundColor
    }
}
