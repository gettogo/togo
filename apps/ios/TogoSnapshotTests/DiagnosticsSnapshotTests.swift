import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct DiagnosticsSnapshotTests {
    private static let report = DiagnosticsReport(
        app: AppFacts(version: "1.0", build: "42", osVersion: "27.2", model: "iPhone19,4", locale: "en_US"),
        settings: DiagnosticsReport.summary(of: AppSettings()),
        machines: [
            MachineFacts(token: MachineToken(value: "m-1a2b3c4d"), helperVersion: "0.3.0", capabilities: ["events", "upload"], transport: "auto"),
            MachineFacts(token: MachineToken(value: "m-9f8e7d6c"), helperVersion: "0.2.4", capabilities: ["events"], transport: "ssh"),
        ],
        events: [
            DiagnosticEvent(at: Samples.now, category: .perf, name: "launch", fields: ["ms": .int(412)]),
            DiagnosticEvent(at: Samples.now.addingTimeInterval(2), category: .machine, name: "reachability", fields: [
                "machine": .machine(MachineToken(value: "m-1a2b3c4d")), "state": .word("online"),
            ]),
            DiagnosticEvent(at: Samples.now.addingTimeInterval(5), category: .link, name: "phase", fields: [
                "machine": .machine(MachineToken(value: "m-1a2b3c4d")), "phase": .word("live"), "transport": .word("ssh"), "attempt": .int(1),
            ]),
            DiagnosticEvent(at: Samples.now.addingTimeInterval(90), category: .link, name: "phase", fields: [
                "machine": .machine(MachineToken(value: "m-9f8e7d6c")), "phase": .word("failed"), "error": .word("togokit.sshfailure.4"),
            ]),
        ]
    )

    private var summary: DiagnosticsSummary {
        DiagnosticsSummary(
            events: Self.report.events.count, machines: Self.report.machines.count, text: Self.report.text(),
            file: URL(fileURLWithPath: "/tmp/togo-diagnostics-2026-11-05.json")
        )
    }

    @Test func page() {
        assertScreen(named: "settings-diagnostics") {
            NavigationStack { DiagnosticsPage(summary: summary, clear: {}) }.togoControls()
        }
    }

    @Test func empty() {
        assertScreen(named: "settings-diagnostics-empty") {
            NavigationStack {
                DiagnosticsPage(summary: DiagnosticsSummary(events: 0, machines: 0, text: "", file: nil), clear: {})
            }.togoControls()
        }
    }

    @Test func preview() {
        assertScreen(named: "settings-diagnostics-preview") {
            NavigationStack { DiagnosticsPreview(text: summary.text) }.togoControls()
        }
    }
}
