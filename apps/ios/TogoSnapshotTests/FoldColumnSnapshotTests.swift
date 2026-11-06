import Foundation
import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// Screens with a readable column on an unfolded Duo: held as a book the column keeps to one
/// page; held flat it stays centred.
@MainActor
struct FoldColumnSnapshotTests {
    private func model(on page: TutorialPage) -> TutorialModel {
        let model = TutorialModel(defaults: UserDefaults(suiteName: "fold-column-\(UUID().uuidString)")!)
        model.go(to: page)
        return model
    }

    @Test func firstRunKeepsToTheLeadingPage() {
        let model = model(on: .welcome)
        model.skip()
        assertScreen(named: "first-run", sizes: [.duoBook, .duoOpen]) { FirstRunView(tutorial: model) {} }
    }

    @Test func tutorialKeepsToTheLeadingPage() {
        let model = model(on: .welcome)
        assertScreen(named: "tutorial-welcome", sizes: [.duoBook]) {
            TutorialFlow(model: model, animated: false, now: Samples.now) { _ in }
        }
    }
}
