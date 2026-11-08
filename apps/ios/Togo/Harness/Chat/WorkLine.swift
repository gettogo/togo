import SwiftUI
import TogoKit

/// A turn's work folded into one quiet line in SF Mono: "Worked 1m 12s  6 steps  2 files
/// changed". Tapping it opens the steps underneath, each a compact row with its files; tapping
/// a step opens its details. Shell output and subagent conversations stay in mission control.
///
/// In the main chat the live line at the foot carries the turn's clock, so a running work line
/// only counts its steps (`counting` false) and turns into "Worked …" with a check when the
/// turn ends. A subagent's thread has no live line, so its running line ticks itself.
///
/// The steps open and close as a two-phase disclosure (spec §2.1/§2.2): the parent toggles
/// `disclosure` with `Motion.toggle`, so the rows below move in the same transaction.
struct WorkLine: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let summary: WorkSummary
    let steps: [HarnessItem]
    let disclosure: Disclosure
    let onToggle: () -> Void
    let onStep: (HarnessItem) -> Void
    /// How VoiceOver reads the line ("Worked 1 minute 12 seconds, 6 steps, collapsed"). The
    /// line is one element; once open, each step after it is its own, so its details are
    /// reachable too.
    var spoken: String?
    /// "Show in terminal" as an accessibility action on the line, when its first step can.
    var showInTerminal: (() -> Void)?
    /// The running turn waits on the user: the line says "Waiting for you" in the beacon and
    /// the spinner stops. Only while `counting`.
    var waiting = false
    /// The line ticks its own clock while it runs; off where the live line ticks for it.
    var counting = true

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button(action: onToggle) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    WorkGlyph(summary: summary, waiting: isWaiting, counting: counting)
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        WorkLabel(summary: summary, waiting: isWaiting, counting: counting)
                        Image(systemName: "chevron.right")
                            .font(.caption2.weight(.semibold))
                            .rotationEffect(.degrees(disclosure.isOpen && !reduceMotion ? 90 : 0))
                            .opacity(steps.isEmpty ? 0 : 1)
                            // motion: cause → effect — the chevron turns as the person opens the steps under it.
                            .motion(.respond, value: disclosure.isOpen)
                    }
                }
                .foregroundStyle(Color.togoMute)
                .frame(minHeight: 44, alignment: .leading)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .disabled(steps.isEmpty)
            .accessibilityLabel(spoken.map { Text(verbatim: $0) } ?? Text(verbatim: ChatCopy.workLine(summary, now: .now)))
            .accessibilityActions {
                if let showInTerminal {
                    Button(ChatCopy.showInTerminal, action: showInTerminal)
                }
            }
            // The steps were underneath the line: its growing edge uncovers them in place.
            if disclosure.open, !steps.isEmpty {
                StepList(steps: steps, onStep: onStep)
                    .unfolds(disclosure)
            }
        }
        // The steps never draw outside the line's slot while it shrinks.
        .clipped()
    }

    private var isWaiting: Bool { counting && summary.running && waiting }
}

/// The line's words. While it counts (a subagent's thread, which has no live line) the clock
/// ticks at the end of the line, never animated; finished, it says "Worked 1m 12s" with the
/// waits left out.
private struct WorkLabel: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.loopsPaused) private var loopsPaused
    let summary: WorkSummary
    var waiting = false
    var counting = true

    var body: some View {
        Group {
            if waiting {
                Text(verbatim: WorkingCopy.waiting)
                    .font(.system(.footnote, design: .monospaced).weight(.semibold))
                    .foregroundStyle(Color.togoAccentText)
                    .fixedSize(horizontal: false, vertical: true)
            } else if summary.running, counting {
                HStack(alignment: .firstTextBaseline, spacing: 0) {
                    text(ChatCopy.workRunningLead(summary))
                    if let clock = summary.clock {
                        text(LiveLineLayout.separator)
                        TimelineView(.clock(from: clock.displayOrigin(waiting: false), paused: loopsPaused)) { context in
                            text(ChatCopy.clock(summary.elapsed(now: context.date) ?? 0))
                                .clockText()
                        }
                    }
                }
            } else if summary.running {
                text(ChatCopy.workCounts(summary))
            } else {
                text(ChatCopy.workLine(summary, now: .now))
            }
        }
        .contentTransition(reduceMotion ? .identity : .interpolate)
        // motion: state change — "Working" turns into "Worked …" in place as the turn ends.
        .motion(.respond, value: summary.running)
        // One driver: a note's arrival transaction moves the rows, never these words (step
        // counts and files update in place).
        .transaction { $0.animation = nil }
    }

    private func text(_ words: String) -> some View {
        Text(verbatim: words)
            .font(.system(.footnote, design: .monospaced))
            .multilineTextAlignment(.leading)
            .fixedSize(horizontal: false, vertical: true)
    }
}

