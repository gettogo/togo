import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor
struct ReviewPortsSnapshotTests {
    private func screen(_ model: PortsModel) -> some View {
        NavigationStack {
            PortsView(model: model)
                .navigationTitle(ReviewCopy.tabName(.ports))
                .navigationBarTitleDisplayMode(.inline)
        }
    }

    private static let node = ReviewFixtures.ports[0]

    @Test func noPorts() {
        assertScreen(named: "empty") { screen(.fixture(ports: [])) }
    }

    @Test func threePorts() {
        assertScreen(named: "three") { screen(.fixture()) }
    }

    @Test func oneOpen() {
        let info = ForwardInfo(remote: Self.node, localPort: 50_000, url: PortsModel.loopbackURL(50_000))
        assertScreen(named: "open") {
            screen(.fixture(states: [3000: .open(info)], notes: [3000: ReviewCopy.nothingListening(3000)]))
        }
    }

    @Test func failed() {
        let lan = ListeningPort(port: 9229, address: "192.168.1.5", pid: 4410, command: "node")
        let info = ForwardInfo(remote: Self.node, localPort: 50_000, url: PortsModel.loopbackURL(50_000))
        assertScreen(named: "failed") {
            screen(.fixture(
                ports: ReviewFixtures.ports + [lan],
                states: [3000: .open(info), 5173: .failed("The machine didn't answer.")],
                notes: [8080: PortsCopy.idleStopped],
                gone: [3000], manualText: "70000", manualError: PortsCopy.manualRange
            ))
        }
    }
}
