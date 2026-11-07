import SwiftUI
import TogoKit

/// A background shell, dev server or job in mission control: its title and command, how long
/// it has run, and its live tail in SF Mono, which follows the row as mission control updates
/// it. Tapping opens a live terminal on it, or its output as a Live list for work without a
/// pane of its own; with neither, the session's terminal.
struct BackgroundTerminalCard: View {
    let row: HarnessRow
    let context: PrimitiveContext

    var body: some View {
        BackgroundJobCard(job: BackgroundJob(row: row)) { open(row) }
    }

    private func open(_ row: HarnessRow) {
        if let target = row.open { context.open(target) } else { context.showTerminal() }
    }
}

/// The card itself, for a job read from a row or an item.
struct BackgroundJobCard: View {
    @Environment(\.loopsPaused) private var loopsPaused
    let job: BackgroundJob
    let onOpen: () -> Void
    @ScaledMetric(relativeTo: .body) private var tile: CGFloat = 26

    var body: some View {
        let look = PrimitiveStateLook(job.state)
        Button(action: onOpen) {
            VStack(alignment: .leading, spacing: 8) {
                HStack(alignment: .top, spacing: 10) {
                    Image(systemName: symbol)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(look.color)
                        .frame(width: tile, height: tile)
                        .background(look.color.opacity(0.12), in: .rect(cornerRadius: 8, style: .continuous))
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: job.title)
                            .font(.body.weight(.semibold))
                            .foregroundStyle(Color.togoSnow)
                        if let command = job.command, !command.isEmpty, command != job.title {
                            Text(verbatim: command)
                                .font(.mono(.caption))
                                .foregroundStyle(Color.togoMute)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    elapsed
                    Image(systemName: job.terminal != nil ? "terminal" : "chevron.right")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(Color.togoMute)
                        .padding(.top, 4)
                }
                tail
                    .padding(.leading, tile + 10)
            }
            .padding(14)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([job.title, job.command].compactMap { $0 }.joined(separator: ", "))
        .accessibilityValue([look.label, job.tail.last].compactMap { $0 }.joined(separator: ", "))
        .accessibilityHint(job.terminal != nil ? TerminalSyncCopy.openTerminalHint : "")
        .accessibilityAddTraits(.isButton)
    }

    private var symbol: String {
        job.state.isLive ? "play.rectangle" : PrimitiveStateLook(job.state).symbol
    }

    @ViewBuilder private var tail: some View {
        if job.tail.isEmpty {
            if job.state.isLive {
                Text(TerminalSyncCopy.noOutput)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
            }
        } else {
            Text(verbatim: job.tail.joined(separator: "\n"))
                .font(.mono(.caption2))
                .foregroundStyle(Color.togoSnow.opacity(0.8))
                .lineLimit(BackgroundJob.tailLines)
                .primitiveWell(radius: 10)
        }
    }

    @ViewBuilder private var elapsed: some View {
        if let started = job.startedAt, job.state.isLive {
            if loopsPaused {
                Text(verbatim: PrimitiveElapsed.string(0)).hidden()
            } else {
                TimelineView(.periodic(from: started, by: 1)) { timeline in
                    Text(verbatim: PrimitiveElapsed.string(timeline.date.timeIntervalSince(started)))
                        .font(.mono(.caption))
                        .monospacedDigit()
                        .foregroundStyle(Color.togoMute)
                }
            }
        }
    }
}
