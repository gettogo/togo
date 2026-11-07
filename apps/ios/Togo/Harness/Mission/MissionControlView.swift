import SwiftUI
import TogoKit

/// Mission control: the session's sections in their fixed order (needs you, agents,
/// background, plan, changes, ports, status), each shown only when the harness supports it.
/// A sheet with medium and large detents on compact widths; a side inspector on iPad and on
/// an unfolded Duo.
struct MissionControlView: View {
    let store: HarnessSessionStore
    let context: PrimitiveContext

    var body: some View {
        EmptyView()
    }
}
