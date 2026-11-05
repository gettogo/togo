import SwiftUI
import TogoKit

/// A session's status in words. "Needs you" is the one place a row uses the accent.
struct StatusWord: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let status: SessionStatus
    var font: Font = .subheadline

    var body: some View {
        Text(Self.label(status))
            .font(font)
            .fontWeight(status == .needsYou ? .semibold : .regular)
            .foregroundStyle(status == .needsYou ? Color.togoAccentText : Color.togoMute)
            .contentTransition(reduceMotion ? .identity : .interpolate)
            .animation(Motion.stateText(reduce: reduceMotion), value: status)
            .lineLimit(1)
    }

    nonisolated static func label(_ status: SessionStatus) -> String {
        switch status {
        case .working: String(localized: "Working")
        case .needsYou: String(localized: "Needs you")
        case .done: String(localized: "Done")
        case .idle: String(localized: "Idle")
        case .exited: String(localized: "Ended")
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
        let parts = [bib, title, StatusWord.label(status), "in \(PathText.abbreviate(cwd))"]
        return parts.compactMap { $0 }.joined(separator: ", ")
    }
}
