import SwiftUI
import TogoKit

/// Where the session stands: model, mode and effort, how full the context is (with Compact on
/// its meter, the thing it changes), usage meters with when they reset, cost, other facts, and
/// any other actions the harness offers.
///
/// Motion: the block is there on the first render; a part that arrives later fades in place.
/// Values change in place; only the percentages roll, in the direction they moved, once per
/// reply. Never inside a timeline.
struct StatusPrimitive: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let status: HarnessStatus
    let context: PrimitiveContext
    /// Drawn inside another scroll view, such as a mission control section: no scroll of its own.
    var embedded = false
    @State private var actions = PrimitiveActions()
    @ScaledMetric(relativeTo: .subheadline) private var minimumCellWidth: CGFloat = 104

    var body: some View {
        Group {
            if embedded {
                content
            } else {
                ScrollView { content.padding(.horizontal, 16).padding(.vertical, 12) }
                    .background(Color.togoInk)
            }
        }
        .primitiveActions(actions, context: context)
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 14) {
            let cells = StatusLogic.cells(status)
            let compact = StatusLogic.compactAction(status)
            if !cells.isEmpty {
                StatusCellsLayout(minimum: minimumCellWidth) {
                    ForEach(cells, id: \.label) { cell in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(cell.label).font(.caption).foregroundStyle(Color.togoMute)
                            Text(verbatim: cell.value)
                                .font(.mono(.subheadline, weight: .medium))
                                .foregroundStyle(Color.togoSnow)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 10)
                        .background(Color.togoGrouped)
                        .accessibilityElement(children: .combine)
                    }
                }
                .clipShape(.rect(cornerRadius: 14, style: .continuous))
                // motion: preventing a jump — the cells fade in where space opened; nothing rises.
                .transition(Motion.arrive(reduce: reduceMotion))
            }
            if let percent = StatusLogic.knownContext(status.contextPercent) {
                StatusMeter(label: PrimitiveCopy.context, percent: percent, resetsAt: nil) {
                    if let compact {
                        CompactButton(ref: compact, percent: percent, context: context)
                    }
                }
                // motion: preventing a jump — the meter fades in where space opened.
                .transition(Motion.arrive(reduce: reduceMotion))
            }
            ForEach(status.meters ?? []) { meter in
                StatusMeter(label: meter.label, percent: meter.percent, resetsAt: meter.resetsAt?.date) { EmptyView() }
            }
            if let facts = status.facts, !facts.isEmpty {
                VStack(spacing: 0) {
                    ForEach(Array(facts.enumerated()), id: \.offset) { index, fact in
                        if index > 0 { Rectangle().fill(Color.togoLine).frame(height: 1) }
                        LabeledContent {
                            Text(verbatim: fact.value).font(.mono(.subheadline)).foregroundStyle(Color.togoSnow)
                        } label: {
                            Text(fact.label).foregroundStyle(Color.togoMute)
                        }
                        .font(.subheadline)
                        .padding(.horizontal, 12)
                        .frame(minHeight: 44)
                    }
                }
                .background(Color.togoRaised, in: .rect(cornerRadius: 14, style: .continuous))
                // motion: preventing a jump — the facts fade in where space opened.
                .transition(Motion.arrive(reduce: reduceMotion))
            }
            let others = StatusLogic.otherActions(status)
            if !others.isEmpty {
                PrimitiveActionRow(refs: others, actions: actions, context: context)
                    // motion: preventing a jump — the actions fade in where space opened.
                    .transition(Motion.arrive(reduce: reduceMotion))
            }
        }
    }
}

enum StatusLogic {
    /// A context reading worth showing: a fresh session's 0 % (a status line or token feed that
    /// has nothing yet) is unknown, not "empty".
    static func knownContext(_ percent: Double?) -> Double? {
        guard let percent, percent.isFinite, percent >= 0.5 else { return nil }
        return percent
    }

    struct Cell: Equatable {
        var label: String
        var value: String
    }

    /// The action ids that compact the conversation: claude, copilot, droid, kimi, pi and vibe
    /// say `compact`; omp's one-shot is `snapcompact`.
    static let compactIDs: Set<String> = ["compact", "snapcompact"]

    /// The headline numbers: model, mode, effort and cost, the ones the harness reported.
    static func cells(_ status: HarnessStatus) -> [Cell] {
        var cells: [Cell] = []
        if let model = status.model, !model.isEmpty { cells.append(Cell(label: PrimitiveCopy.model, value: model)) }
        if let mode = status.mode, !mode.isEmpty { cells.append(Cell(label: PrimitiveCopy.mode, value: mode)) }
        if let effort = status.effort, !effort.isEmpty { cells.append(Cell(label: PrimitiveCopy.effort, value: effort)) }
        if let cost = status.costUsd { cells.append(Cell(label: PrimitiveCopy.cost, value: cost.formatted(.currency(code: "USD")))) }
        return cells
    }

    /// Compact, when the harness offers it in its Status actions. It sits on the context meter;
    /// without a context reading it stays in the action row.
    static func compactAction(_ status: HarnessStatus) -> HarnessActionRef? {
        guard StatusLogic.knownContext(status.contextPercent) != nil else { return nil }
        return status.actions?.first { compactIDs.contains($0.id) }
    }

    /// The actions that are not on the context meter.
    static func otherActions(_ status: HarnessStatus) -> [HarnessActionRef] {
        let compact = compactAction(status)?.id
        return (status.actions ?? []).filter { $0.id != compact }
    }

