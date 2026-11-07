import SwiftUI
import TogoPush

/// The first page: the oldest waiting request fills the screen; with none, "All clear"
/// over the roster. Answering shows the next request.
struct RequestsView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: WatchStore
    /// An answered request kept on the page for a moment, so its result shows.
    @State private var held: String?
    /// The page on screen. It follows the model's page in one presentation transaction, so the
    /// next request or "All clear" crossfades in rather than popping.
    @State private var shown: WatchHome.Page?

    var body: some View {
        let page = WatchHome.page(for: store.requests, holding: held)
        ZStack {
            switch shown ?? page {
            case let .request(id, position, count):
                // motion: state change — the next request takes the answered one's place, in place
                RequestDetailView(store: store, requestId: id, position: position, count: count)
                    .id(id)
                    .transition(Motion.swap(reduce: reduceMotion))
            case .allClear:
                // motion: state change — nothing is left; the all-clear takes the request's place
                AllClearView(store: store)
                    .transition(Motion.swap(reduce: reduceMotion))
            }
        }
        .onChange(of: page, initial: true) { _, new in
            guard shown != nil else {
                shown = new
                return
            }
            // motion: state change — a crossfade in place, no shift
            withAnimation(Motion.animation(.respond, reduce: reduceMotion)) { shown = new }
        }
        .onChange(of: store.requests.first?.requestId) { old, new in
            let answered = old.map { store.outcome($0) == .approved || store.outcome($0) == .denied } ?? false
            guard WatchHome.holds(previous: old, next: new, answeredHere: answered), let old else { return }
            held = old
            // A model-level hold: the page itself changes through `shown`'s transaction.
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
                ForEach(WatchHome.lines(store.sessions, limit: 4), id: \.id) { line in
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
