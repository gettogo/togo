// The whole file is compiled out of Release so no debug symbol ships;
// make ios-release-check verifies that with nm.
#if DEBUG
import SwiftUI

enum DebugEntry {
    @MainActor static func root() -> AnyView {
        AnyView(DebugRootView())
    }
}
#endif
