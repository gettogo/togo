import SwiftUI
import TogoKit

/// Supervision uses the helper's running/waiting count, not the number of retained rows.
/// Unknown states remain visible without claiming that they are running or finished.
enum MissionActivity {
    static func isRunning(_ state: HarnessState) -> Bool {
        state == .running || state == .waiting
    }

    static func label(_ state: HarnessState) -> String {
        switch state.rawValue {
        case "ok", "done", "completed", "finished", "succeeded":
            String(localized: "mission.state.finished", defaultValue: "Finished")
        case "exited": String(localized: "mission.state.exited", defaultValue: "Exited")
        case "ready": String(localized: "mission.state.ready", defaultValue: "Ready")
        case "active": String(localized: "mission.state.active", defaultValue: "Active")
        case "failed", "error": PrimitiveStateLook(.failed).label
        case "cancelled", "killed", "aborted": String(localized: "mission.state.stopped", defaultValue: "Stopped")
        case "timeout": String(localized: "mission.state.timeout", defaultValue: "Timed out")
        case "running", "waiting", "denied", "interrupted", "idle", "pending", "skipped":
            PrimitiveStateLook(state).label
        default: sourceLabel(state.rawValue) ?? String(localized: "mission.state.unknown", defaultValue: "Unknown state")
        }
    }

    /// Preserve a short state word supplied by a newer harness without guessing its meaning.
    private static func sourceLabel(_ value: String) -> String? {
        guard !value.isEmpty, value.count <= 32,
              value.allSatisfy({ $0.isLetter || $0 == " " || $0 == "-" || $0 == "_" }) else { return nil }
        let words = value.replacingOccurrences(of: "_", with: " ").replacingOccurrences(of: "-", with: " ")
            .split(separator: " ").map(String.init)
        guard !words.isEmpty else { return nil }
        let plain = words.joined(separator: " ").lowercased()
        return plain.prefix(1).uppercased() + plain.dropFirst()
    }

    static func clockStart(_ row: HarnessRow) -> Date? {
        isRunning(row.state) ? row.startedAt?.date : nil
    }

    static func countLabel(_ count: Int) -> String {
        String(localized: "mission.count.running", defaultValue: "\(count) running")
    }

    static func isFailure(_ state: HarnessState) -> Bool {
        ["failed", "error", "timeout"].contains(state.rawValue)
    }
}

/// A separate line keeps the state readable even with a long title or at large text sizes.
struct MissionActivityLine: View {
    @Environment(\.dynamicTypeSize) private var typeSize
    let row: HarnessRow

    var body: some View {
        let layout = typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 3))
            : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 8))
        layout {
            Text(verbatim: MissionActivity.label(row.state))
                .font(.caption)
                .foregroundStyle(MissionActivity.isFailure(row.state) ? PrimitiveTint.fail : Color.togoMute)
                .fixedSize(horizontal: false, vertical: true)
            if let start = MissionActivity.clockStart(row) {
                MissionClock(start: start)
            }
        }
    }
}
