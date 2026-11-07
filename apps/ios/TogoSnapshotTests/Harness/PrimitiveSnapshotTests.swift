import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// Each native primitive with sample data, in a navigation stack as a sheet shows it, on an
/// iPhone and a folded Duo; the Picker also in a regular window, where groups move to a sidebar.
@MainActor
struct PrimitiveSnapshotTests {
    private static let phoneAndFold: [ScreenSize] = [.iPhone17, .duoFolded]

    private func screen(_ screen: HarnessScreen) -> some View {
        NavigationStack {
            PrimitiveScreenView(screen: screen, context: .inert)
                .navigationBarTitleDisplayMode(.inline)
        }
    }

    @Test func picker() {
        assertScreen(screen(HarnessSamples.model), named: "primitive-picker", sizes: Self.phoneAndFold)
    }

    @Test func pickerWithRolesAndFilters() {
        assertScreen(screen(HarnessSamples.models), named: "primitive-picker-roles", sizes: Self.phoneAndFold + [.iPadPro13])
    }

    @Test func settings() {
        assertScreen(screen(HarnessSamples.settings), named: "primitive-settings", sizes: Self.phoneAndFold)
    }

    @Test func checklist() {
        assertScreen(screen(HarnessSamples.plan), named: "primitive-checklist", sizes: Self.phoneAndFold)
    }

    @Test func liveList() {
        assertScreen(screen(HarnessSamples.jobs), named: "primitive-live-list", sizes: Self.phoneAndFold)
    }

    @Test func detailDiff() {
        assertScreen(screen(HarnessSamples.patch), named: "primitive-detail-diff", sizes: Self.phoneAndFold)
    }

    @Test func detailMarkdown() {
        assertScreen(screen(HarnessSamples.notes), named: "primitive-detail-markdown")
    }

    @Test func confirmQuestions() {
        assertScreen(screen(HarnessSamples.question), named: "primitive-confirm", sizes: Self.phoneAndFold)
    }

    @Test func confirmNeedsYou() {
        assertScreen(named: "primitive-confirm-needs-you") {
            NavigationStack {
                ConfirmPrimitive(confirm: HarnessSamples.compact.confirm!, context: .inert, needsYou: true)
                    .navigationTitle("Compact")
                    .navigationBarTitleDisplayMode(.inline)
            }
        }
    }

    /// Reset times relative to now, so the words stay the same from run to run.
    @Test func status() {
        var usage = HarnessSamples.usage
        usage.status?.meters = [
            .init(id: "session", label: "Current session", percent: 41, resetsAt: WireDate(Date().addingTimeInterval(7_800))),
            .init(id: "week", label: "This week", percent: 86, resetsAt: WireDate(Date().addingTimeInterval(302_400))),
        ]
        assertScreen(screen(usage), named: "primitive-status", sizes: Self.phoneAndFold)
    }

    @Test func unknownPrimitive() {
        let unknown = HarnessScreen(id: "x", primitive: "timeline", title: "Timeline", fetchedAt: HarnessSamples.at)
        assertScreen(screen(unknown), named: "primitive-unknown")
    }

    @Test func composer() {
        let services = PreviewHarnessServices()
        let model = CommandLineModel(
            store: .preview(services), memory: CommandMemory(defaults: UserDefaults(suiteName: "composer-snapshots")!),
            routes: .preview(services, working: true, queued: [HarnessSamples.queuedItem])
        )
        model.draft = "also check the parser"
        assertScreen(named: "composer-working", sizes: Self.phoneAndFold) {
            VStack {
                Spacer()
                SmartCommandLine(model: model).padding(12)
            }
        }
    }
}
