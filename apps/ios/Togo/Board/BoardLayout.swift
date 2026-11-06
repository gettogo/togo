import CoreGraphics
import SwiftUI
import TogoKit

/// One group of cards on the board.
struct BoardSection: Identifiable, Equatable, Sendable {
    enum Kind: CaseIterable, Sendable {
        case needsYou, working, done, idle
    }

    var kind: Kind
    var cards: [BoardCard]
    var id: Kind { kind }
}

struct BoardCard: Identifiable, Equatable, Sendable {
    var row: SessionRow
    /// The machine's name, under the card's title.
    var machine: String
    var symbol: HostSymbol
    var id: TabID { TabID(hostID: row.hostID, sessionID: row.id) }
}

enum Board {
    /// Live sessions grouped by status, in machine order and bib order within each group. Ended
    /// sessions stay with the archive; a status this version does not know counts as idle.
    static func sections(machines: [MachineSection]) -> [BoardSection] {
        var groups: [BoardSection.Kind: [BoardCard]] = [:]
        for machine in machines {
            for row in machine.sessions {
                guard let kind = kind(of: row.status) else { continue }
                groups[kind, default: []].append(BoardCard(row: row, machine: machine.name, symbol: machine.symbol))
            }
        }
        return BoardSection.Kind.allCases.compactMap { kind in
            groups[kind].map { BoardSection(kind: kind, cards: $0) }
        }
    }

    /// The cards reported on screen that are still on the board. A card that went with its
    /// session may never report leaving.
    static func onBoard(_ visible: Set<TabID>, sections: [BoardSection]) -> Set<TabID> {
        visible.intersection(sections.lazy.flatMap(\.cards).map(\.id))
    }

    private static func kind(of status: SessionStatus) -> BoardSection.Kind? {
        switch status {
        case .needsYou: .needsYou
        case .working: .working
        // An agent that ended or failed waits on the user like one that finished its turn.
        case .done, .ended, .failed: .done
        case .idle, .unknown: .idle
        case .exited: nil
        }
    }
}

/// Where the board's columns go across its width.
enum BoardLayout {
    static let minimumCardWidth: CGFloat = 280
    static let spacing: CGFloat = 16
    /// The board's side margin.
    static let margin: CGFloat = 20

    struct Grid: Equatable {
        /// Each column's span across the board.
        var columns: [ClosedRange<CGFloat>]
    }

    /// As many 280 pt columns as fit; on a device with a fold, rounded down to an even count so
    /// the fold falls between columns.
    static func columnCount(width: CGFloat, fold: FoldInfo) -> Int {
        columnCount(usable: width - 2 * margin, fold: fold)
    }

    private static func columnCount(usable: CGFloat, fold: FoldInfo) -> Int {
        var count = max(1, Int(((usable + spacing) / (minimumCardWidth + spacing)).rounded(.down)))
        if fold.inactiveDivision != nil || fold.division != nil, count > 1, count % 2 == 1 {
            count -= 1
        }
        return count
    }

    /// The columns for a board `width` wide. A vertical fold across the middle of the board,
    /// bent or not, becomes the middle gap. A bent fold at one edge, as in book posture beside
    /// the sidebar, moves that edge's margin past it. No card ever sits on the fold.
    static func grid(width: CGFloat, fold: FoldInfo) -> Grid {
        if let band = edgeBand(width: width, fold: fold) {
            let start = band.midX < width / 2 ? max(margin, band.maxX + spacing) : margin
            let end = band.midX < width / 2 ? width - margin : min(width - margin, band.minX - spacing)
            let count = max(1, Int(((end - start + spacing) / (minimumCardWidth + spacing)).rounded(.down)))
            return Grid(columns: even(from: start, to: end, count: count))
        }
        let count = columnCount(width: width, fold: fold)
        let band = middleBand(width: width, fold: fold)
        guard count > 1, count % 2 == 0, let band else {
            return Grid(columns: even(from: margin, to: width - margin, count: count))
        }
        let gap = max(spacing / 2, band.width / 2 + spacing / 2)
        let leading = even(from: margin, to: band.midX - gap, count: count / 2)
        let trailing = even(from: band.midX + gap, to: width - margin, count: count / 2)
        guard (leading + trailing).allSatisfy({ $0.upperBound - $0.lowerBound >= minimumCardWidth }) else {
            return Grid(columns: even(from: margin, to: width - margin, count: count))
        }
        return Grid(columns: leading + trailing)
    }

    /// The fold, when it runs top to bottom through the middle of the board. A fold anywhere
    /// else is not in this view's coordinates, such as one read for the whole window.
    private static func middleBand(width: CGFloat, fold: FoldInfo) -> CGRect? {
        guard let band = fold.division ?? fold.inactiveDivision, band.height > band.width,
              abs(band.midX - width / 2) < 1 else { return nil }
        return band
    }

    /// The bent fold, when it reaches into the board away from its middle.
    private static func edgeBand(width: CGFloat, fold: FoldInfo) -> CGRect? {
        guard let band = fold.division, band.height > band.width, band.maxX > 0, band.minX < width,
              abs(band.midX - width / 2) >= 1 else { return nil }
        return band
    }

    private static func even(from start: CGFloat, to end: CGFloat, count: Int) -> [ClosedRange<CGFloat>] {
        let width = max(0, (end - start - CGFloat(count - 1) * spacing) / CGFloat(count))
        return (0..<count).map { index in
            let x = start + CGFloat(index) * (width + spacing)
            return x...(x + width)
        }
    }
}

extension ClosedRange where Bound == CGFloat {
    var minX: CGFloat { lowerBound }
    var maxX: CGFloat { upperBound }
    var width: CGFloat { upperBound - lowerBound }
}
