import SwiftUI
import TogoKit

/// Where the session stands: model, mode and effort, how full the context is, usage meters with
/// when they reset, cost, other facts, and actions such as Compact.
struct StatusPrimitive: View {
    let status: HarnessStatus
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
            }
        }
        .primitiveActions(actions, context: context)
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 14) {
            let cells = StatusLogic.cells(status)
            if !cells.isEmpty {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 104), spacing: 1)], spacing: 1) {
                    ForEach(cells, id: \.label) { cell in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(cell.label).font(.caption).foregroundStyle(Color.togoMute)
                            Text(verbatim: cell.value)
                                .font(.mono(.subheadline, weight: .medium))
                                .foregroundStyle(Color.togoSnow)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 10)
                        .background(Color.togoGrouped)
                        .accessibilityElement(children: .combine)
                    }
                }
                .clipShape(.rect(cornerRadius: 14, style: .continuous))
            }
            if let percent = status.contextPercent {
                StatusMeter(label: PrimitiveCopy.context, percent: percent, resetsAt: nil)
            }
            ForEach(status.meters ?? []) { meter in
                StatusMeter(label: meter.label, percent: meter.percent, resetsAt: meter.resetsAt?.date)
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
            }
            if let refs = status.actions, !refs.isEmpty {
                PrimitiveActionRow(refs: refs, actions: actions, context: context)
            }
        }
    }
}

enum StatusLogic {
    struct Cell: Equatable {
        var label: String
        var value: String
    }

    /// The headline numbers: model, mode, effort and cost, the ones the harness reported.
    static func cells(_ status: HarnessStatus) -> [Cell] {
        var cells: [Cell] = []
        if let model = status.model, !model.isEmpty { cells.append(Cell(label: PrimitiveCopy.model, value: model)) }
        if let mode = status.mode, !mode.isEmpty { cells.append(Cell(label: PrimitiveCopy.mode, value: mode)) }
        if let effort = status.effort, !effort.isEmpty { cells.append(Cell(label: PrimitiveCopy.effort, value: effort)) }
        if let cost = status.costUsd { cells.append(Cell(label: PrimitiveCopy.cost, value: cost.formatted(.currency(code: "USD")))) }
        return cells
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

/// A labelled bar with its percent in mono and, for usage limits, when it resets.
private struct StatusMeter: View {
    let label: String
    let percent: Double
    let resetsAt: Date?

    var body: some View {
        let level = StatusLogic.level(percent)
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(label).font(.subheadline.weight(.semibold)).foregroundStyle(Color.togoSnow)
                Spacer(minLength: 8)
                Text(verbatim: StatusLogic.percentText(percent))
                    .font(.mono(.subheadline))
                    .monospacedDigit()
                    .foregroundStyle(level == .normal ? Color.togoSnow : tint(level))
            }
            Gauge(value: StatusLogic.fraction(percent)) { EmptyView() }
                .gaugeStyle(.linearCapacity)
                .tint(tint(level))
                .motion(.collapse, value: percent)
            if let resetsAt {
                Text(PrimitiveCopy.resets(resetsAt.formatted(.relative(presentation: .named))))
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
            }
        }
        .primitiveCard(radius: 16, padding: 14)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label)
        .accessibilityValue([
            StatusLogic.percentText(percent),
            resetsAt.map { PrimitiveCopy.resets($0.formatted(.relative(presentation: .named))) },
        ].compactMap { $0 }.joined(separator: ", "))
    }

    private func tint(_ level: StatusLogic.Level) -> Color {
        switch level {
        case .normal: .togoSnow
        case .high: PrimitiveTint.warn
        case .full: PrimitiveTint.fail
        }
    }
}
