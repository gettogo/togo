import SwiftUI

/// Settings where it owns its navigation: the sheet beside the roster, or a tab or sidebar item
/// if the root makes it one. Where the width is regular (iPad, an unfolded iPhone Duo) the panes
/// sit in a sidebar beside the selected one; where it is compact the split view collapses to the
/// list with the pane pushed. It is one tree for every layout, so a fold, an unfold or a window
/// resize keeps the pane: the selection lives in scene storage and picks the column on top when
/// the split view collapses (N11, N13, P11).
///
/// Nothing here writes state when the size class changes. The sheet's own size class flips when
/// a Duo folds part way (the system moves the sheet onto one page) and back when it opens; the
/// split view collapses and expands by itself then. Writing the pane or the top column in that
/// same update (as this view once did, defaulting the pane on regular) re-identifies the detail
/// stack and pushes into a split view that is mid-expand inside a sheet mid-resize: the sheet
/// stayed at its folded size over a dimmed window that took no touches. Only the user picks a
/// pane; the wide detail shows the first pane until they do, as a view, not as a selection.
struct SettingsSplitView: View {
    @SceneStorage("settings.pane") private var pane: SettingsPane?

    let values: SettingsValues
    let actions: SettingsActions
    /// Closes Settings; nil where it is not modal (a tab).
    var close: (() -> Void)?

    var body: some View {
        let detail = Self.detail(for: pane)
        NavigationSplitView(preferredCompactColumn: compactColumn) {
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
                SettingsView(values: values, actions: actions).pane(detail)
            }
            // Another pane starts its own stack; what the last one pushed does not carry over.
            .id(detail)
        }
    }

    /// The column on top while collapsed, read from the pane, so a collapse or an expand never
    /// needs a write to agree with it. Back (or a swipe) from the pane clears the pick.
    private var compactColumn: Binding<NavigationSplitViewColumn> {
        Binding {
            Self.column(for: pane)
        } set: { column in
            let next = Self.pick(after: column, pane: pane)
            if next != pane { pane = next }
        }
    }

    /// Side by side, the detail is never blank: it shows the first pane until one is picked.
    /// Collapsed it is not on screen until the user picks one, so the list comes first.
    static func detail(for pane: SettingsPane?) -> SettingsPane {
        pane ?? .machines
    }

    /// The column on top once the split view collapses (a fold, a narrower window): the pane the
    /// user is in, or the list when there is none.
    static func column(for pane: SettingsPane?) -> NavigationSplitViewColumn {
        pane == nil ? .sidebar : .detail
    }

    /// The pick after the collapsed split view shows `column`: back on the list, nothing is
    /// picked; anything else leaves the pick as it is.
    static func pick(after column: NavigationSplitViewColumn, pane: SettingsPane?) -> SettingsPane? {
        column == .sidebar ? nil : pane
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
