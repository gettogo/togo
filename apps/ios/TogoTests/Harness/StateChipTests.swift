import Foundation
import Testing
import TogoKit
@testable import Togo

/// The state chip shows what the harness reports: model · effort · mode, segments it doesn't
/// report left out, a placeholder only while the session is described, and never the word "Mode".
@MainActor struct StateChipTests {
    private let screens: [HarnessDescriptor.ScreenRef] = [
        .init(id: "model", primitive: .picker, title: "Model", section: "composer"),
        .init(id: "effort", primitive: .settings, title: "Effort", section: "composer"),
        .init(id: "resume", primitive: .picker, title: "Resume", section: "more"),
    ]
    private let modes: [HarnessChoice] = [
        .init(id: "default", label: "Manual"), .init(id: "acceptEdits", label: "Accept edits"), .init(id: "plan", label: "Plan"),
    ]
    private var commands: [HarnessCompletion] {
        [
            .init(insert: "/model ", title: "/model", screen: "model", kind: .choice, action: "set_model",
                  choices: [.init(id: "opus", label: "Opus", selected: true), .init(id: "sonnet", label: "Sonnet")]),
            .init(insert: "/effort ", title: "/effort", screen: "effort", kind: .choice, action: "set_effort",
                  choices: [.init(id: "low", label: "low"), .init(id: "high", label: "high", selected: true)]),
            .init(insert: "/compact ", title: "/compact", kind: .run, action: "compact"),
        ]
    }

    private func status(model: String? = nil, effort: String? = nil) -> HarnessStatus {
        var status = HarnessStatus()
        status.model = model
        status.effort = effort
        return status
    }

    @Test func rowsComeFromTheComposerScreensThenTheMode() {
        let rows = StateChip.rows(screens: screens, commands: commands, modes: modes, hasMode: true)
        #expect(rows.map(\.title) == ["Model", "Effort", PrimitiveCopy.mode])
        guard case .choices(let choices, let action) = rows[1].control else {
            Issue.record("effort is a choice")
            return
        }
        #expect(action == "set_effort")
        #expect(choices.map(\.id) == ["low", "high"])
        #expect(StateChip.folded(rows) == ["/model", "/effort"])
    }

    @Test func aScreenWithoutChoicesIsReachableAsAList() {
        let rows = StateChip.rows(screens: screens, commands: [], modes: [], hasMode: false)
        #expect(rows.map(\.control) == [.screen("model"), .screen("effort")])
    }

    @Test func segmentsComeFromTheStatusBlock() {
        let rows = StateChip.rows(screens: screens, commands: commands, modes: modes, hasMode: true)
        let title = StateChip.title(rows: rows, status: status(model: "sonnet", effort: "low"), modeLabel: "Plan", describing: false)
        #expect(title == "Sonnet · low · Plan")
    }

    @Test func withoutAStatusTheHelpersMarkedChoiceShows() {
        let rows = StateChip.rows(screens: screens, commands: commands, modes: modes, hasMode: true)
        #expect(StateChip.title(rows: rows, status: nil, modeLabel: "Manual", describing: false) == "Opus · high · Manual")
    }

    @Test func segmentsTheHarnessDoesNotReportAreLeftOut() {
        let rows = StateChip.rows(screens: screens, commands: [], modes: modes, hasMode: true)
        // No choices, no status: only the mode.
        #expect(StateChip.title(rows: rows, status: nil, modeLabel: "Manual", describing: false) == "Manual")
        // A model the status reports even without its choices.
        #expect(StateChip.title(rows: rows, status: status(model: "Opus 4.5"), modeLabel: nil, describing: false) == "Opus 4.5")
        #expect(StateChip.title(rows: rows, status: nil, modeLabel: nil, describing: false) == nil)
    }

    @Test func aPlaceholderOnlyWhileDescribing() {
        let rows = StateChip.rows(screens: screens, commands: commands, modes: modes, hasMode: true)
        #expect(StateChip.title(rows: rows, status: nil, modeLabel: "Manual", describing: true) == nil)
    }

    @Test func neverTheWordMode() {
        let rows = StateChip.rows(screens: [], commands: [], modes: modes, hasMode: true)
        for describing in [false, true] {
            let title = StateChip.title(rows: rows, status: nil, modeLabel: ModeMemory.label(for: "", in: modes), describing: describing)
            #expect(title != PrimitiveCopy.mode)
            #expect(title != "Mode")
        }
    }

    @Test func modesTheChipCannotPickStillShowTheirLabels() {
        let all = modes + [.init(id: "bypassPermissions", label: "Bypass permissions")]
        #expect(ModeMemory.label(for: "bypassPermissions", in: all) == "Bypass permissions")
        // An id the harness never listed shows as itself, not as "Mode".
        #expect(ModeMemory.label(for: "dontAsk", in: modes) == "dontAsk")
    }

    @Test func aCommandWhoseChoicesAreTheModesIsTheModeRow() {
        let chatMode = HarnessCompletion(insert: "/chat-mode ", title: "/chat-mode", kind: .choice, action: "set_mode",
                                         choices: [.init(id: "default", label: "Manual"), .init(id: "plan", label: "Plan")])
        let rows = StateChip.rows(screens: [], commands: [chatMode], modes: modes, hasMode: true)
        #expect(rows.count == 1)
        #expect(rows.first?.command == chatMode)
        #expect(StateChip.folded(rows) == ["/chat-mode"])
    }
}
