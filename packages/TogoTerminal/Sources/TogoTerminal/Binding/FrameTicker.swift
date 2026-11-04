import QuartzCore

@MainActor public protocol FrameTicker: AnyObject {
    var onFrame: () -> Void { get set }
    func start()
    func stop()
}

/// Calls `onFrame` once per display refresh, up to 120 Hz on ProMotion screens.
@MainActor public final class DisplayLinkTicker: FrameTicker {
    public var onFrame: () -> Void = {}
    /// When set, every frame's timestamps go into `frameStats`.
    public var recordsFrameStats = false
    public private(set) var frameStats = FrameStats()
    private var link: CADisplayLink?

    public init() {}

    public func start() {
        guard link == nil else { return }
        let link = CADisplayLink(target: DisplayLinkTarget(owner: self), selector: #selector(DisplayLinkTarget.tick(_:)))
        link.preferredFrameRateRange = CAFrameRateRange(minimum: 60, maximum: 120, preferred: 120)
        link.add(to: .main, forMode: .common)
        self.link = link
    }

    public func stop() {
        link?.invalidate()
        link = nil
    }

    public func resetFrameStats() {
        frameStats = FrameStats()
    }

    fileprivate func tick(_ link: CADisplayLink) {
        if recordsFrameStats {
            frameStats.record(timestamp: link.timestamp, targetTimestamp: link.targetTimestamp)
        }
        onFrame()
    }

    isolated deinit {
        link?.invalidate()
    }
}

/// CADisplayLink retains its target; this weak hop lets the ticker be released while running.
@MainActor private final class DisplayLinkTarget: NSObject {
    weak var owner: DisplayLinkTicker?

    init(owner: DisplayLinkTicker) {
        self.owner = owner
    }

    @objc func tick(_ link: CADisplayLink) {
        owner?.tick(link)
    }
}
