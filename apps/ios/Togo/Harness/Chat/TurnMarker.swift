import SwiftUI
import TogoKit

/// The quiet line that opens a turn the agent started on its own, after a background task or
/// subagent it launched finished: "Subagent finished · 2m later", between two hairlines, so
/// the follow-up never reads as part of the reply before it.
struct TurnMarkerRow: View {
    let notice: HarnessItem.Notice
    /// The silence before it, when longer than `ChatGaps.threshold`.
    var gap: TimeInterval?

    var body: some View {
        HStack(spacing: 10) {
            hairline
            Text(verbatim: TurnMarkerCopy.marker(notice.text, gap: gap))
                .font(.footnote)
                .foregroundStyle(Color.togoMute)
                .multilineTextAlignment(.center)
                .layoutPriority(1)
            hairline
        }
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(TurnMarkerCopy.spokenMarker(notice.text, gap: gap))
    }

    private var hairline: some View {
        Rectangle()
            .fill(Color.togoLine)
            .frame(height: 1)
            .frame(minWidth: 12, maxWidth: .infinity)
            .accessibilityHidden(true)
    }
}

/// When a row comes after a pause: its time, small and centered above it.
struct ChatTimestamp: View {
    let date: Date

    var body: some View {
        Text(verbatim: TurnMarkerCopy.stamp(date, now: .now))
            .font(.caption)
            .foregroundStyle(Color.togoMuteDim)
            .frame(maxWidth: .infinity, alignment: .center)
            .accessibilityLabel(TurnMarkerCopy.spokenStamp(date))
    }
}

enum TurnMarkerCopy {
    /// "Subagent finished · 2m later", or the trigger alone after a short pause.
    static func marker(_ trigger: String, gap: TimeInterval?) -> String {
        guard let gap, gap > ChatGaps.threshold else { return trigger }
        return trigger + " · " + later(gap)
    }

    static func spokenMarker(_ trigger: String, gap: TimeInterval?) -> String {
        guard let gap, gap > ChatGaps.threshold else { return trigger }
        let span = Duration.seconds(Int(gap.rounded(.down)) / 60 * 60)
            .formatted(.units(allowed: [.days, .hours, .minutes], width: .wide, maximumUnitCount: 2))
        return trigger + ", " + String(localized: "chat.marker.laterSpoken", defaultValue: "\(span) later")
    }

    /// "2m later", "1h 4m later", "3d later".
    static func later(_ seconds: TimeInterval) -> String {
        let minutes = Int(seconds.rounded(.down)) / 60
        let span: String
        switch minutes {
        case ..<60: span = "\(max(minutes, 1))m"
        case ..<(24 * 60): span = minutes % 60 == 0 ? "\(minutes / 60)h" : "\(minutes / 60)h \(minutes % 60)m"
        default: span = "\(minutes / (24 * 60))d"
        }
        return String(localized: "chat.marker.later", defaultValue: "\(span) later")
    }

    /// "8:29 PM" today, "Yesterday 8:29 PM", else "Nov 3, 8:29 PM".
    static func stamp(_ date: Date, now: Date, calendar: Calendar = .current) -> String {
        let time = date.formatted(date: .omitted, time: .shortened)
        if calendar.isDate(date, inSameDayAs: now) { return time }
        if let yesterday = calendar.date(byAdding: .day, value: -1, to: now), calendar.isDate(date, inSameDayAs: yesterday) {
            return String(localized: "chat.stamp.yesterday", defaultValue: "Yesterday \(time)")
        }
        return date.formatted(.dateTime.month(.abbreviated).day()) + ", " + time
    }

    static func spokenStamp(_ date: Date) -> String {
        date.formatted(date: .complete, time: .shortened)
    }
}
