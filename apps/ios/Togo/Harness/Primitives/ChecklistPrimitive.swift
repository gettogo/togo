import SwiftUI
import TogoKit

/// A plan or todo list: a progress card with a ring and what is being worked on now, then the
/// items with their states, children indented under their parent.
struct ChecklistPrimitive: View {
    let checklist: HarnessScreen.Checklist
    let context: PrimitiveContext
    /// Drawn inside another scroll view, such as a mission control section: no scroll of its own.
    var embedded = false

    var body: some View {
        if embedded {
            content
        } else {
            ScrollView { content.padding(.horizontal, 16).padding(.vertical, 12) }
                .background(Color.togoInk)
        }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 14) {
            ChecklistProgress(checklist: checklist)
            if checklist.items.isEmpty {
                Text(PrimitiveCopy.nothingHere)
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, minHeight: 120)
            } else {
                VStack(spacing: 0) {
                    ForEach(ChecklistLogic.flatten(checklist.items), id: \.item.id) { entry in
                        ChecklistRow(item: entry.item, depth: entry.depth, first: entry.index == 0)
                            .id(entry.item.id)
                    }
                }
                .background(Color.togoRaised, in: .rect(cornerRadius: 18, style: .continuous))
            }
        }
        .accessibilityRotor(String(localized: "primitive.rotor.open", defaultValue: "Open items")) {
            ForEach(ChecklistLogic.flatten(checklist.items).filter { !ChecklistLogic.isFinished($0.item.state) }, id: \.item.id) { entry in
                AccessibilityRotorEntry(entry.item.text, id: entry.item.id)
            }
        }
    }
}

/// Checklist arithmetic, apart from the view.
enum ChecklistLogic {
    struct Entry {
        var item: HarnessScreen.Checklist.Item
        var depth: Int
        var index: Int
    }

    /// Items in reading order with their nesting depth.
    static func flatten(_ items: [HarnessScreen.Checklist.Item], depth: Int = 0) -> [Entry] {
        var out: [Entry] = []
        func walk(_ items: [HarnessScreen.Checklist.Item], _ depth: Int) {
            for item in items {
                out.append(Entry(item: item, depth: depth, index: out.count))
                walk(item.children ?? [], depth + 1)
            }
        }
        walk(items, depth)
        return out
    }

    static func isFinished(_ state: HarnessState) -> Bool {
        state == .done || state == .ok || state == .skipped
    }

    /// The first item being worked on, at any depth.
    static func current(_ items: [HarnessScreen.Checklist.Item]) -> HarnessScreen.Checklist.Item? {
        flatten(items).first { $0.item.state.isLive }?.item
    }

    /// Done over total, 0...1, safe for an empty list.
    static func fraction(done: Int, total: Int) -> Double {
        total > 0 ? min(1, max(0, Double(done) / Double(total))) : 0
    }
}

/// The ring, "3 of 7 done", what is active now, and one segment per top-level item.
private struct ChecklistProgress: View {
    let checklist: HarnessScreen.Checklist
    @ScaledMetric(relativeTo: .title3) private var ring: CGFloat = 52

    var body: some View {
        let fraction = ChecklistLogic.fraction(done: checklist.done, total: checklist.total)
        let finished = checklist.total > 0 && checklist.done >= checklist.total
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 14) {
                ZStack {
                    Circle().stroke(Color.togoLine, lineWidth: 4)
                    Circle()
                        .trim(from: 0, to: fraction)
                        .stroke(finished ? PrimitiveTint.ok : Color.togoSnow, style: StrokeStyle(lineWidth: 4, lineCap: .round))
                        .rotationEffect(.degrees(-90))
                    Text(verbatim: "\(checklist.done)/\(checklist.total)")
                        .font(.mono(.caption, weight: .semibold))
                        .monospacedDigit()
                        .foregroundStyle(Color.togoSnow)
                        // trunc-ok: done/total inside the ring scales to fit; the headline beside it says the same in words
                        .minimumScaleFactor(0.6)
                }
                .frame(width: ring, height: ring)
                .motion(.collapse, value: fraction)
                VStack(alignment: .leading, spacing: 4) {
                    Text(PrimitiveCopy.progress(done: checklist.done, total: checklist.total))
                        .font(.headline)
                        .foregroundStyle(Color.togoSnow)
                    if let current = ChecklistLogic.current(checklist.items) {
                        Text(verbatim: current.text)
                            .font(.mono(.footnote))
                            .foregroundStyle(Color.togoMute)
                            // trunc-ok: the current item, also listed in full below
                            .lineLimit(2)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            if !checklist.items.isEmpty, checklist.items.count <= 24 {
                HStack(spacing: 3) {
                    ForEach(checklist.items) { item in
                        Capsule()
                            .fill(segment(item.state))
                            .frame(height: 4)
                    }
                }
                .accessibilityHidden(true)
            }
        }
        .primitiveCard()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(PrimitiveCopy.progress(done: checklist.done, total: checklist.total))
        .accessibilityValue(ChecklistLogic.current(checklist.items)?.text ?? "")
    }

    private func segment(_ state: HarnessState) -> Color {
        switch state {
        case .done, .ok: PrimitiveTint.ok
        case .failed: PrimitiveTint.fail
        case .skipped: Color.togoMuteDim.opacity(0.5)
        default: state.isLive ? Color.togoSnow : Color.togoLine
        }
    }
}

private struct ChecklistRow: View {
    let item: HarnessScreen.Checklist.Item
    let depth: Int
    let first: Bool
    @ScaledMetric(relativeTo: .body) private var indent: CGFloat = 24

    var body: some View {
        let look = PrimitiveStateLook(item.state)
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            PrimitiveStateGlyph(state: item.state)
            Text(verbatim: item.text)
                .font(.body)
                .foregroundStyle(ChecklistLogic.isFinished(item.state) ? Color.togoMute : Color.togoSnow)
                .strikethrough(item.state == .skipped, color: Color.togoMute)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.leading, 14 + CGFloat(depth) * indent)
        .padding(.trailing, 14)
        .padding(.vertical, 12)
        .frame(minHeight: 44)
        .background(item.state.isLive ? Color.togoGrouped : Color.clear)
        .overlay(alignment: .top) {
            if !first {
                Rectangle().fill(Color.togoLine).frame(height: 1)
                    .padding(.leading, 14 + CGFloat(depth) * indent + 30)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(look.label): \(item.text)")
    }
}
