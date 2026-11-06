import Foundation
import SwiftUI
import TogoPush

/// A session's state as a Live Activity shows it. Anything outside the known set reads
/// "Waiting", so a newer helper can never put a raw value on the lock screen. A stopped agent
/// reads as the app says it, using the exit reason word the activity carries; a reason this
/// build does not know reads as the plain status. An activity past
/// its stale date no longer claims anything current: a request reads as expired, anything
/// else as not updated, both without the accent or a running clock.
enum LiveStatus: Equatable {
    case needsYou, working, done, ended, failed, offline, other, expired, notUpdated

    init(_ raw: String, stale: Bool = false) {
        if stale {
            self = raw == "needs_you" ? .expired : .notUpdated
            return
        }
        switch raw {
        case "needs_you": self = .needsYou
        case "working": self = .working
        case "done": self = .done
        case "ended": self = .ended
        case "failed": self = .failed
        case "offline": self = .offline
        default: self = .other
        }
    }

    var word: String {
        switch self {
        case .needsYou: String(localized: "live.status.needsYou", defaultValue: "Needs you")
        case .working: String(localized: "live.status.working", defaultValue: "Working")
        case .done: String(localized: "live.status.done", defaultValue: "Done")
        case .ended: LifecycleWords.ended
        case .failed: LifecycleWords.error
        case .offline: String(localized: "live.status.offline", defaultValue: "Offline")
        case .other: String(localized: "live.status.other", defaultValue: "Waiting")
        case .expired: String(localized: "live.status.expired", defaultValue: "Request expired")
        case .notUpdated: String(localized: "live.status.notUpdated", defaultValue: "Not updated")
        }
    }

    /// The status in words, with the exit reason word for a stopped agent. The reason only
    /// matters for ended and failed; `exit` says nothing the status does not, and any word
    /// this build does not know reads as the plain status.
    func word(reason: String?) -> String {
        let status: String
        switch self {
        case .ended: status = "ended"
        case .failed: status = "failed"
        default: return word
        }
        return LifecycleWords.status(status: status, reason: LifecycleWords.knownReason(reason), code: nil) ?? word
    }

    /// A finished session has nothing to time, and a stale one cannot say how long.
    var showsElapsed: Bool { self == .needsYou || self == .working || self == .other }

    /// The waiting count only means something while it is current.
    var showsCount: Bool { self != .expired && self != .notUpdated }

    /// The compact island has room for one thing: while the machine is silent that is the word
    /// "Offline", not a count.
    var showsCompactCount: Bool { showsCount && self != .offline }
}

/// The words and links the Live Activity and the waiting widget share.
enum LiveCopy {
    /// Shown when the pairing record for an activity's machine is gone or unreadable.
    static let unknownMachine = String(localized: "live.machine.unknown", defaultValue: "Mac")

    static func waiting(_ count: Int) -> String {
        String(localized: "live.waiting", defaultValue: "\(count) waiting")
    }

    static let nothingWaiting = String(localized: "live.nothingWaiting", defaultValue: "Nothing waiting")

    /// The word under a count on its own, as in the circular widget.
    static let waitingWord = String(localized: "live.waitingWord", defaultValue: "waiting")

    /// "Claude Code · Studio": the one separator the line gets.
    static func agentOnMachine(agent: String, machine: String) -> String {
        let agent = agent.trimmingCharacters(in: .whitespacesAndNewlines)
        return agent.isEmpty ? machine : "\(agent) · \(machine)"
    }

    /// Command text cut to what a glance can hold. Only set for machines that opted in.
    static func command(_ raw: String?) -> String? {
        guard let raw else { return nil }
        let clean = PromptText.clean(raw, maxLines: 2)
        return clean.isEmpty ? nil : clean
    }

    /// The machine's name from its pairing record.
    static func machineName(hostId: String, pairings: PairingStore?) -> String {
        pairings?.pairing(hostId: hostId)?.machineName ?? unknownMachine
    }

    /// The pairings the app shares with its extensions; nil without the app group.
    static func sharedPairings() -> PairingStore? {
        SharedGroup.storeDirectory(SharedGroup.ios).map(PairingStore.init(directory:))
    }

    /// `togo://session/<bib>?host=<machine>`: the link a tap opens, the same one notifications
    /// carry, so the app lands on the session that is asking.
    static func link(bib: String?, machine: String?) -> URL? {
        guard let bib, !bib.isEmpty, var parts = URLComponents(string: "togo://session/") else { return nil }
        parts.path = "/" + bib
        if let machine, !machine.isEmpty, machine != unknownMachine {
            parts.queryItems = [URLQueryItem(name: "host", value: machine)]
        }
        return parts.url
    }

    /// The system draws Live Activities and widgets no larger than this; capping here keeps
    /// the layout the same as what the lock screen shows.
    static let largestType = DynamicTypeSize.xxxLarge

    static func sinceDate(_ since: Int) -> Date { Date(timeIntervalSince1970: TimeInterval(since)) }
}

extension EnvironmentValues {
    /// Stops the elapsed time at this moment, so a snapshot renders the same every run.
    @Entry var liveClockFrozenAt: Date?
}

/// Time since a status began, counting up. Digits keep their width so the line does not jitter.
struct LiveElapsed: View {
    @Environment(\.liveClockFrozenAt) private var frozenAt
    let since: Int

    var body: some View {
        let start = LiveCopy.sinceDate(since)
        Group {
            if let frozenAt {
                Text(verbatim: Self.format(frozenAt.timeIntervalSince(start)))
            } else {
                Text(timerInterval: start...Date.distantFuture, countsDown: false)
            }
        }
        .monospacedDigit()
        .lineLimit(1)
    }

    static func format(_ seconds: TimeInterval) -> String {
        let duration = Duration.seconds(max(0, seconds.rounded(.down)))
        let pattern: Duration.TimeFormatStyle.Pattern = seconds >= 3600 ? .hourMinuteSecond : .minuteSecond
        return duration.formatted(.time(pattern: pattern))
    }
}
