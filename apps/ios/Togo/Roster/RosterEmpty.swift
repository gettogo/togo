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
    /// The roster column is hidden, so this screen also offers to show it.
    var rosterHidden = false
    var showRoster: () -> Void = {}

    var body: some View {
        VStack(spacing: 12) {
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
            if rosterHidden {
                Button(RosterCopy.showRoster, action: showRoster)
                    .buttonStyle(.secondary(.fit))
                    .padding(.top, 8)
            }
        }
        .frame(maxWidth: dynamicTypeSize.isAccessibilitySize ? .infinity : 340)
        .padding(.horizontal, Layout.margin)
        .centredInDetail()
        .background(Color.togoInk)
    }
}

/// The filter the roster is narrowed by, at the top of its lines, with the way back to every
/// session. At accessibility sizes the button goes under the words.
struct RosterFilterRow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let filter: SessionFilter
    let showAll: () -> Void

    var body: some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4))
            : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 12))
        layout {
            Label(RosterCopy.filtered(ControlCenterCopy.filterName(filter)), systemImage: "line.3.horizontal.decrease")
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(RosterCopy.showAll, action: showAll)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .buttonStyle(.borderless)
                .frame(minHeight: 44)
                .accessibilityIdentifier("roster.filter.showAll")
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("roster.filter")
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
