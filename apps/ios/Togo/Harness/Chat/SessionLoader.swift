import SwiftUI

/// What a session shows while the app learns whether it gets its chat or its terminal: the
/// launch's paws, still walking, so a fresh open reads as one continuous arrival rather than
/// a blank pane (S4). Under Reduce Motion the paws stand still and the words carry it.
/// VoiceOver hears where it is going once, on arrival.
struct SessionLoader: View {
    /// "OMP"; empty when the session's agent is not known yet.
    let agent: String

    var body: some View {
        PawWalk(ChatCopy.opening(agent))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            .onAppear {
                AccessibilityNotification.Announcement(ChatCopy.opening(agent)).post()
            }
    }
}

#if DEBUG
#Preview("Session loader") {
    SessionLoader(agent: "OMP")
}
#endif
