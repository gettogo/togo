#if DEBUG
import SwiftUI
import TogoKit

/// The longest realistic strings each agent harness sends, for previews and the truncation
/// tests: permission options, model ids, agent and task names, commands, paths and counts.
enum StressFixtures {
    /// Extra answers to a permission prompt, one or more per adapter, as the agents word them.
    static let options: [AnswerOption] = [
        // Claude Code
        AnswerOption(id: 2, label: "Yes, and switch to auto mode · auto mode handles these prompts for you"),
        AnswerOption(id: 3, label: "Yes, and don't ask again for git commands in /Users/jamie/Dev/togo/.worktrees/integ-native"),
        // Codex
        AnswerOption(id: 4, label: "Yes, and don't ask again for this command in this session"),
        // oh-my-pi
        AnswerOption(id: 5, label: "Always allow for this project · saves the rule to .omp/permissions.json"),
        // OpenCode
        AnswerOption(id: 6, label: "Allow always · applies to webfetch on docs.anthropic.com for this session"),
        // Gemini CLI
        AnswerOption(id: 7, label: "Allow always for every tool from server google-workspace-mcp"),
        // Copilot CLI
        AnswerOption(id: 9, label: "Yes, and approve 'gh pr create' for the rest of the running session"),
    ]

    static let approveLabel = "Yes, and allow edits to these files for the rest of the session"
    static let question = "Allow Bash to run `git push --force-with-lease origin native/trunc` in /Users/jamie/Dev/togo?"

    static let modelIDs = [
        "claude-opus-5-5[1m]", "gpt-5.5-codex-max-high", "anthropic/claude-opus-5-5-20261101",
        "gemini-3.5-pro-preview-11-2026", "qwen3.8-coder-480b-a35b-instruct",
        "claude-opus-5-5 (Preview, 1x premium requests)",
    ]

    static let modes = ["Bypass permissions", "Accept edits", "Plan mode", "Full access", "Read only, ask for everything"]

    static let agentName = "Investigate why the relay drops encrypted pushes after a key rotation on the Studio Mac"
    static let taskTitle = "Rewrite the folder browser so deep paths keep both their root and their last folder visible"

    static let command = #"for i in $(seq 1 12); do echo "tick $i/12 at $(date +%H:%M:%S)"; sleep 5; done"#
    static let buildCommand = "cd apps/ios && DEVELOPER_DIR=/Applications/Xcode-beta.app/Contents/Developer xcodebuild -project Togo.xcodeproj -scheme Togo -destination 'platform=iOS Simulator,name=Togo iPhone 17' test"
    static let lastOutput = "Test Suite 'All tests' passed at 2026-11-07 08:28:11.452. Executed 1284 tests, with 0 failures (0 unexpected) in 312.118 seconds"

    static let path = "/Users/jamie/Dev/togo/.worktrees/native-trunc/apps/ios/Togo/Harness/Primitives/PickerParts.swift"

    static let count = 12_345

    /// A background shell whose title is the command itself, as an adapter without a
    /// description sends it.
    static func background(startedAgo seconds: TimeInterval = 20) -> HarnessRow {
        HarnessRow(
            id: "bg-stress", title: command, subtitle: command, state: .running,
            startedAt: WireDate(Date.now.addingTimeInterval(-seconds)),
            tail: ["tick 3/12 at 08:28:01", "tick 4/12 at 08:28:06", lastOutput]
        )
    }

    static let agentRow = HarnessRow(
        id: "agent-stress", title: agentName, subtitle: "general-purpose", state: .running,
        startedAt: WireDate(Date.now.addingTimeInterval(-45))
    )

    static let waitingSummary = MissionSummary(
        needsYou: 3, agents: 12, background: 7, plan: .init(done: 128, total: 345), working: true
    )
}

#Preview("Stress: more answers, AX5, Duo folded", traits: .fixedLayout(width: 360, height: 1400)) {
    ScrollView {
        AnswerOptionList(options: StressFixtures.options) { _ in }
            .padding(16)
    }
    .background(Color.togoInk)
    .environment(\.dynamicTypeSize, .accessibility5)
}

#Preview("Stress: mission rows, AX5", traits: .fixedLayout(width: 360, height: 1200)) {
    ScrollView {
        MissionCard {
            MissionRowView(row: StressFixtures.agentRow, opens: true)
            MissionBackgroundRow(row: StressFixtures.background())
        }
        .padding(16)
    }
    .background(Color.togoInk)
    .environment(\.dynamicTypeSize, .accessibility5)
}

#Preview("Stress: approve and pill, AX5", traits: .fixedLayout(width: 360, height: 900)) {
    VStack(alignment: .leading, spacing: 16) {
        MissionPill(summary: StressFixtures.waitingSummary) {}
        ApproveButton(StressFixtures.approveLabel) {}
        PathText(StressFixtures.path)
        ForEach(StressFixtures.modelIDs, id: \.self) { PrimitiveChip(text: $0, mono: true, tone: .strong) }
    }
    .padding(16)
    .background(Color.togoInk)
    .environment(\.dynamicTypeSize, .accessibility5)
}
#endif
