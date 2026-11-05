import SwiftUI

// Role calls the motion check must accept.
struct RoleCalls: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var on = false

    var body: some View {
        let a = Motion.animation(.dropIn, reduce: reduceMotion)
        let _ = a
        Circle()
            .scaleEffect(on ? 1.2 : 1)
            .animation(Motion.animation(.confirm, reduce: reduceMotion), value: on)
            .motion(.fade, value: on)
            .onTapGesture {
                var t = Transaction()
                t.animation = Motion.animation(.chrome, reduce: reduceMotion)
                withTransaction(t) { on.toggle() }
                withAnimation(Motion.animation(.tabSettle, reduce: reduceMotion)) { on.toggle() }
            }
    }
}
