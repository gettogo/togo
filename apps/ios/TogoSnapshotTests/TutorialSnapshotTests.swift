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
        TutorialFlow(model: model, now: Samples.now) { _ in }
    }

    @Test func welcome() {
        let model = model(on: .welcome)
        assertScreen(named: "tutorial-welcome") { flow(model) }
    }

    /// The last page, with Add your first machine and the demo in the bar.
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

    @Test func approvals() {
        let model = model(on: .approvals)
        assertScreen(named: "tutorial-approvals") { flow(model) }
    }

    /// Without Live Activities, as on an iPad: the request as an opened notification.
    @Test func approvalsWithoutLiveActivities() {
        let model = model(on: .approvals)
        assertScreen(named: "tutorial-approvals-notification", sizes: [.iPhone17, .iPadPro13]) {
            flow(model).environment(\.liveActivitiesAvailable, false)
        }
    }

    @Test func sessions() {
        let model = model(on: .sessions)
        assertScreen(named: "tutorial-sessions") { flow(model) }
    }

    /// Learn more open: the detail each page folds away.
    @Test func welcomeLearnMore() {
        assertScreen(named: "tutorial-welcome-more") {
            NavigationStack { WelcomePage(expanded: true) }
        }
    }

    @Test func approvalsLearnMore() {
        assertScreen(named: "tutorial-approvals-more") {
            NavigationStack { ApprovalsPage(now: Samples.now, expanded: true) }
        }
    }

    @Test func helperLimits() {
        assertScreen(named: "tutorial-helper-limits") {
            ScrollView {
                HelperLimits()
                    .screenMargins()
            }
        }
    }

    /// The guide on a full-screen iPad and beside another app: the column keeps to a readable width.
    @Test func welcomeOnIPad() {
        let model = model(on: .welcome)
        assertScreen(named: "tutorial-welcome-ipad", sizes: [.iPadPro13, .iPadHalf]) { flow(model) }
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
