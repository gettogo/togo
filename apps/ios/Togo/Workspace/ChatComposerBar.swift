import SwiftUI
import TogoKit

/// Both rows belong to the bottom inset. The chat ends above the agents line; the
/// composer's own glass contains only input controls and its queued-prompt count.
struct ChatComposerBar: View {
    let model: CommandLineModel
    var agentsReconciled = false

    private var agentCount: Int {
        guard model.store.phase == .live, model.store.has(HarnessCapability.agents) else { return 0 }
        return MissionLayout.runningAgents(model.store.mission.mission, working: model.store.working, reconciled: agentsReconciled)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if agentCount > 0 {
                AgentsRunningLine(store: model.store, count: agentCount)
                    .padding(.horizontal, 16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color.togoInk)
            }
            SmartCommandLine(model: model)
        }
        // motion: preventing a jump — count changes resize the inset at once, never animate the chat.
        .animation(nil, value: agentCount)
    }
}
