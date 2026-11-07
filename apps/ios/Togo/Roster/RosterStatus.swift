import SwiftUI
import TogoKit

/// What a status looks like on a roster line and in the terminal header.
enum RosterState: Equatable, Sendable {
    case needsYou, working, done, connecting, idle, ended, failed

    init(_ status: SessionStatus) {
        switch status {
        case .needsYou: self = .needsYou
        case .working: self = .working
        case .done: self = .done
        case .exited, .ended: self = .ended
        case .failed: self = .failed
        case .idle, .unknown: self = .idle
        }
    }
}

/// A session's status as a glyph and a word. The beacon marks "Needs you", the only accent;
/// "Working" has a braille spinner in the terminal's own idiom that turns only when `animated`
/// is set (one line at a time) and stays still under Reduce Motion or Low Power Mode; "Done" is
/// the prompt `>`, never a check or a green; "Connecting" is the paw trail; only a failure word
/// is red.
struct RosterStatus: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ScaledMetric(relativeTo: .footnote) private var beaconSide: CGFloat = 7
    @Environment(\.loopsPaused) private var paused
    @State private var lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled

    let state: RosterState
    let word: String
    var animated = false
    var font: Font = .subheadline

    init(status: SessionStatus, exit: SessionExit? = nil, animated: Bool = false, font: Font = .subheadline) {
        state = RosterState(status)
        word = StatusWord.label(status, exit: exit)
        self.animated = animated
        self.font = font
    }

    /// The link is still being made; there is no session status yet.
    init(connecting word: String = WorkspaceCopy.connecting, font: Font = .subheadline) {
        state = .connecting
        self.word = word
        self.font = font
    }

    // MARK: Glyphs

    static let stillSpinner = "\u{283F}"
    static let prompt = ">"
    /// One turn of the spinner takes one second.
    static let spinnerFrames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"]
    static let spinnerLoop: TimeInterval = 1
    /// The spinner's frames are read from the reference date, so its schedule starts there too:
    /// a tick lands on every frame, and a re-render never re-anchors it.
    static let spinnerEpoch = Date(timeIntervalSinceReferenceDate: 0)

    /// The mono glyph before the word, if the state has one. Only "Working" has frames.
    static func glyph(for state: RosterState, animated: Bool, at time: TimeInterval = 0) -> String? {
        switch state {
        case .working:
            guard animated else { return stillSpinner }
            let progress = time.truncatingRemainder(dividingBy: spinnerLoop) / spinnerLoop
            return spinnerFrames[min(spinnerFrames.count - 1, Int(progress * Double(spinnerFrames.count)))]
        case .done: return prompt
        default: return nil
        }
    }

    private var spins: Bool {
        animated && state == .working && !reduceMotion && !paused && !lowPower
    }

    // MARK: Body

    var body: some View {
        HStack(spacing: 6) {
            leading
            Text(word)
                .font(font)
                .fontWeight(state == .needsYou ? .semibold : .regular)
                .foregroundStyle(wordColor)
                .contentTransition(reduceMotion ? .identity : .interpolate)
                // motion: state change — the row's status word changes in place; nothing moves.
                .motion(.fade, value: word)
                // trunc-ok: a status word from a fixed short set
                .lineLimit(1)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(word)
        .onReceive(NotificationCenter.default.publisher(for: .NSProcessInfoPowerStateDidChange)) { _ in
            lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled
        }
    }

    @ViewBuilder private var leading: some View {
        switch state {
        case .needsYou:
            Circle()
                .fill(Color.togoAccent)
                .frame(width: beaconSide, height: beaconSide)
                .padding(4)
                .background(Circle().fill(Color.togoAccent.opacity(0.18)))
        case .working:
            if spins {
                TimelineView(.periodic(from: Self.spinnerEpoch, by: Self.spinnerLoop / Double(Self.spinnerFrames.count))) { context in
                    mono(Self.glyph(for: .working, animated: true, at: context.date.timeIntervalSinceReferenceDate))
                }
            } else {
                mono(Self.glyph(for: .working, animated: false))
            }
        case .done:
            mono(Self.prompt)
        case .connecting:
            PawWalk()
        case .idle, .ended, .failed:
            EmptyView()
        }
    }

    private func mono(_ glyph: String?) -> some View {
        Text(verbatim: glyph ?? "")
            .font(.system(.subheadline, design: .monospaced))
            .foregroundStyle(Color.togoMute)
    }

    private var wordColor: Color {
        switch state {
        case .needsYou: .togoAccentText
        case .failed: .red
        default: .togoMute
        }
    }
}
