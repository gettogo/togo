import SwiftUI

/// The bar above the tab strip: back to the sessions list, which session this is and how it
/// is connected, and the session's actions. At accessibility sizes the title takes its own
/// line under the buttons, so neither is cut short.
struct WorkspaceHeader<Actions: View>: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    let bib: String?
    let title: String
    let link: LinkPhase
    /// A line under the bar that stays until the next action replaces it, such as "Copied".
    let status: String?
    @Binding var noteShown: Bool
    let onBack: () -> Void
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
                    actionsMenu
                }
                titleBlock(alignment: .leading)
                    .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                HStack(spacing: 8) {
                    backButton
                        .frame(maxWidth: .infinity, alignment: .leading)
                    titleBlock(alignment: .center)
                        .layoutPriority(1)
                    actionsMenu
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
        .animation(Motion.stateText(reduce: reduceMotion), value: line)
    }

    private var backButton: some View {
        Button(action: onBack) {
            Label(WorkspaceCopy.sessions, systemImage: "chevron.backward")
                .font(.body.weight(.medium))
                .lineLimit(1)
                .fixedSize()
                .frame(minHeight: 44)
        }
        .buttonStyle(.glass)
        .buttonBorderShape(.capsule)
        .foregroundStyle(Color.togoSnow)
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
            .animation(Motion.stateText(reduce: reduceMotion), value: link)
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
