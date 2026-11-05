import SwiftUI

/// The one layout for a screen with nothing to list yet: empty, loading, failed, offline or
/// waiting on a permission. One symbol, one title, one sentence and at most one button.
struct StateView: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    enum Kind: CaseIterable, Sendable {
        case empty, loading, error, offline, permission

        var systemImage: String? {
            switch self {
            case .empty: "terminal"
            case .loading: nil
            case .error: "exclamationmark.triangle"
            case .offline: "moon.zzz"
            case .permission: "lock"
            }
        }
    }

    struct Action {
        var title: String
        var run: () -> Void

        init(_ title: String, run: @escaping () -> Void) {
            self.title = title
            self.run = run
        }
    }

    let kind: Kind
    let title: String
    let message: String
    var action: Action?

    init(kind: Kind, title: String, message: String, action: Action? = nil) {
        self.kind = kind
        self.title = title
        self.message = message
        self.action = action
    }

    var body: some View {
        VStack(spacing: 14) {
            VStack(spacing: 14) {
                mark
                VStack(spacing: 6) {
                    Text(title)
                        .font(.title3.weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                    Text(message)
                        .font(.body)
                        .foregroundStyle(Color.togoMute)
                }
                .multilineTextAlignment(.center)
            }
            .accessibilityElement(children: .combine)
            if let action {
                Button(action.title, action: action.run)
                    .buttonStyle(.primary(.fit))
                    .padding(.top, 8)
            }
        }
        // A narrow column reads well at normal sizes; at accessibility sizes it would leave
        // one word per line.
        .frame(maxWidth: dynamicTypeSize.isAccessibilitySize ? .infinity : 340)
        .padding(.horizontal, Layout.margin)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    @ViewBuilder private var mark: some View {
        if let systemImage = kind.systemImage {
            Image(systemName: systemImage)
                .font(.system(.largeTitle).weight(.light))
                .foregroundStyle(Color.togoMute)
                .accessibilityHidden(true)
        } else {
            PawWalk()
                .accessibilityHidden(true)
        }
    }
}
