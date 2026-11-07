import SwiftUI
import TogoKit

/// Running things: agents, shells, servers, jobs. Each row has its state, how long it has run,
/// progress when known and the last lines of its output in mono. With `refreshSeconds` above 0
/// the list reads itself again while it is on screen.
struct LiveListPrimitive: View {
    @Environment(\.scenePhase) private var scenePhase
    let list: HarnessScreen.LiveList
    let context: PrimitiveContext
    /// Drawn inside another scroll view, such as a mission control section: no scroll of its own.
    var embedded = false
    @State private var actions = PrimitiveActions()

    var body: some View {
        Group {
            if embedded {
                content
            } else {
                ScrollView { content.padding(.horizontal, 16).padding(.vertical, 12) }
                    .background(Color.togoInk)
                    .refreshable { await context.reload?() }
            }
        }
        .primitiveActions(actions, context: context)
        .task(id: LiveListLogic.refreshKey(seconds: list.refreshSeconds, active: scenePhase == .active)) {
            guard list.refreshSeconds > 0, scenePhase == .active, let reload = context.reload else { return }
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(list.refreshSeconds))
                guard !Task.isCancelled else { return }
                await reload()
            }
        }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 12) {
            let summary = LiveListLogic.summary(list.rows)
            if !summary.isEmpty {
                Text(verbatim: summary)
                    .font(.mono(.footnote))
                    .foregroundStyle(Color.togoMute)
                    .padding(.horizontal, 4)
            }
            if list.rows.isEmpty {
                Text(PrimitiveCopy.nothingHere)
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, minHeight: 120)
            } else {
                VStack(spacing: 0) {
                    ForEach(LiveListLogic.flatten(list.rows)) { entry in
                        LiveRow(row: entry.row, depth: entry.depth, first: entry.index == 0, actions: actions, context: context)
                    }
                }
                .background(Color.togoRaised, in: .rect(cornerRadius: 18, style: .continuous))
            }
        }
    }
}

/// Live list arithmetic, apart from the view.
enum LiveListLogic {
    /// "2 running  1 done  1 failed": counts by state, separated by spaces.
    static func summary(_ rows: [HarnessRow]) -> String {
        let all = flatten(rows).map(\.row.state)
        let live = all.filter(\.isLive).count
        let done = all.filter { $0 == .ok || $0 == .done }.count
        let failed = all.filter { $0 == .failed }.count
        var parts: [String] = []
        if live > 0 { parts.append(String(localized: "primitive.live.running", defaultValue: "\(live) running")) }
        if done > 0 { parts.append(String(localized: "primitive.live.done", defaultValue: "\(done) done")) }
        if failed > 0 { parts.append(String(localized: "primitive.live.failed", defaultValue: "\(failed) failed")) }
        return parts.joined(separator: "  ")
    }

    struct Entry: Identifiable {
        var row: HarnessRow
        var depth: Int
        var index: Int
        var id: String { row.id }
    }

    /// Rows in reading order with their nesting depth.
    static func flatten(_ rows: [HarnessRow]) -> [Entry] {
        var out: [Entry] = []
        func walk(_ rows: [HarnessRow], _ depth: Int) {
            for row in rows {
                out.append(Entry(row: row, depth: depth, index: out.count))
                walk(row.children ?? [], depth + 1)
            }
        }
        walk(rows, 0)
        return out
    }

    /// Changes when polling should restart: a new interval, or the app coming back.
    static func refreshKey(seconds: Int, active: Bool) -> String { "\(seconds)-\(active)" }

    /// At most four lines of tail, as the protocol promises; a longer tail keeps its end.
    static func tail(_ row: HarnessRow) -> [String] {
        Array((row.tail ?? []).suffix(4))
    }
}

private struct LiveRow: View {
    @Environment(\.loopsPaused) private var loopsPaused
    let row: HarnessRow
    let depth: Int
    let first: Bool
    let actions: PrimitiveActions
    let context: PrimitiveContext
    @ScaledMetric(relativeTo: .body) private var tile: CGFloat = 26
    @ScaledMetric(relativeTo: .body) private var indent: CGFloat = 20

