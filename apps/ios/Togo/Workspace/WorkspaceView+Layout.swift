import SwiftUI

#if DEBUG
#Preview("Three tabs") {
    let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
    store.open(hostID: Samples.studio, sessionID: "s-01")
    store.open(hostID: Samples.studio, sessionID: "s-scratch")
    store.open(hostID: Samples.studio, sessionID: "s-04")
    store.select(TabID(hostID: Samples.studio, sessionID: "s-01"))
    let terminals = TerminalPool()
    terminals.sync(with: store)
    return WorkspaceView(store: store, terminals: terminals)
        .tint(Color.togoSnow)
}
#endif

/// In book posture the header and tab strip keep to the page before the fold, so no button or
/// chip sits on it. Read from the fold itself rather than the panes' report, so the chrome is
/// laid out once and does not move when the report arrives.
struct LeadingPageOnly: ViewModifier {
    @Environment(\.fold) private var fold

    func body(content: Content) -> some View {
        if fold.posture == .book, let division = fold.division, division.minX > 0 {
            content
                .frame(width: division.minX)
                .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            content
        }
    }
}

/// Hardware shortcuts while the terminal does not hold keyboard focus.
struct WorkspaceShortcuts: View {
    let store: AppStore
    let layout: WorkspaceBarLayout
    let chips: [TabStrip.Chip]

    var body: some View {
        ZStack {
            ForEach(Array(chips.prefix(9).enumerated()), id: \.element.id) { index, chip in
                Button(WorkspaceCopy.goTo([chip.bib, chip.name].compactMap(\.self).joined(separator: " "))) {
                    // A cut, like a chip tap (`show`).
                    store.selectTab(number: index + 1)
                }
                .keyboardShortcut(KeyEquivalent(Character(String(index + 1))), modifiers: .command)
            }
            if !layout.sessionItemsInBar, let mission = store.missionStore {
                Button(MissionCopy.title) { mission.showsMission.toggle() }
                    .keyboardShortcut("i", modifiers: .command)
            }
        }
        .opacity(0)
        .accessibilityHidden(true)
    }
}
