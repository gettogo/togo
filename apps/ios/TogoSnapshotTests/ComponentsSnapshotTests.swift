import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct ComponentsSnapshotTests {
    @Test func statusWordsAllStates() {
        let statuses: [SessionStatus] = [.working, .needsYou, .done, .idle, .exited, .unknown("waiting_on_ci")]
        let gallery = VStack(alignment: .leading, spacing: 20) {
            SectionHeading("Studio", detail: "Online")
            ForEach(statuses, id: \.rawValue) { status in
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    BibText("04")
                    StatusWord(status: status)
                }
            }
            PathText("/Users/sam/src/a-very-long-project-name/packages/feature-module/sources")
            PawWalk("Connecting")
        }
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        assertScreen(gallery, named: "status-words")
    }

    @Test func rosterStatusAllStates() {
        let gallery = VStack(alignment: .leading, spacing: 16) {
            RosterStatus(status: .needsYou)
            RosterStatus(status: .working, animated: true)
            RosterStatus(status: .working)
            RosterStatus(status: .done)
            RosterStatus(connecting: "Connecting")
            RosterStatus(status: .idle)
            RosterStatus(status: .ended)
            RosterStatus(status: .failed)
        }
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        assertScreen(gallery, named: "roster-status")
    }

    @Test(arguments: StateView.Kind.allCases)
    func stateViewKinds(kind: StateView.Kind) {
        let copy: (title: String, message: String, action: String?) = switch kind {
        case .empty: ("No sessions yet", "Start one with the plus button or from your desk.", "New session")
        case .loading: ("Connecting to Studio", "Reaching the machine over your network.", nil)
        case .error: ("Couldn't reach Studio", "The connection was refused on port 22.", "Try again")
        case .offline: ("Studio is asleep", "Its sessions come back when it wakes up.", nil)
        case .permission: ("Local network is off", "Allow it in Settings to find machines nearby.", "Open Settings")
        }
        let view = StateView(kind: kind, title: copy.title, message: copy.message, action: copy.action.map { .init($0) {} })
        assertScreen(view, named: "state-\(kind)")
    }
}
