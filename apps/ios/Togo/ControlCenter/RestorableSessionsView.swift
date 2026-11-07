import SwiftUI
import TogoKit

/// A session a restart of the machine ended: what it was, where, and the way back. The
/// actions sit in the row so they are one tap away; swiping the row dismisses it.
struct RestorableSessionRow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let session: RestorableSession
    var busy = false
    let perform: (RestoreAction) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                if !session.bib.isEmpty {
                    BibText(session.bib, style: .body)
                }
                Text(verbatim: session.name)
                    .font(.body)
                    .foregroundStyle(Color.togoSnow)
            }
            Text(RestoreCopy.endedByRestart)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
            PathText(session.cwd)
            actions
                .padding(.top, 4)
        }
        .padding(.vertical, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .contain)
    }

    private var actions: some View {
        let offered = RestoreActions.offered(for: session)
        return ViewThatFits(in: .horizontal) {
            HStack(spacing: 12) { buttons(offered) }
            VStack(alignment: .leading, spacing: 4) { buttons(offered) }
        }
        .disabled(busy)
    }

    @ViewBuilder private func buttons(_ offered: [RestoreAction]) -> some View {
        ForEach(offered, id: \.self) { action in
            Button(RestoreActions.title(action)) { perform(action) }
                .buttonStyle(RestoreButtonStyle(plain: action == .restoreAndContinue))
                .accessibilityLabel(RestoreCopy.axLabel(RestoreActions.title(action), RestoreCopy.label(session)))
        }
    }
}

extension View {
    /// Dismiss on the trailing swipe and in the context menu of a restorable row.
    func dismissesRestorable(_ dismiss: @escaping () -> Void) -> some View {
        // No destructive role on the swipe: the row leaves once the helper has dropped it, and
        // a list that already let it go would count one row short when it does.
        swipeActions(edge: .trailing) {
            Button(RestoreCopy.dismiss, systemImage: "xmark", action: dismiss)
                .tint(.red)
        }
        .contextMenu {
            Button(RestoreCopy.dismiss, systemImage: "xmark", role: .destructive, action: dismiss)
        }
    }
}

/// Restore is the one filled action in a row; continuing the conversation is the quieter one.
private struct RestoreButtonStyle: PrimitiveButtonStyle {
    let plain: Bool

    func makeBody(configuration: Configuration) -> some View {
        if plain {
            Button(configuration) .buttonStyle(.secondary(.fit))
        } else {
            Button(configuration) .buttonStyle(.primary(.fit))
        }
    }
}