    /// Compacting: while the action runs, then until the context reading drops below where it
    /// was when Compact was asked for. The chat's "compacted" notice says the rest.
    static func compacting(running: Bool, from: Double?, percent: Double?) -> Bool {
        if running { return true }
        guard let from else { return false }
        guard let percent else { return true }
        return percent >= from
    }

    /// 0...1 from a percent the harness may send out of range.
    static func fraction(_ percent: Double) -> Double {
        min(1, max(0, percent / 100))
    }

    enum Level: Equatable { case normal, high, full }

    /// Amber from 80 percent, red from 95.
    static func level(_ percent: Double) -> Level {
        percent >= 95 ? .full : percent >= 80 ? .high : .normal
    }

    static func percentText(_ percent: Double) -> String {
        "\(Int(min(100, max(0, percent)).rounded()))%"
    }
}

/// Words on the Status block's own controls.
enum StatusCopy {
    static let compact = String(localized: "status.compact", defaultValue: "Compact")
    static let compacting = String(localized: "status.compacting", defaultValue: "Compacting")
    static let compactHint = String(localized: "status.compact.hint", defaultValue: "Summarises the conversation to free up context")
}

/// A labelled bar with its percent in mono and, for usage limits, when it resets. `accessory`
/// sits at the end of the label's line (Compact on the context meter), or under the bar at
/// accessibility sizes.
private struct StatusMeter<Accessory: View>: View {
    @Environment(\.dynamicTypeSize) private var typeSize
    let label: String
    let percent: Double
    let resetsAt: Date?
    @ViewBuilder let accessory: () -> Accessory

    var body: some View {
        let level = StatusLogic.level(percent)
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Text(label).font(.subheadline.weight(.semibold)).foregroundStyle(Color.togoSnow)
                Spacer(minLength: 8)
                Text(verbatim: StatusLogic.percentText(percent))
                    .font(.mono(.subheadline))
                    // A background value (motion v2 §2): instant, never rolled; the gauge's
                    // `progress` fill already shows the change.
                    .clockText()
                    .foregroundStyle(level == .normal ? Color.togoSnow : tint(level))
                if !typeSize.isAccessibilitySize { accessory() }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(label)
            .accessibilityValue(spokenValue)
            Gauge(value: StatusLogic.fraction(percent)) { EmptyView() }
                .gaugeStyle(.linearCapacity)
                .tint(tint(level))
                // motion: continuity — the bar fills or empties from the old reading to the new.
                .motion(.progress, value: percent)
                .accessibilityHidden(true)
            if let resetsAt {
                Text(PrimitiveCopy.resets(resetsAt.formatted(.relative(presentation: .named))))
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .accessibilityHidden(true)
            }
            if typeSize.isAccessibilitySize { accessory() }
        }
        .primitiveCard(radius: 16, padding: 14)
    }

    private var spokenValue: String {
        [
            StatusLogic.percentText(percent),
            resetsAt.map { PrimitiveCopy.resets($0.formatted(.relative(presentation: .named))) },
        ].compactMap { $0 }.joined(separator: ", ")
    }

    private func tint(_ level: StatusLogic.Level) -> Color {
        switch level {
        case .normal: .togoSnow
        case .high: PrimitiveTint.warn
        case .full: PrimitiveTint.fail
        }
    }
}

/// Compact on the context meter: one button and the system's dialog ("Compact now", "With
/// instructions…", Cancel), an action sheet on compact widths and a popover from this button
/// on regular ones. While it works the button says "Compacting" with a spinner in its place;
/// the result is the chat's notice and the meter dropping, no toast.
private struct CompactButton: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let ref: HarnessActionRef
    let percent: Double
    let context: PrimitiveContext
    @State private var actions = PrimitiveActions()
    /// The context reading when Compact went through, until the meter drops below it.
    @State private var from: Double?

    private var compacting: Bool {
        StatusLogic.compacting(running: actions.isRunning(ref.id), from: from, percent: percent)
    }

    private var failure: String? {
        actions.outcome(ref.id).flatMap { $0.isFailure ? $0.title : nil }
    }

    var body: some View {
        VStack(alignment: .trailing, spacing: 4) {
            Button {
                Task { await actions.trigger(ref, place: .control, context: context) }
            } label: {
                HStack(spacing: 6) {
                    if compacting {
                        ProgressView().controlSize(.mini)
                    }
                    Text(compacting ? StatusCopy.compacting : StatusCopy.compact)
                        // motion: cause → effect — the button's own words become what it is doing.
                        .contentTransition(reduceMotion ? .identity : .interpolate)
                }
                // motion: cause → effect — the tapped button turns into its progress in place.
                .motion(.respond, value: compacting)
            }
            .buttonStyle(PrimitiveCapsuleStyle(role: ref.role, large: false))
            .disabled(compacting)
            .accessibilityHint(StatusCopy.compactHint)
            .accessibilityValue(compacting ? StatusCopy.compacting : "")
            .actionConfirmation($actions.asking)
            if let failure {
                // Instant, at the control that failed.
                Text(verbatim: failure)
                    .font(.footnote)
                    .foregroundStyle(PrimitiveTint.fail)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .haptic(.error, trigger: actions.lastOutcome) { _, new in new?.isFailure == true }
        .onChange(of: actions.lastOutcome) { _, outcome in
            guard let outcome, !outcome.isFailure, outcome.kind != .terminal else { return }
            from = percent
        }
        .onChange(of: percent) { _, now in
            if let from, now < from { self.from = nil }
        }
        .task(id: from) {
            // A reading that never drops (the harness says nothing) stops waiting after a while.
            guard from != nil else { return }
            try? await Task.sleep(for: .seconds(120))
            from = nil
        }
    }
}