/// Counting: the terminal's braille spinner, drawn as text so it sits on the words' first
/// baseline, or the still beacon while waiting on the user. Not counting (the live line has
/// the clock): a quiet dotted circle. Finished: a check that bounces once as the turn ends, or
/// a stop mark. All in one fixed slot.
private struct WorkGlyph: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ScaledMetric(relativeTo: .footnote) private var side: CGFloat = 20
    @ScaledMetric(relativeTo: .footnote) private var beacon: CGFloat = 7
    let summary: WorkSummary
    var waiting = false
    var counting = true

    @State private var finishes = 0

    var body: some View {
        ZStack {
            if waiting {
                Circle()
                    .fill(Color.togoAccent)
                    .frame(width: beacon, height: beacon)
                    .padding(3)
                    .background(Circle().fill(Color.togoAccent.opacity(0.18)))
            } else if summary.running, counting {
                LiveSpinner()
            } else {
                Image(systemName: symbol)
                    .font(.footnote)
                    .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
                    .symbolEffect(.bounce, options: .nonRepeating, value: finishes)
            }
        }
        .frame(width: side)
        // motion: continuity — the spinner becomes the turn's check in the same fixed slot, where the eye already is.
        .motion(.respond, value: GlyphKey(running: summary.running, waiting: waiting))
        .onChange(of: summary.running) { was, running in
            if was, !running, !summary.failed, !reduceMotion { finishes += 1 }
        }
        .accessibilityHidden(true)
    }

    /// One driver for the slot: whichever of the two changed.
    private struct GlyphKey: Equatable {
        var running: Bool
        var waiting: Bool
    }

    private var symbol: String {
        if summary.running { return "circle.dotted" }
        return summary.failed ? "stop.circle" : "checkmark.circle"
    }
}

/// The braille spinner in the footnote's mono, starting on its first frame. Still under Reduce
/// Motion, Low Power Mode, in a paused snapshot, and while the app is not in front.
struct LiveSpinner: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.loopsPaused) private var loopsPaused
    @Environment(\.scenePhase) private var scenePhase
    @State private var lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled
    @State private var origin = Date.now

    var body: some View {
        Group {
            if spins {
                TimelineView(.periodic(from: origin, by: RosterStatus.spinnerLoop / Double(RosterStatus.spinnerFrames.count))) { context in
                    glyph(RosterStatus.glyph(for: .working, animated: true, at: context.date.timeIntervalSince(origin)))
                }
            } else {
                glyph(RosterStatus.glyph(for: .working, animated: false))
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .NSProcessInfoPowerStateDidChange)) { _ in
            lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled
        }
    }

    private var spins: Bool {
        !reduceMotion && !loopsPaused && !lowPower && scenePhase == .active
    }

    /// The braille cells are small in any face, so the glyph is drawn a size up and heavy, in
    /// full snow, to read as a spinner rather than a faint dot.
    private func glyph(_ text: String?) -> some View {
        Text(verbatim: text ?? "")
            .font(.system(.body, design: .monospaced).weight(.heavy))
            .foregroundStyle(Color.togoSnow)
    }
}

/// The steps under an open work line, hung from one hairline.
private struct StepList: View {
    @Environment(\.displayScale) private var displayScale
    let steps: [HarnessItem]
    let onStep: (HarnessItem) -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Rectangle()
                .fill(Color.togoLine)
                .frame(width: 1 / displayScale)
                .padding(.leading, 7)
            VStack(alignment: .leading, spacing: 2) {
                ForEach(steps) { step in
                    Button { onStep(step) } label: {
                        StepRow(step: step)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }
}

/// One step: its state, its title in SF Mono, how long it took and the files it changed.
struct StepRow: View {
    let step: HarnessItem

    var body: some View {
        let info = StepInfo(step)
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: info.symbol)
                .font(.caption)
                .foregroundStyle(info.state == .failed ? Color.togoSnow : Color.togoMute)
                .frame(width: 14)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: info.title)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(Color.togoSnow)
                    // trunc-ok: a step's title; tapping the step opens its details with the full text
                    .lineLimit(2)
                ForEach(info.files, id: \.path) { file in
                    HStack(spacing: 8) {
                        // The changed file opens its diff (the chat's link router).
                        Link(destination: ChatLinks.fileURL(path: file.path, line: nil, changed: true)) {
                            PathText(file.path, style: .caption)
                        }
                        Text(verbatim: ChatCopy.lines(file))
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(Color.togoMute)
                    }
                }
            }
            Spacer(minLength: 8)
            if let time = info.duration {
                Text(verbatim: ChatCopy.clock(time))
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(Color.togoMute)
            }
        }
        .padding(.vertical, 6)
        .frame(minHeight: 44)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(info.spoken)
    }
}

