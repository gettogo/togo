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
            // The system animates between entries: the beacon lights or goes out, the count rolls.
            if entry.count > 0 {
                Circle().fill(Color.togoAccent)
                    // motion: state change — the system crossfades between entries in place; nothing moves.
                    .transition(.opacity)
                Text(entry.count, format: .number)
                    .font(.system(.caption, design: .monospaced).weight(.semibold))
                    .foregroundStyle(Color.black)
                    .contentTransition(.numericText(value: Double(entry.count)))
            } else {
                Circle().strokeBorder(Color.secondary, lineWidth: 2)
                    // motion: state change — the system crossfades between entries in place; nothing moves.
                    .transition(.opacity)
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
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text(Self.spoken(entry)))
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
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text(Self.spoken(entry)))
        case .accessoryInline:
            Text(Self.inline(entry.count))
                .contentTransition(.interpolate)
        default:
            rectangular
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(Text(Self.spoken(entry)))
        }
    }

    private var rectangular: some View {
        HStack(alignment: .top, spacing: 8) {
            beacon.frame(width: 20, height: 20)
            VStack(alignment: .leading, spacing: 1) {
                if entry.count == 0 {
                    Text(Self.allClear).font(.headline)
                        // motion: state change — the system crossfades between entries in place; nothing moves.
                        .transition(.opacity)
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
                            // The request can quote a command: hidden while the Watch is locked or dimmed.
                            .privacySensitive()
                    }
                    Text(Self.inline(entry.count)).font(.footnote).foregroundStyle(.secondary)
                        .contentTransition(.numericText(value: Double(entry.count)))
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    static let allClear = String(localized: "watch.allClear", defaultValue: "All clear")

    /// What VoiceOver says for any family: how many wait, and the oldest one's session and words.
    static func spoken(_ entry: WaitingEntry) -> String {
        guard entry.count > 0 else { return String(localized: "watch.complication.spoken.none", defaultValue: "Nothing needs you") }
        let count = entry.count == 1
            ? String(localized: "watch.complication.spoken.one", defaultValue: "1 request needs you")
            : String(localized: "watch.complication.spoken.many", defaultValue: "\(entry.count) requests need you")
        guard let bib = entry.oldestBib else { return count }
        let oldest = entry.oldestTitle.map {
            String(localized: "watch.complication.spoken.oldest", defaultValue: "Session \(bib): \($0)")
        } ?? String(localized: "watch.complication.spoken.session", defaultValue: "Session \(bib)")
        return "\(count). \(oldest)"
    }

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
