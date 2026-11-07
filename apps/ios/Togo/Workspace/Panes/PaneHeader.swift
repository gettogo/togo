import SwiftUI

/// The strip above each of two panes: the session's bib and name, and a menu to swap the
/// panes, close this one, or open its tab in a window of its own. The focused pane's header
/// reads in snow, the other in mute.
struct PaneHeader: View {
    @Environment(\.displayScale) private var displayScale
    let tab: WorkspaceTab
    let pane: Pane
    let store: AppStore

    var body: some View {
        let label = store.chipLabel(for: tab)
        let focused = store.workspace.focused == pane
        HStack(spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if let bib = label.bib {
                    Text(verbatim: bib)
                        .font(.system(.subheadline, design: .monospaced))
                        .monospacedDigit()
                }
                Text(verbatim: label.name)
                    .font(.subheadline.weight(focused ? .semibold : .regular))
            }
            // trunc-ok: a split pane's one-line header; the name is in .help, the combined accessibility label and the roster line it came from
            .lineLimit(1)
            .truncationMode(.middle)
            .help(label.name)
            .foregroundStyle(focused ? Color.togoSnow : Color.togoMute)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(focused ? [.isHeader, .isSelected] : .isHeader)
            Menu {
                Button(PanesCopy.swap, systemImage: "arrow.left.arrow.right") { store.swapPanes() }
                Button(PanesCopy.closePane, systemImage: "xmark.square") { Self.closePane(pane, store: store) }
                OpenInWindowButton(tab: tab.id)
            } label: {
                Label(PanesCopy.paneMenu, systemImage: "slider.horizontal.3")
                    .labelStyle(.iconOnly)
                    .font(.body.weight(.medium))
                    .foregroundStyle(Color.togoSnow)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
        }
        .padding(.leading, 12)
        .padding(.trailing, 2)
        .frame(minHeight: PaneGeometry.headerHeight)
        .background(Color.togoInk)
        .overlay(alignment: .bottom) {
            Rectangle()
                .fill(Color.togoLine)
                .frame(height: 1 / displayScale)
        }
        .motion(.fade, value: focused)
        // Like the workspace chrome, it stops growing at the third accessibility size.
        .dynamicTypeSize(...DynamicTypeSize.accessibility3)
    }

    /// Closing the primary pane leaves the secondary's tab on its own.
    static func closePane(_ pane: Pane, store: AppStore) {
        if pane == .primary {
            store.swapPanes()
        }
        store.closeSecondary()
    }
}

enum PanesCopy {
    static let swap = String(localized: "panes.swap", defaultValue: "Swap")
    static let closePane = String(localized: "panes.closePane", defaultValue: "Close pane")
    static let paneMenu = String(localized: "panes.paneMenu", defaultValue: "Pane options")
    static let openBeside = String(localized: "panes.openBeside", defaultValue: "Open beside")
    static let keyboard = String(localized: "panes.keyboard", defaultValue: "Keyboard")
    static let divider = String(localized: "panes.divider", defaultValue: "Pane divider")
    static let dividerHint = String(
        localized: "panes.dividerHint", defaultValue: "Swipe up or down to change how the width is shared."
    )
    /// The two panes' shares of the width, such as "50 percent, 50 percent".
    static func dividerValue(_ first: Int) -> String {
        String(localized: "panes.dividerValue", defaultValue: "\(first) percent, \(100 - first) percent")
    }
}
