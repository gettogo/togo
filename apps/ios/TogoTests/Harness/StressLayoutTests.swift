import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

/// Renders key rows with the longest strings the agents send, at the largest accessibility
/// text size and the narrow folded Duo width, and checks no text was cut: each row must be at
/// least as tall as its words need when they wrap freely, measured with the same font.
@MainActor struct StressLayoutTests {
    /// About the content width of a folded Duo or a narrow iPad split column.
    private let width: CGFloat = 340
    private let ax5 = UITraitCollection(preferredContentSizeCategory: .accessibilityExtraExtraExtraLarge)

    private func fitted(_ view: some View, width: CGFloat) -> CGSize {
        let host = UIHostingController(rootView: view.environment(\.dynamicTypeSize, .accessibility5))
        return host.sizeThatFits(in: CGSize(width: width, height: .greatestFiniteMagnitude))
    }

    /// The height `text` needs at `width` in the AX5 size of `style`, wrapped freely.
    private func needed(_ text: String, _ style: UIFont.TextStyle, weight: UIFont.Weight = .regular,
                        mono: Bool = false, width: CGFloat) -> CGFloat {
        let size = UIFont.preferredFont(forTextStyle: style, compatibleWith: ax5).pointSize
        let font = mono ? UIFont.monospacedSystemFont(ofSize: size, weight: weight) : UIFont.systemFont(ofSize: size, weight: weight)
        let box = (text as NSString).boundingRect(
            with: CGSize(width: width, height: .greatestFiniteMagnitude),
            options: [.usesLineFragmentOrigin, .usesFontLeading], attributes: [.font: font], context: nil
        )
        return ceil(box.height)
    }

    @Test func everyExtraAnswerShowsAllItsWords() {
        for option in StressFixtures.options {
            let size = fitted(AnswerOptionList(options: [option]) { _ in }, width: width)
            // Row padding is 14 on each side.
            var words = needed(option.title, .body, weight: .semibold, width: width - 28)
            if let why = option.explanation { words += needed(why, .footnote, width: width - 28) }
            #expect(size.height >= words, "\(option.label) was cut: \(size.height) < \(words)")
            #expect(size.width <= width + 0.5)
        }
    }

    @Test func aLongApproveLabelWrapsInItsCapsule() {
        let size = fitted(ApproveButton(StressFixtures.approveLabel) {}, width: width)
        let words = needed(StressFixtures.approveLabel, .body, weight: .semibold, width: width - 36)
        #expect(size.height >= words)
        #expect(size.width <= width + 0.5)
    }

    @Test func aLongAgentTitleWrapsInMissionControl() {
        let size = fitted(MissionRowView(row: StressFixtures.agentRow, opens: true), width: width)
        // Glyph column 20, spacing 12, padding 28, chevron about 30 at AX5.
        let words = needed(StressFixtures.agentName, .body, width: width - 90)
        #expect(size.height >= words)
    }

    @Test func aBackgroundRowKeepsItsShortTitleWhole() {
        let row = StressFixtures.background()
        let size = fitted(MissionBackgroundRow(row: row), width: width)
        let words = needed(BackgroundLabel.title(row), .body, width: width - 90)
        #expect(size.height >= words)
    }

    @Test func modelIdsWrapInsteadOfCutting() {
        for id in StressFixtures.modelIDs {
            let size = fitted(PrimitiveChip(text: id, mono: true, tone: .strong), width: width)
            let words = needed(id, .caption2, mono: true, width: width - 16)
            #expect(size.height >= words, "\(id) was cut")
            #expect(size.width <= width + 0.5)
        }
    }

    @Test func theWaitingLineReadsWhole() {
        let summary = WorkSummary(running: true, startedAt: .now, endedAt: nil, steps: 0, filesChanged: 0, failed: false)
        let line = WorkLine(summary: summary, steps: [], disclosure: Disclosure(), onToggle: {}, onStep: { _ in }, waiting: true)
        let size = fitted(line, width: 200)
        let words = needed(WorkingCopy.waiting, .footnote, weight: .semibold, mono: true, width: 200 - 60)
        #expect(size.height >= words)
    }

    @Test func thePillsCountsWrapRatherThanDrop() {
        let parts = MissionText.parts(StressFixtures.waitingSummary)
        let size = fitted(MissionPill(summary: StressFixtures.waitingSummary) {}, width: width)
        // The pill stops growing at AX2; each count takes at least a line of its own there.
        let ax2 = UIFont.preferredFont(forTextStyle: .footnote,
                                       compatibleWith: UITraitCollection(preferredContentSizeCategory: .accessibilityLarge))
        #expect(size.height >= ax2.lineHeight * CGFloat(parts.count))
        #expect(size.width <= width + 0.5)
    }
}