    var body: some View {
        let look = PrimitiveStateLook(row.state)
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: look.symbol)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(look.color)
                    .frame(width: tile, height: tile)
                    .background(look.color.opacity(row.state.isLive ? 0.1 : 0.14), in: .rect(cornerRadius: 8, style: .continuous))
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: row.title)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                    if let subtitle = row.subtitle, !subtitle.isEmpty {
                        Text(verbatim: subtitle)
                            .font(.mono(.caption))
                            .foregroundStyle(Color.togoMute)
                            .lineLimit(2)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                elapsed
                if row.open != nil {
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(Color.togoMute)
                        .padding(.top, 4)
                        .accessibilityHidden(true)
                }
            }
            if let progress = row.progress {
                ProgressView(value: min(1, max(0, progress)))
                    .tint(row.state == .failed ? PrimitiveTint.fail : Color.togoSnow)
                    .padding(.leading, tile + 10)
                    .motion(.collapse, value: progress)
            }
            let tail = LiveListLogic.tail(row)
            if !tail.isEmpty {
                Text(verbatim: tail.joined(separator: "\n"))
                    .font(.mono(.caption2))
                    .foregroundStyle(Color.togoSnow.opacity(0.8))
                    .lineLimit(4)
                    .primitiveWell(radius: 10)
                    .padding(.leading, tile + 10)
            }
            if let refs = row.actions, !refs.isEmpty {
                PrimitiveActionRow(refs: refs, target: row.id, actions: actions, context: context)
                    .padding(.leading, tile + 10)
            }
        }
        .padding(.leading, 14 + CGFloat(depth) * indent)
        .padding(.trailing, 14)
        .padding(.vertical, 12)
        .contentShape(.rect)
        .onTapGesture { if let open = row.open { context.open(open) } }
        .overlay(alignment: .top) {
            if !first { Rectangle().fill(Color.togoLine).frame(height: 1).padding(.leading, 14 + tile + 10) }
        }
        .contextMenu {
            if let open = row.open {
                Button(PrimitiveCopy.details, systemImage: "arrow.up.forward.square") { context.open(open) }
            }
            ForEach(row.actions ?? []) { ref in
                Button(ref.label, systemImage: PrimitiveActionSymbol.of(ref), role: ref.role == .destructive ? .destructive : nil) {
                    Task { await actions.trigger(ref, target: row.id, context: context) }
                }
            }
            Button(PrimitiveCopy.openTerminal, systemImage: "terminal") { context.showTerminal() }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([row.title, row.subtitle].compactMap { $0 }.joined(separator: ", "))
        .accessibilityValue(spokenValue(look))
        .accessibilityAddTraits(row.open != nil ? .isButton : [])
        .accessibilityAction { if let open = row.open { context.open(open) } }
        .accessibilityActions {
            ForEach(row.actions ?? []) { ref in
                Button(ref.label) { Task { await actions.trigger(ref, target: row.id, context: context) } }
            }
        }
    }

    @ViewBuilder private var elapsed: some View {
        if let started = row.startedAt?.date, row.state.isLive {
            if loopsPaused {
                Text(verbatim: PrimitiveElapsed.string(0)).hidden()
            } else {
                TimelineView(.periodic(from: started, by: 1)) { timeline in
                    Text(verbatim: PrimitiveElapsed.string(timeline.date.timeIntervalSince(started)))
                        .font(.mono(.caption))
                        .monospacedDigit()
                        .foregroundStyle(Color.togoMute)
                }
            }
        }
    }

    private func spokenValue(_ look: PrimitiveStateLook) -> String {
        var parts = [look.label]
        if let progress = row.progress { parts.append(progress.formatted(.percent.precision(.fractionLength(0)))) }
        if let last = row.tail?.last { parts.append(last) }
        return parts.joined(separator: ", ")
    }
}
