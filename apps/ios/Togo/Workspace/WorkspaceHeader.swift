import SwiftUI

/// The button at the start of the workspace header.
enum WorkspaceHeaderLeading: Equatable {
    /// "Sessions", back to the list the workspace covers.
    case back
    /// Shows the sidebar the workspace sits beside.
    case showSidebar
    /// Nothing: the sidebar is on screen beside the workspace.
    case none
}

/// The bar above the tab strip: back to the sessions list, which session this is and how it
/// is connected, and the session's actions, with buttons such as "3 changes" before them. At
/// accessibility sizes the title takes its own line under the buttons, so neither is cut short.
struct WorkspaceHeader<Accessories: View, Actions: View>: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    let bib: String?
    let title: String
    let link: LinkPhase
    /// A line under the bar that stays until the next action replaces it, such as "Copied".
    let status: String?
    @Binding var noteShown: Bool
    let onBack: () -> Void
    var leading: WorkspaceHeaderLeading = .back
    @ViewBuilder let accessories: () -> Accessories
    @ViewBuilder let actions: () -> Actions

    private var fallback: String? {
        guard case .live(let transport, let note?) = link else { return nil }
        return WorkspaceCopy.fallback(transport: transport, note: note)
    }

    private var line: String? {
        if let status { return status }
        return noteShown ? fallback : nil
    }

    var body: some View {
        VStack(spacing: 4) {
            if dynamicTypeSize.isAccessibilitySize {
                HStack {
                    backButton
                    Spacer(minLength: 8)
                    trailing
                }
                titleBlock(alignment: .leading)
                    .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                HStack(spacing: 8) {
                    backButton
                        .frame(maxWidth: .infinity, alignment: .leading)
                    titleBlock(alignment: .center)
                        .layoutPriority(1)
                    trailing
                        .frame(maxWidth: .infinity, alignment: .trailing)
                }
            }
            if let line {
                Text(line)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .multilineTextAlignment(dynamicTypeSize.isAccessibilitySize ? .leading : .center)
                    .frame(maxWidth: .infinity, alignment: dynamicTypeSize.isAccessibilitySize ? .leading : .center)
                    .transition(.opacity)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 4)
        .motion(.fade, value: line)
    }

    /// "Sessions" while there is room for it beside the title and the buttons, otherwise the
    /// chevron alone, which VoiceOver still reads as "Sessions".
    @ViewBuilder private var backButton: some View {
        switch leading {
        case .back:
            ViewThatFits(in: .horizontal) {
                backButton(iconOnly: false)
                backButton(iconOnly: true)
            }
        case .showSidebar:
            Button(action: onBack) {
                Label(AppCommandCopy.showSidebar, systemImage: "sidebar.leading")
                    .labelStyle(.iconOnly)
                    .font(.body.weight(.medium))
                    .frame(minWidth: 44, minHeight: 44)
            }
            .buttonStyle(.glass)
            .buttonBorderShape(.circle)
            .foregroundStyle(Color.togoSnow)
        case .none:
            Color.clear.frame(width: 0, height: 44)
        }
    }

    private func backButton(iconOnly: Bool) -> some View {
        Button(action: onBack) {
            Label(WorkspaceCopy.sessions, systemImage: "chevron.backward")
                .labelStyle(BackLabelStyle(iconOnly: iconOnly))
                .font(.body.weight(.medium))
                .lineLimit(1)
                .fixedSize()
                .frame(minWidth: iconOnly ? 44 : nil, minHeight: 44)
        }
        .buttonStyle(.glass)
        .buttonBorderShape(iconOnly ? .circle : .capsule)
        .foregroundStyle(Color.togoSnow)
    }

    private var trailing: some View {
        HStack(spacing: 8) {
            accessories()
            actionsMenu
        }
    }

    private var actionsMenu: some View {
        Menu {
            actions()
        } label: {
            Label(WorkspaceCopy.actions, systemImage: "ellipsis")
                .labelStyle(.iconOnly)
                .font(.body.weight(.medium))
                .frame(minWidth: 44, minHeight: 44)
        }
        .menuStyle(.button)
        .buttonStyle(.glass)
        .buttonBorderShape(.circle)
        .foregroundStyle(Color.togoSnow)
    }

    private func titleBlock(alignment: HorizontalAlignment) -> some View {
        VStack(alignment: alignment, spacing: 1) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if let bib {
                    BibText(bib, style: .headline)
                }
                Text(verbatim: title)
                    .font(.headline)
                    .foregroundStyle(Color.togoSnow)
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 1)
                    .truncationMode(.middle)
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            linkWord
        }
    }

    /// The transport word explains itself on tap when the link fell back from what was asked.
    @ViewBuilder private var linkWord: some View {
        let word = Text(link.word)
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
            .lineLimit(1)
            .contentTransition(reduceMotion ? .identity : .interpolate)
            .motion(.fade, value: link)
        if fallback != nil {
            Button {
                noteShown.toggle()
            } label: {
                HStack(spacing: 3) {
                    word
                    Image(systemName: "info.circle")
                        .font(.caption2)
                        .foregroundStyle(Color.togoMute)
                }
                // A 44 pt target without making the bar taller.
                .padding(.vertical, 12)
                .padding(.horizontal, 8)
                .contentShape(.rect)
                .padding(.vertical, -12)
                .padding(.horizontal, -8)
            }
            .buttonStyle(.plain)
            .accessibilityHint(WorkspaceCopy.showFallback)
        } else {
            word
        }
    }
}

/// "Run again" beside the session's other buttons once its agent stopped, as an arrow so the
/// title keeps the middle of the bar; the line under the bar says how the agent stopped, and
/// VoiceOver reads the words. While the helper starts it the button waits, so a second tap
/// does nothing.
struct RunAgainButton: View {
    var running = false
    let run: () -> Void

    var body: some View {
        Button(action: run) {
            Label(LifecycleCopy.runAgain, systemImage: "arrow.clockwise")
                .labelStyle(.iconOnly)
                .frame(minWidth: 44, minHeight: 44)
        }
        .font(.body.weight(.medium))
        .buttonStyle(.glass)
        .buttonBorderShape(.circle)
        .foregroundStyle(Color.togoSnow)
        .disabled(running)
    }
}

private struct BackLabelStyle: LabelStyle {
    let iconOnly: Bool

    func makeBody(configuration: Configuration) -> some View {
        if iconOnly {
            Label(configuration).labelStyle(.iconOnly)
        } else {
            Label(configuration).labelStyle(.titleAndIcon)
        }
    }
}

extension LinkPhase {
    /// The one word under the title: the transport while live, otherwise what the link is doing.
    var word: String {
        switch self {
        case .waking: WorkspaceCopy.waking
        case .connecting: WorkspaceCopy.connecting
        case .live(let transport, _): transport
        case .roaming: WorkspaceCopy.roaming
        case .reconnecting: WorkspaceCopy.reconnecting
        case .failed, .signedOut: WorkspaceCopy.notConnected
        }
    }
}
