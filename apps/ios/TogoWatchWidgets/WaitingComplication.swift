import SwiftUI
import TogoPush
import WidgetKit

/// How many requests wait on the user, read from the Watch's pending store. The notification
/// service, the app and an answer reload it; each entry ends when its oldest request expires.
struct WaitingEntry: TimelineEntry {
    var date: Date
    var count: Int
    var oldestBib: String?
    var oldestAgent: String?
    var oldestTitle: String?

    static func make(_ requests: [PendingRequest], at date: Date) -> WaitingEntry {
        let waiting = RequestLifetime.waiting(requests, now: date)
        let oldest = waiting.first
        return WaitingEntry(
            date: date, count: waiting.count,
            oldestBib: oldest?.bib.map { PromptText.clean($0, maxLines: 1) }.flatMap { $0.isEmpty ? nil : $0 },
            oldestAgent: oldest?.agent.map(AgentName.display),
            oldestTitle: oldest.map { PromptText.clean($0.title, maxLines: 1) }.flatMap { $0.isEmpty ? nil : $0 }
        )
    }
}

struct WaitingProvider: TimelineProvider {
    func placeholder(in context: Context) -> WaitingEntry {
        WaitingEntry(date: .now, count: 1, oldestBib: "04", oldestAgent: "Claude Code", oldestTitle: "Run npm test?")
    }

    func getSnapshot(in context: Context, completion: @escaping (WaitingEntry) -> Void) {
        completion(context.isPreview ? placeholder(in: context) : WaitingEntry.make(Self.requests(), at: .now))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<WaitingEntry>) -> Void) {
        let requests = Self.requests()
        let now = Date.now
        // A new entry each time a request runs out, so the count drops on its own.
        let expiries = RequestLifetime.waiting(requests, now: now)
            .map { $0.receivedAt.addingTimeInterval(RequestLifetime.seconds) }
        let entries = [WaitingEntry.make(requests, at: now)] + expiries.map { WaitingEntry.make(requests, at: $0) }
        completion(Timeline(entries: entries, policy: .never))
    }

    private static func requests() -> [PendingRequest] {
        guard let directory = SharedGroup.storeDirectory(SharedGroup.watch) else { return [] }
        return PendingStore(directory: directory, clock: { Date() }).all()
    }
}

struct WaitingComplication: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "togo.watch.waiting", provider: WaitingProvider()) { entry in
            WaitingComplicationView(entry: entry)
                .containerBackground(.clear, for: .widget)
        }
        .configurationDisplayName("Waiting")
        .description("Requests waiting on you.")
        .supportedFamilies([.accessoryCircular, .accessoryCorner, .accessoryInline, .accessoryRectangular])
    }
}

struct WaitingComplicationView: View {
    let entry: WaitingEntry
    @Environment(\.widgetFamily) private var family

    /// A lit beacon with the count when something waits, an unlit ring when clear.
    private var beacon: some View {
        ZStack {
            if entry.count > 0 {
                Circle().fill(Color.togoAccent)
                Text(entry.count, format: .number)
                    .font(.system(.caption, design: .monospaced).weight(.semibold))
                    .foregroundStyle(Color.black)
            } else {
                Circle().strokeBorder(Color.secondary, lineWidth: 2)
            }
        }
        .widgetAccentable(entry.count > 0)
    }

    var body: some View {
        switch family {
        case .accessoryCircular:
            ZStack {
                AccessoryWidgetBackground()
                beacon.padding(6)
            }
        case .accessoryCorner:
            Group {
                if let bib = entry.oldestBib, entry.count > 0 {
                    Text(bib).font(.system(.title3, design: .monospaced).weight(.semibold))
                } else {
                    Circle().strokeBorder(Color.secondary, lineWidth: 2).frame(width: 18, height: 18)
                }
            }
            .widgetLabel {
                Text(Self.inline(entry.count))
            }
        case .accessoryInline:
            Text(Self.inline(entry.count))
        default:
            rectangular
        }
    }

    private var rectangular: some View {
        HStack(alignment: .top, spacing: 8) {
            beacon.frame(width: 20, height: 20)
            VStack(alignment: .leading, spacing: 1) {
                if entry.count == 0 {
                    Text(Self.allClear).font(.headline)
                } else {
                    // "04  Run npm test?"
                    HStack(spacing: 6) {
                            if let bib = entry.oldestBib {
                            // trunc-ok: a bib is two or three digits
                            Text(bib).font(.system(.headline, design: .monospaced)).lineLimit(1)
                        }
                        // trunc-ok: a complication has a fixed size: the bib and two lines of the
                        // oldest request; tapping opens the app with the whole request.
                        Text(entry.oldestTitle ?? entry.oldestAgent ?? Self.inline(entry.count))
                            .font(.headline).lineLimit(2).layoutPriority(1)
                    }
                    Text(Self.inline(entry.count)).font(.footnote).foregroundStyle(.secondary)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    static let allClear = String(localized: "watch.allClear", defaultValue: "All clear")

    /// "2 waiting", or "Nothing waiting".
    static func inline(_ count: Int) -> String {
        count == 0
            ? String(localized: "watch.complication.none", defaultValue: "Nothing waiting")
            : String(localized: "watch.complication.count", defaultValue: "\(count) waiting")
    }
}

extension Color {
    /// The waiting count when it is not zero. The Watch is always dark, so this is the
    /// accent's dark value.
    static let togoAccent = Color(red: 0x9F / 255, green: 0xD0 / 255, blue: 0xF5 / 255)
}
