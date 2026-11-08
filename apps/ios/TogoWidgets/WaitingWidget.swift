import SwiftUI
import TogoPush
import WidgetKit

/// What the waiting widget shows: how many requests are open and which session has waited
/// longest. Never the question or the command.
struct WaitingSummary: Equatable {
    struct Oldest: Equatable {
        var bib: String?
        var agent: String
        var link: URL?
    }

    var count: Int
    var oldest: Oldest?

    /// The helper refuses answers after this long, so older requests no longer count.
    static let openFor: TimeInterval = 600

    init(count: Int, oldest: Oldest?) {
        self.count = count
        self.oldest = oldest
    }

    init(requests: [PendingRequest], now: Date, machineName: (String) -> String) {
        let open = Self.open(requests, now: now)
        count = open.count
        oldest = open.first.map { request in
            Oldest(
                bib: request.bib, agent: request.agent ?? "",
                link: LiveCopy.link(bib: request.bib, machine: request.host ?? machineName(request.hostId))
            )
        }
    }

    /// Unanswered requests still inside their window, oldest first.
    static func open(_ requests: [PendingRequest], now: Date) -> [PendingRequest] {
        requests
            .filter { $0.answered == nil && now.timeIntervalSince($0.receivedAt) < openFor }
            .sorted { $0.receivedAt < $1.receivedAt }
    }
}

struct WaitingEntry: TimelineEntry {
    var date: Date
    var summary: WaitingSummary
}

/// Reads the requests the notification service and the app keep in the app group. A new entry
/// is due each time a request ages out; new requests reload the timeline from where they land.
struct WaitingProvider: TimelineProvider {
    func placeholder(in context: Context) -> WaitingEntry {
        WaitingEntry(date: .now, summary: WaitingSummary(
            count: 1, oldest: .init(bib: "04", agent: "Claude Code", link: nil)
        ))
    }

    func getSnapshot(in context: Context, completion: @escaping (WaitingEntry) -> Void) {
        completion(context.isPreview ? placeholder(in: context) : entry(at: .now, requests: Self.requests()))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<WaitingEntry>) -> Void) {
        let now = Date.now
        let requests = Self.requests()
        let expiries = WaitingSummary.open(requests, now: now).map { $0.receivedAt.addingTimeInterval(WaitingSummary.openFor) }
        let dates = [now] + Array(Set(expiries)).sorted()
        let entries = dates.map { entry(at: $0, requests: requests) }
        completion(Timeline(entries: entries, policy: .after(now.addingTimeInterval(15 * 60))))
    }

    private func entry(at date: Date, requests: [PendingRequest]) -> WaitingEntry {
        let pairings = LiveCopy.sharedPairings()
        return WaitingEntry(date: date, summary: WaitingSummary(requests: requests, now: date) {
            LiveCopy.machineName(hostId: $0, pairings: pairings)
        })
    }

    private static func requests() -> [PendingRequest] {
        guard let directory = SharedGroup.storeDirectory(SharedGroup.ios) else { return [] }
        return PendingStore(directory: directory, clock: { .now }).all()
    }
}

struct WaitingWidget: Widget {
    static let kind = WidgetKinds.waiting

    var body: some WidgetConfiguration {
        StaticConfiguration(kind: Self.kind, provider: WaitingProvider()) { entry in
            WaitingWidgetView(summary: entry.summary)
        }
        .configurationDisplayName(String(localized: "live.widget.name", defaultValue: "Waiting"))
        .description(String(localized: "live.widget.description", defaultValue: "How many sessions need you."))
        .supportedFamilies([.systemSmall, .accessoryRectangular, .accessoryCircular])
    }
}

struct WaitingWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let summary: WaitingSummary

    var body: some View {
        Group {
            switch family {
            case .accessoryRectangular: WaitingRectangularView(summary: summary)
            case .accessoryCircular: WaitingCircularView(summary: summary)
            default: WaitingSmallView(summary: summary)
            }
        }
        .dynamicTypeSize(...LiveCopy.largestType)
        .containerBackground(Color.togoInk, for: .widget)
        .widgetURL(summary.oldest?.link)
    }
}

/// The oldest waiting session: bib in mono, then the agent.
private struct OldestLine: View {
    let oldest: WaitingSummary.Oldest
    var font: Font.TextStyle = .footnote

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            if let bib = oldest.bib {
                Text(verbatim: bib)
                    .font(.system(font, design: .monospaced))
                    .monospacedDigit()
                    // trunc-ok: a bib is two or three digits
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
            Text(verbatim: oldest.agent)
                .font(.system(font))
                // trunc-ok: a widget has a fixed size; tapping opens the waiting request with its agent
                .lineLimit(1)
                .layoutPriority(1)
        }
    }
}

struct WaitingSmallView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let summary: WaitingSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if summary.count == 0 {
                Spacer(minLength: 0)
                Text(LiveCopy.nothingWaiting)
                    .font(.headline)
                    .foregroundStyle(Color.togoMute)
                    // motion: state change — the system crossfades between entries in place; nothing moves.
                    .transition(.opacity)
            } else {
                Text(verbatim: "\(summary.count)")
                    .font(.system(size: 48, weight: .semibold))
                    .monospacedDigit()
                    // accent-ok: the count of requests that need the user.
                    .foregroundStyle(Color.togoAccentText)
                    .contentTransition(reduceMotion ? .identity : .numericText(value: Double(summary.count)))
                Text(LiveCopy.waitingWord)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoSnow)
                Spacer(minLength: 0)
                if let oldest = summary.oldest {
                    OldestLine(oldest: oldest)
                        .foregroundStyle(Color.togoMute)
                        // motion: state change — the system crossfades between entries in place; nothing moves.
                        .transition(.opacity)
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .dynamicTypeSize(...LiveCopy.largestType)
        .accessibilityElement(children: .combine)
    }
}

struct WaitingRectangularView: View {
    let summary: WaitingSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(summary.count == 0 ? LiveCopy.nothingWaiting : LiveCopy.waiting(summary.count))
                .font(.headline)
                .contentTransition(.interpolate)
                .widgetAccentable()
                // trunc-ok: "3 waiting" or "Nothing waiting"
                .lineLimit(1)
            if summary.count > 0, let oldest = summary.oldest {
                OldestLine(oldest: oldest, font: .subheadline)
                    // motion: state change — the system crossfades between entries in place; nothing moves.
                    .transition(.opacity)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

struct WaitingCircularView: View {
    let summary: WaitingSummary

    var body: some View {
        ZStack {
            AccessoryWidgetBackground()
            VStack(spacing: -2) {
                Text(verbatim: "\(summary.count)")
                    .font(.title2)
                    .fontWeight(.semibold)
                    .monospacedDigit()
                    .contentTransition(.numericText(value: Double(summary.count)))
                    .widgetAccentable()
                Text(LiveCopy.waitingWord)
                    .font(.caption2)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(summary.count == 0 ? LiveCopy.nothingWaiting : LiveCopy.waiting(summary.count))
    }
}
