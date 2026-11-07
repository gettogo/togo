import SwiftUI
import TogoPush

/// The first page: the oldest waiting request fills the screen; with none, "All clear"
/// over the roster. Answering shows the next request.
struct RequestsView: View {
    let store: WatchStore
    /// An answered request kept on the page for a moment, so its result shows.
    @State private var held: String?

    var body: some View {
        Group {
            switch WatchHome.page(for: store.requests, holding: held) {
            case let .request(id, position, count):
                RequestDetailView(store: store, requestId: id, position: position, count: count)
                    .id(id)
                    .transition(.opacity)
            case .allClear:
                AllClearView(store: store)
            }
        }
        .onChange(of: store.requests.first?.requestId) { old, new in
            let answered = old.map { store.outcome($0) == .approved || store.outcome($0) == .denied } ?? false
            guard WatchHome.holds(previous: old, next: new, answeredHere: answered), let old else { return }
            held = old
            Task { @MainActor in
                try? await Task.sleep(for: WatchHome.holdAfterAnswer)
                if held == old { held = nil }
            }
        }
    }
}

/// An unlit beacon ring and "All clear", then the roster.
private struct AllClearView: View {
    let store: WatchStore

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 6) {
                    BeaconDot(lit: false)
                    Text(WatchCopy.allClear)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                ForEach(Array(WatchHome.lines(store.sessions, limit: 4).enumerated()), id: \.offset) { _, line in
                    RosterLine(line: line)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// The beacon: a filled dot while something waits, a hairline ring when clear.
struct BeaconDot: View {
    var lit: Bool
    var size: CGFloat = 8

    var body: some View {
        Group {
            if lit {
                Circle().fill(Color.togoAccent)
            } else {
                Circle().strokeBorder(Color.secondary, lineWidth: 1)
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// "04  Claude Code  Needs you": mono bib, name, status word. Beacon blue only for "Needs you".
struct RosterLine: View {
    let line: WatchHome.Line

    var body: some View {
        HStack(spacing: 6) {
            Text(line.bib)
                .font(.system(.footnote, design: .monospaced))
                .foregroundStyle(.secondary)
                // trunc-ok: a bib is two or three digits
                .lineLimit(1)
                .truncationMode(.middle)
            Text(line.name)
                .font(.footnote)
                .fixedSize(horizontal: false, vertical: true)
                .layoutPriority(1)
            Spacer(minLength: 4)
            Text(line.status)
                .font(.footnote)
                .foregroundStyle(line.needsYou ? Color.togoAccent : Color.secondary)
                // trunc-ok: a status word from a fixed short set
                .lineLimit(1)
        }
        .accessibilityElement(children: .combine)
    }
}
