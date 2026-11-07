import SwiftUI
import TogoKit
import TogoPush

/// A session's status in words. "Needs you" is the one place a row uses the accent.
struct StatusWord: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let status: SessionStatus
    /// How the agent stopped, which words an ended or failed status.
    var exit: SessionExit? = nil
    var font: Font = .subheadline

    var body: some View {
        Text(Self.label(status, exit: exit))
            .font(font)
            .fontWeight(status == .needsYou ? .semibold : .regular)
            .foregroundStyle(status == .needsYou ? Color.togoAccentText : Color.togoMute)
            .contentTransition(reduceMotion ? .identity : .interpolate)
            // motion: state change — the status word changes in place; nothing moves.
            .motion(.fade, value: Self.label(status, exit: exit))
            // trunc-ok: a status word from a fixed short set; ended states say a short phrase
            .lineLimit(1)
    }

    /// An ended or failed session says how it stopped: "Stopped with an error".
    nonisolated static func label(_ status: SessionStatus, exit: SessionExit?) -> String {
        LifecycleCopy.status(status, exit: exit)
    }

    nonisolated static func label(_ status: SessionStatus) -> String {
        switch status {
        case .working: String(localized: "Working")
        case .needsYou: String(localized: "Needs you")
        case .done: String(localized: "Done")
        case .idle: String(localized: "Idle")
        case .exited, .ended: LifecycleWords.ended
        case .failed: LifecycleWords.error
        case .unknown(let raw): readable(raw)
        }
    }

    private nonisolated static func readable(_ raw: String) -> String {
        let words = raw.replacingOccurrences(of: "_", with: " ").replacingOccurrences(of: "-", with: " ")
        return words.prefix(1).uppercased() + words.dropFirst()
    }
}

extension SessionRow {
    /// What VoiceOver reads for a row: "04, Claude Code, Needs you, in ~/src/app".
    var spokenSummary: String {
        let title = name.isEmpty ? agentName : name
        let parts = [bib, title, StatusWord.label(status, exit: exit), "in \(PathText.abbreviate(cwd))"]
        return parts.compactMap { $0 }.joined(separator: ", ")
    }
}
