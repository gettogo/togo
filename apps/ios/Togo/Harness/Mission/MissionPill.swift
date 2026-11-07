import SwiftUI
import TogoKit

/// The live activity pill at the top of the chat: "2 agents · 1 shell · plan 3/7", beacon blue
/// while something needs the user. Tap or drag down opens mission control.
struct MissionPill: View {
    let summary: MissionSummary
    let onOpen: () -> Void

    var body: some View {
        EmptyView()
    }
}
