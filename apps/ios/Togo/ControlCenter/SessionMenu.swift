import SwiftUI
import TogoKit

/// A session's context menu, the same for a list row and a board card. `openers` go right after
/// Open, for the board's Open Beside and Open in New Window.
struct SessionMenu<Openers: View>: View {
    let row: SessionRow
    let actions: SessionActionSet
    var etAvailable = false
    let perform: (SessionCommand) -> Void
    @ViewBuilder let openers: () -> Openers

    var body: some View {
        ForEach(actions.available, id: \.self) { action in
            switch action {
            case .open:
                menuButton(action) { perform(.open(row, nil)) }
                    .disabled(actions.disabled.contains(action))
                openers()
            case .connectWith:
                ConnectWithMenu(etAvailable: etAvailable) { perform(.open(row, $0)) }
            case .kill:
                reviewItems
                Divider()
                menuButton(action, role: .destructive) { perform(.kill(row)) }
                    .disabled(actions.disabled.contains(action))
            case .rename, .mute, .copyPath:
                menuButton(action) { perform(command(action)) }
                    .disabled(actions.disabled.contains(action))
            case .changes, .files, .ports:
                EmptyView()
            }
        }
        if !actions.available.contains(.kill) {
            reviewItems
        }
        if let note = actions.note {
            Divider()
            Text(note)
        }
    }

    /// Changes, Files and Ports, after the row's own actions and before Kill when there is one.
    @ViewBuilder private var reviewItems: some View {
        if !actions.review.isEmpty {
            Divider()
            ForEach(actions.review, id: \.self) { action in
                menuButton(action) { perform(command(action)) }
                    .disabled(actions.disabled.contains(action))
            }
            if let note = actions.reviewNote {
                Text(note)
            }
        }
    }

    private func menuButton(_ action: SessionAction, role: ButtonRole? = nil, run: @escaping () -> Void) -> some View {
        Button(action.title(for: row), systemImage: action.systemImage(for: row), role: role, action: run)
    }

    private func command(_ action: SessionAction) -> SessionCommand {
        switch action {
        case .open, .connectWith: .open(row, nil)
        case .rename: .rename(row)
        case .mute: .toggleMute(row)
        case .copyPath: .copyPath(row)
        case .kill: .kill(row)
        case .changes: .review(row, .changes)
        case .files: .review(row, .files)
        case .ports: .review(row, .ports)
        }
    }
}

extension SessionMenu where Openers == EmptyView {
    init(row: SessionRow, actions: SessionActionSet, etAvailable: Bool = false, perform: @escaping (SessionCommand) -> Void) {
        self.init(row: row, actions: actions, etAvailable: etAvailable, perform: perform) { EmptyView() }
    }
}
