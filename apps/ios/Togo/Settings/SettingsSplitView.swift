import SwiftUI

/// Settings where it owns its navigation: the sheet beside the roster, or a tab or sidebar item
/// if the root makes it one. Where the width is regular (iPad, an unfolded iPhone Duo) the panes
/// sit in a sidebar beside the selected one; where it is compact the split view collapses to the
/// list with the pane pushed. It is one tree for every layout, so a fold, an unfold or a window
/// resize keeps the pane: the selection lives in scene storage and picks the column on top when
/// the split view collapses (N11, N13, P11).
struct SettingsSplitView: View {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @SceneStorage("settings.pane") private var pane: SettingsPane?
    @State private var compactColumn = NavigationSplitViewColumn.sidebar

    let values: SettingsValues
    let actions: SettingsActions
    /// Closes Settings; nil where it is not modal (a tab).
    var close: (() -> Void)?

    var body: some View {
        NavigationSplitView(preferredCompactColumn: $compactColumn) {
            SettingsView(values: values, actions: actions, selection: $pane)
                .toolbar {
                    if let close {
                        ToolbarItem(placement: .cancellationAction) {
                            Button(role: .close, action: close)
                                .accessibilityIdentifier("settings.close")
                        }
                    }
                }
        } detail: {
            NavigationStack {
                if let pane {
                    SettingsView(values: values, actions: actions).pane(pane)
                }
            }
            // Another pane starts its own stack; what the last one pushed does not carry over.
            .id(pane)
        }
        .onChange(of: sizeClass, initial: true) { _, size in
            pane = SettingsSplitView.pane(pane, sizeClass: size)
        }
        .onChange(of: pane, initial: true) { _, pane in
            compactColumn = SettingsSplitView.column(for: pane)
        }
    }

    /// Side by side, the detail is never blank: it shows the first pane until one is picked.
    /// Collapsed, nothing is picked for the user; the list comes first.
    static func pane(_ pane: SettingsPane?, sizeClass: UserInterfaceSizeClass?) -> SettingsPane? {
        guard pane == nil, sizeClass == .regular else { return pane }
        return .machines
    }

    /// The column on top once the split view collapses (a fold, a narrower window): the pane the
    /// user is in, or the list when there is none.
    static func column(for pane: SettingsPane?) -> NavigationSplitViewColumn {
        pane == nil ? .sidebar : .detail
    }
}

/// HIG sheets: a sheet whose action leads to another sheet closes first, and comes back once the
/// other has gone. Editing or adding a machine from the Settings sheet hands the window's one
/// sheet over to the form; closing the form brings Settings back, on the pane it was on.
struct SettingsHandover: Equatable {
    private(set) var returnsToSettings = false

    /// The sheet to show for `next`, asked for while `current` is up.
    mutating func open(_ next: AppSheet, from current: AppSheet?) -> AppSheet {
        returnsToSettings = current == .settings
        return next
    }

    /// What shows once the sheet that was up has closed: Settings again when it handed over.
    mutating func closed() -> AppSheet? {
        defer { returnsToSettings = false }
        return returnsToSettings ? .settings : nil
    }
}
