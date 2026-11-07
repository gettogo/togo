import SwiftUI
import TogoKit

/// A turn's work folded into one quiet line in SF Mono: "Worked 1m 12s  6 steps  2 files
/// changed". Tapping it opens the steps underneath, each a compact row with its files; tapping
/// a step opens its details. Shell output and subagent conversations stay in mission control.
struct WorkLine: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let summary: WorkSummary
    let steps: [HarnessItem]
    let expanded: Bool
    let onToggle: () -> Void
    let onStep: (HarnessItem) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button(action: onToggle) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    WorkGlyph(summary: summary)
                    WorkLabel(summary: summary)
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .rotationEffect(.degrees(expanded && !reduceMotion ? 90 : 0))
                        .opacity(steps.isEmpty ? 0 : 1)
                }
                .foregroundStyle(Color.togoMute)
                .frame(minHeight: 44, alignment: .leading)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .disabled(steps.isEmpty)
            if expanded, !steps.isEmpty {
                StepList(steps: steps, onStep: onStep)
                    .transition(.opacity)
            }
        }
        .motion(.rowGrow, value: expanded)
    }
}

/// The line's words; ticks once a second while the turn runs.
private struct WorkLabel: View {
    let summary: WorkSummary

    var body: some View {
        if summary.running {
            TimelineView(.periodic(from: .now, by: 1)) { context in
                text(at: context.date)
            }
        } else {
            text(at: .now)
        }
    }

    private func text(at now: Date) -> some View {
        Text(verbatim: ChatCopy.workLine(summary, now: now))
            .font(.system(.footnote, design: .monospaced))
            .multilineTextAlignment(.leading)
            .fixedSize(horizontal: false, vertical: true)
    }
}

/// Running: the system's small spinner. Finished: a quiet check, or a stop mark.
private struct WorkGlyph: View {
    let summary: WorkSummary

    var body: some View {
        Group {
            if summary.running {
                ProgressView()
                    .controlSize(.mini)
            } else {
                Image(systemName: summary.failed ? "stop.circle" : "checkmark.circle")
                    .font(.footnote)
            }
        }
        .frame(width: 16)
        .accessibilityHidden(true)
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
                    .lineLimit(2)
                ForEach(info.files, id: \.path) { file in
                    HStack(spacing: 8) {
                        PathText(file.path, style: .caption)
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
                                    PathText(file.path, style: .footnote)
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
        .presentationDetents([.medium, .large])
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
