import SwiftUI

struct RootView: View {
    var body: some View {
        #if DEBUG
        DebugEntry.root()
        #else
        Text("Togo")
            .foregroundStyle(Color.togoSnow)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
        #endif
    }
}
