import SwiftUI

// An inline curve the motion check must reject.
struct Inline: View {
    @State private var on = false

    var body: some View {
        Circle()
            .scaleEffect(on ? 1.2 : 1)
            .animation(.spring(response: 0.3), value: on)
    }
}