/// What a step row shows, worked out once from the item.
struct StepInfo {
    var symbol: String
    var title: String
    var state: HarnessState
    var duration: TimeInterval?
    var files: [HarnessFileChange] = []
    var subject: String?
    var output: String?
    var exitCode: Int?
    var category: HarnessItem.ToolCategory?

    init(_ item: HarnessItem) {
        switch item.body {
        case .tool(let tool):
            symbol = Self.symbol(tool.state, category: tool.category)
            title = tool.title.isEmpty ? tool.name : tool.title
            state = tool.state
            duration = tool.endedAt.map { $0.date.timeIntervalSince(tool.startedAt.date) }
            files = tool.files ?? []
            subject = tool.subject?.text
            // Shell output belongs to mission control and the terminal, not the chat.
            output = tool.category == .shell ? nil : tool.output
            exitCode = tool.exitCode
            category = tool.category
        case .thinking(let text):
            symbol = "brain"
            title = ChatCopy.thinking()
            state = text.partial ? .running : .ok
            output = text.text
        case .agent(let agent):
            symbol = "person.2"
            title = [agent.name, agent.role].compactMap(\.self).joined(separator: "  ")
            state = agent.state
            duration = agent.endedAt.map { $0.date.timeIntervalSince(agent.startedAt.date) }
            output = agent.summary
        case .background(let job):
            symbol = "play.circle"
            title = job.title
            state = job.state
            subject = job.command
        case .plan(let plan):
            symbol = "list.bullet"
            title = plan.current ?? ChatCopy.planStep(plan)
            state = .ok
        default:
            symbol = "circle"
            title = item.body.kind
            state = .ok
        }
    }

    var spoken: String {
        var parts = [title, ChatCopy.stepState(state)]
        if let duration { parts.append(ChatCopy.spokenClock(duration)) }
        if !files.isEmpty { parts.append(ChatCopy.files(files.count)) }
        return parts.joined(separator: ", ")
    }

    private static func symbol(_ state: HarnessState, category: HarnessItem.ToolCategory) -> String {
        switch state {
        case .failed: return "xmark"
        case .denied, .cancelled, .interrupted: return "slash.circle"
        case .running, .waiting, .active: return "circle.dotted"
        default: break
        }
        switch category {
        case .read: return "doc.text"
        case .edit, .write: return "pencil"
        case .search: return "magnifyingglass"
        case .shell: return "chevron.right.square"
        case .web: return "globe"
        case .agent: return "person.2"
        default: return "checkmark"
        }
    }
}

/// A step's details: what it touched, how it ended and what it said, with the way to the
/// same moment in the terminal.
struct StepDetail: View {
    @Environment(\.dismiss) private var dismiss
    let step: HarnessItem
    let canShowInTerminal: Bool
    let onShowInTerminal: () -> Void

    var body: some View {
        let info = StepInfo(step)
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    if let subject = info.subject {
                        CodeWell(text: subject)
                    }
                    facts(info)
                    if !info.files.isEmpty {
                        VStack(alignment: .leading, spacing: 8) {
                            ForEach(info.files, id: \.path) { file in
                                HStack {
                                    Link(destination: ChatLinks.fileURL(path: file.path, line: nil, changed: true)) {
                                        PathText(file.path, style: .footnote)
                                    }
                                    Spacer()
                                    Text(verbatim: ChatCopy.lines(file))
                                        .font(.system(.footnote, design: .monospaced))
                                        .foregroundStyle(Color.togoMute)
                                }
                            }
                        }
                    }
                    if let output = info.output, !output.isEmpty {
                        if case .thinking = step.body {
                            MarkdownText(text: output)
                        } else {
                            CodeWell(text: output)
                        }
                    }
                }
                .padding(16)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .navigationTitle(info.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(ChatCopy.done, systemImage: "checkmark") { dismiss() }
                }
                if canShowInTerminal {
                    ToolbarItem(placement: .bottomBar) {
                        Button(ChatCopy.showInTerminal, systemImage: "terminal") {
                            dismiss()
                            onShowInTerminal()
                        }
                    }
                }
            }
        }
    }

    private func facts(_ info: StepInfo) -> some View {
        var words = [ChatCopy.stepState(info.state)]
        if let duration = info.duration { words.append(ChatCopy.clock(duration)) }
        if let code = info.exitCode { words.append(ChatCopy.exitCode(code)) }
        return Text(verbatim: words.joined(separator: "  "))
            .font(.system(.footnote, design: .monospaced))
            .foregroundStyle(Color.togoMute)
    }
}
