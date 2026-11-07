import SnapshotTesting
import SwiftUI
import Testing
@testable import Togo

@MainActor
struct ActivityBarsSnapshotTests {
    private static let busy: [Double] = [0, 0, 0.1, 0.4, 0.9, 1, 0.3, 0, 0, 0.2, 0.6, 1, 0.7, 0.1, 0.5, 0.8]

    @Test func liveIdleAndEmpty() {
        let gallery = VStack(alignment: .leading, spacing: 20) {
            ActivityBars(samples: Self.busy, live: true)
            ActivityBars(samples: Self.busy, live: false)
            ActivityBars(samples: Array(Self.busy.suffix(6)), live: true)
            ActivityBars(samples: [], live: true)
            Text("No bars above this line").font(.footnote).foregroundStyle(Color.togoMute)
        }
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        assertScreen(gallery, named: "activity-bars")
    }
}
