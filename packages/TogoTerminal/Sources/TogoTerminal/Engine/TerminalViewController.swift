import QuartzCore
import UIKit

/// Hosts one engine's view inside the safe area and above the keyboard, so showing the
/// keyboard shrinks the grid and reaches the remote as a resize. The canvas is opaque: glass
/// belongs to chrome only.
@MainActor public final class TerminalViewController: UIViewController {
    public let engine: SwiftTermEngine

    /// Off by default: a running display link holds the display at its top refresh rate.
    public var recordsFrameStats = false {
        didSet { updateDisplayLink() }
    }
    public private(set) var frameStats = FrameStats()

    private var displayLink: CADisplayLink?

    public init(engine: SwiftTermEngine) {
        self.engine = engine
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) {
        nil
    }

    override public func viewDidLoad() {
        super.viewDidLoad()
        let terminal = engine.view
        terminal.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(terminal)
        NSLayoutConstraint.activate([
            terminal.leadingAnchor.constraint(equalTo: view.safeAreaLayoutGuide.leadingAnchor),
            terminal.trailingAnchor.constraint(equalTo: view.safeAreaLayoutGuide.trailingAnchor),
            terminal.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            terminal.bottomAnchor.constraint(equalTo: view.keyboardLayoutGuide.topAnchor),
        ])
    }

    /// The margins around the grid take the theme background, which can change at any time.
    override public func viewWillLayoutSubviews() {
        super.viewWillLayoutSubviews()
        view.backgroundColor = engine.view.backgroundColor
    }

    override public func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        engine.useMetalIfAvailable()
        engine.view.becomeFirstResponder()
        updateDisplayLink()
    }

    override public func viewDidDisappear(_ animated: Bool) {
        super.viewDidDisappear(animated)
        displayLink?.invalidate()
        displayLink = nil
    }

    private func updateDisplayLink() {
        guard recordsFrameStats, viewIfLoaded?.window != nil else {
            displayLink?.invalidate()
            displayLink = nil
            return
        }
        guard displayLink == nil else { return }
        frameStats = FrameStats()
        let link = CADisplayLink(target: self, selector: #selector(displayTick(_:)))
        link.preferredFrameRateRange = CAFrameRateRange(minimum: 80, maximum: 120, preferred: 120)
        link.add(to: .main, forMode: .common)
        displayLink = link
    }

    @objc private func displayTick(_ link: CADisplayLink) {
        frameStats.record(timestamp: link.timestamp, targetTimestamp: link.targetTimestamp)
    }
}
