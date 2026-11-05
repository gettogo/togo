/// Lays a diff out side by side: old lines on the left, new lines on the right.
public enum SplitPairing {
    /// Each hunk starts with a header row. Context lines appear on both sides. In a block of
    /// changes the removed lines fill the left column and the added lines the right, in
    /// order, with the shorter side padded by nil.
    public static func rows(_ file: DiffFile) -> [SplitRow] {
        var rows: [SplitRow] = []
        rows.reserveCapacity(file.hunks.reduce(0) { $0 + $1.lines.count + 1 })
        for hunk in file.hunks {
            rows.append(SplitRow(hunkHeader: hunk.header))
            var left: [DiffLine] = []
            var right: [DiffLine] = []
            var previous: DiffLineKind = .context

            func flush() {
                for index in 0..<max(left.count, right.count) {
                    rows.append(SplitRow(
                        left: index < left.count ? left[index] : nil,
                        right: index < right.count ? right[index] : nil
                    ))
                }
                left.removeAll(keepingCapacity: true)
                right.removeAll(keepingCapacity: true)
            }

            for line in hunk.lines {
                switch line.kind {
                case .removed:
                    left.append(line)
                case .added:
                    right.append(line)
                case .context:
                    flush()
                    rows.append(SplitRow(left: line, right: line))
                case .noNewline:
                    switch previous {
                    case .removed: left.append(line)
                    case .added: right.append(line)
                    case .context, .noNewline:
                        flush()
                        rows.append(SplitRow(left: line, right: line))
                    }
                }
                if line.kind != .noNewline { previous = line.kind }
            }
            flush()
        }
        return rows
    }
}
