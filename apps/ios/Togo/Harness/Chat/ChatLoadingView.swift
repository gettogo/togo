import SwiftUI

/// The same loading feedback applies to an empty chat and one keeping cached rows.
enum ChatLoadingState: Equatable {
    case none, reading, reconnecting, empty

    @MainActor init(_ store: HarnessSessionStore) {
        if store.loadingOlder { self = .reading; return }
        self = switch store.phase {
        case .describing, .opening: .reading
        case .reconnecting: .reconnecting
        case .live, .terminalOnly: Self.hasContent(store) ? .none : .empty
        }
    }

    @MainActor static func hasContent(_ store: HarnessSessionStore) -> Bool {
        !store.rows.isEmpty || !store.queue.isEmpty || store.liveWork != nil
    }

    var isProgress: Bool { self == .reading || self == .reconnecting }
}

struct ChatLoadingView: View {
    let state: ChatLoadingState
    var inline = false

    var body: some View {
        Group {
            switch state {
            case .reading, .reconnecting:
                ProgressView(state == .reading ? ChatCopy.reading : ChatCopy.reconnecting)
                    .controlSize(inline ? .small : .regular)
                    .font(inline ? .footnote : .body)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("chat.loading")
            case .empty:
                ContentUnavailableView {
                    Label(ChatCopy.emptyTitle, systemImage: "bubble.left.and.text.bubble.right")
                } description: {
                    Text(ChatCopy.emptyDetail)
                }
            case .none:
                EmptyView()
            }
        }
        .foregroundStyle(Color.togoMute)
    }
}
