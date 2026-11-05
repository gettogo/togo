import Foundation
import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct TutorialSnapshotTests {
    private func model(on page: TutorialPage) -> TutorialModel {
        let model = TutorialModel(defaults: UserDefaults(suiteName: "tutorial-snapshots-\(UUID().uuidString)")!)
        model.go(to: page)
        return model
    }

    private func flow(_ model: TutorialModel) -> some View {
        TutorialFlow(model: model, animated: false, now: Samples.now) { _ in }
    }

    @Test func welcome() {
        let model = model(on: .welcome)
        assertScreen(named: "tutorial-welcome") { flow(model) }
    }

    @Test func checklistMixed() {
        let model = model(on: .setup)
        model.record(SetupDetection(keyAccepted: true, report: nil))
        model.toggle(.mosh)
        assertScreen(named: "tutorial-checklist") { flow(model) }
    }

    @Test func checklistStepsExpanded() {
        let model = model(on: .setup)
        model.toggle(.remoteLogin)
        assertScreen(named: "tutorial-checklist-helper-steps") {
            NavigationStack {
                SetupPage(model: model, expanded: .helper)
            }
        }
    }

    /// The checklist on its own, so the AX5 references show the rows and an open item.
    @Test func checklistRows() {
        assertScreen(named: "tutorial-checklist-rows") {
            ScrollView {
                SetupChecklist(ticked: [.remoteLogin, .mosh], detected: [.remoteLogin], expanded: .remoteLogin) { _ in }
                    .screenMargins()
            }
        }
    }

    /// At accessibility sizes the two columns stack.
    @Test func helperComparison() {
        let model = model(on: .helper)
        assertScreen(named: "tutorial-helper") { flow(model) }
    }

    /// The comparison on its own, so the AX5 references show the columns stacked.
    @Test func helperComparisonColumns() {
        assertScreen(named: "tutorial-comparison") {
            ScrollView {
                HelperComparison()
                    .screenMargins()
            }
        }
    }

    @Test func privacy() {
        let model = model(on: .privacy)
        assertScreen(named: "tutorial-privacy") { flow(model) }
    }

    @Test func session() {
        let model = model(on: .session)
        assertScreen(named: "tutorial-session") { flow(model) }
    }

    @Test func ready() {
        let model = model(on: .ready)
        assertScreen(named: "tutorial-ready") { flow(model) }
    }

    @Test func demoMachine() {
        assertScreen(named: "tutorial-demo") {
            DemoControlCenter(store: DemoMachine.store(now: Samples.now), now: Samples.now) {}
        }
    }

    @Test func firstRunWithGuide() {
        let model = model(on: .welcome)
        model.skip()
        assertScreen(named: "first-run-with-guide") { FirstRunView(tutorial: model) {} }
    }
}
