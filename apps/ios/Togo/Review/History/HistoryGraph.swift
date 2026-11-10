import Foundation
import TogoKit

/// The lanes of a commit graph, one row per commit, as `git log --graph` draws them: each lane
/// waits for the next commit it leads to, a merge opens a lane for its other parent, and lanes
/// meet where branches forked. Commits must come before their parents (the helper's graph log
/// is in topological order).
enum HistoryGraph {
    /// A line in one half of a row: from lane `from` at the edge to lane `to` at the dot, or
    /// from the dot's height to the edge below. `lane` picks the line's color.
    struct Edge: Hashable, Sendable {
        var from: Int
        var to: Int
        var lane: Int
    }

    struct Row: Hashable, Sendable {
        /// The lane the commit's dot sits in.
        var lane: Int
        /// How many lanes the row spans, so every row of a page can share one width.
        var width: Int
        /// Lines from the top of the row down to the dot's height.
        var top: [Edge]
        /// Lines from the dot's height down to the bottom of the row.
        var bottom: [Edge]
        var merge: Bool
    }

    /// The rows for `commits`. A log without parents, from a helper before `gitfiles.v1`, is
    /// drawn as one line, each commit the parent of the one above it.
    static func rows(_ commits: [GitCommit]) -> [Row] {
        let linear = commits.allSatisfy { $0.parents.isEmpty }
        var lanes: [String?] = []
        var rows: [Row] = []
        rows.reserveCapacity(commits.count)
        for (index, commit) in commits.enumerated() {
            let parents: [String] = linear
                ? (index + 1 < commits.count ? [commits[index + 1].hash] : [])
                : commit.parents
            let before = lanes
            let lane: Int
            if let waiting = lanes.firstIndex(of: commit.hash) {
                lane = waiting
            } else if let free = lanes.firstIndex(of: nil) {
                lane = free
            } else {
                lane = lanes.count
                lanes.append(nil)
            }

            var top: [Edge] = []
            var passing: [Int] = []
            for (i, hash) in before.enumerated() {
                guard let hash else { continue }
                if hash == commit.hash {
                    top.append(Edge(from: i, to: lane, lane: i))
                    lanes[i] = nil
                } else {
                    top.append(Edge(from: i, to: i, lane: i))
                    passing.append(i)
                }
            }

            var bottom: [Edge] = passing.map { Edge(from: $0, to: $0, lane: $0) }
            lanes[lane] = parents.first
            if parents.first != nil {
                bottom.append(Edge(from: lane, to: lane, lane: lane))
            }
            for parent in parents.dropFirst() {
                let target: Int
                if let waiting = lanes.firstIndex(of: parent) {
                    target = waiting
                } else if let free = lanes.firstIndex(of: nil) {
                    target = free
                    lanes[free] = parent
                } else {
                    target = lanes.count
                    lanes.append(parent)
                }
                bottom.append(Edge(from: lane, to: target, lane: target))
            }
            while let last = lanes.last, last == nil { lanes.removeLast() }

            rows.append(Row(
                lane: lane, width: max(before.count, lanes.count, lane + 1),
                top: top, bottom: bottom, merge: parents.count > 1
            ))
        }
        return rows
    }
}
