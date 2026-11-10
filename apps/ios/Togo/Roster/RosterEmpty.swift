import SwiftUI

/// The roster with no machine to list: the unlit mark, what is missing, and the one way on.
struct RosterNoMachines: View {
    let addMachine: () -> Void

    var body: some View {
        VStack(spacing: 12) {
            UnlitMark()
            Text(RosterCopy.noMachinesTitle)
                .font(.title3.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .multilineTextAlignment(.center)
            Button(RosterCopy.noMachinesAction, action: addMachine)
                .buttonStyle(.primary(.fit))
                .padding(.top, 8)
        }
        .padding(.horizontal, Layout.margin)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.togoInk)
    }
}

/// The terminal column with no terminal open: the unlit mark, whether anything waits, and how
/// to open one. The bar at its foot starts work or answers what waits.
struct RosterEmptyDetail: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    /// A session needs the user, so the headline says so instead of "All clear".
    var waiting = false

    /// With the roster hidden, the split view's own toggle in the bar is the way back to it.
    var body: some View {
        VStack(spacing: 12) {
            UnlitMark()
            Text(waiting ? RosterCopy.detailWaiting : RosterCopy.detailClear)
                .font(.title3.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .multilineTextAlignment(.center)
            Text(RosterCopy.detailMessage)
                .font(.body)
                .foregroundStyle(Color.togoMute)
                .multilineTextAlignment(.center)
        }
        .accessibilityElement(children: .combine)
        .frame(maxWidth: dynamicTypeSize.isAccessibilitySize ? .infinity : 340)
        .padding(.horizontal, Layout.margin)
        .centredInDetail()
        .background(Color.togoInk)
    }
}

#if DEBUG
#Preview("No machines") {
    RosterNoMachines {}
}

#Preview("Empty detail") {
    RosterEmptyDetail(waiting: true)
}
#endif
