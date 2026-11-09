import SwiftUI
import TogoKit
import UIKit

/// A line chosen from its menu: the row, and in a side-by-side row whether the old file's
/// half (the leading one) was pressed.
struct CodeLinePick: Equatable, Sendable {
    var row: Int
    var old = false
}

/// Code or diff text in one virtualised view: line numbers, syntax colours, changed words and
/// `+`/`-` glyphs, smooth with tens of thousands of rows. Only rows on screen are laid out.
struct CodeLinesView: UIViewRepresentable {
    var rows: [CodeRow]
    var wrap: Bool
    var showsOldNumbers: Bool
    var scrollTo: Int?
    var onLongPress: ((CodeLinePick) -> Void)?
    var onTopRow: ((Int) -> Void)?

    /// `scrollTo` brings a row to the top when it changes; `onLongPress` adds "Comment on line"
    /// to a line's menu and its VoiceOver actions (never a header's or a notice's) and receives
    /// the line picked. `onTopRow` hears the row at the top whenever the user scrolls, not when
    /// `scrollTo` moves the view.
    init(
        rows: [CodeRow], wrap: Bool, showsOldNumbers: Bool, scrollTo: Int? = nil,
        onLongPress: ((CodeLinePick) -> Void)? = nil, onTopRow: ((Int) -> Void)? = nil
    ) {
        self.rows = rows
        self.wrap = wrap
        self.showsOldNumbers = showsOldNumbers
        self.scrollTo = scrollTo
        self.onLongPress = onLongPress
        self.onTopRow = onTopRow
    }

    func makeUIView(context: Context) -> CodeLinesUIView {
        CodeLinesUIView()
    }

    func updateUIView(_ view: CodeLinesUIView, context: Context) {
        view.update(rows: rows, wrap: wrap, showsOldNumbers: showsOldNumbers, scrollTo: scrollTo, onLongPress: onLongPress)
        view.onTopRow = onTopRow
    }
}

class CodeLinesUIView: UIView, UICollectionViewDataSource, UICollectionViewDelegate {
    let layout = CodeLinesLayout()
    let collectionView: UICollectionView
    let renderer = CodeRenderer()
    private(set) var rows: [CodeRow] = []
    private var wrap = false
    private var showsOldNumbers = false
    private var onLongPress: ((CodeLinePick) -> Void)?
    var onTopRow: ((Int) -> Void)?
    private var reportedTop: Int?
    private var requestedScroll: Int?
    private var pendingScroll: Int?
    private var hunks: [Int] = []
    private var digits = 1
    private var showsGlyphs = false
    private(set) var metrics: CodeMetrics?

    override init(frame: CGRect) {
        collectionView = UICollectionView(frame: .zero, collectionViewLayout: layout)
        super.init(frame: frame)
        backgroundColor = .togoInk
        collectionView.backgroundColor = .togoInk
        collectionView.register(CodeLineCell.self, forCellWithReuseIdentifier: CodeLineCell.reuseIdentifier)
        collectionView.dataSource = self
        collectionView.delegate = self
        collectionView.alwaysBounceVertical = true
        collectionView.contentInsetAdjustmentBehavior = .always
        collectionView.accessibilityCustomRotors = [hunkRotor()]
        collectionView.accessibilityIdentifier = "code-lines"
        collectionView.frame = bounds
        collectionView.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        addSubview(collectionView)
        registerForTraitChanges([
            UITraitPreferredContentSizeCategory.self, UITraitUserInterfaceStyle.self, UITraitDisplayScale.self,
        ]) { (view: CodeLinesUIView, _: UITraitCollection) in
            view.applyMetrics()
        }
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    func update(
        rows: [CodeRow], wrap: Bool, showsOldNumbers: Bool, scrollTo: Int?, onLongPress: ((CodeLinePick) -> Void)?
    ) {
        self.onLongPress = onLongPress
        let rowsChanged = rows != self.rows
        if rowsChanged {
            self.rows = rows
            hunks = rows.indices.filter { rows[$0].isHunk }
            digits = String(rows.reduce(1) { max($0, Self.largestNumber($1)) }).count
            showsGlyphs = rows.contains(where: Self.isDiff)
            layout.rows = rows.map(Self.layoutRow)
        }
        if rowsChanged || wrap != self.wrap || showsOldNumbers != self.showsOldNumbers || metrics == nil {
            self.wrap = wrap
            self.showsOldNumbers = showsOldNumbers
            applyMetrics()
        }
        if let scrollTo, scrollTo != requestedScroll {
            pendingScroll = scrollTo
            setNeedsLayout()
        }
        requestedScroll = scrollTo
    }

    /// Code wraps at accessibility text sizes whatever the setting, so it never needs two-way scrolling.
    var effectiveWrap: Bool { wrap || traitCollection.preferredContentSizeCategory.isAccessibilityCategory }

    private func applyMetrics() {
        // At accessibility sizes one number per line leaves room for the code; the glyph and
        // VoiceOver still say which side a line is on.
        let large = traitCollection.preferredContentSizeCategory.isAccessibilityCategory
        let metrics = CodeMetrics(
            traits: traitCollection, digits: digits, showsOld: showsOldNumbers && !large, showsGlyphs: showsGlyphs
        )
        self.metrics = metrics
        layout.lineHeight = metrics.lineHeight
        layout.advance = metrics.advance
        layout.gutterWidth = metrics.gutterWidth
        layout.sideGutterWidth = metrics.sideGutterWidth
        layout.wrap = effectiveWrap
        layout.measure = CodeMeasure(font: metrics.font)
        renderer.reset(rows: rows, metrics: metrics, traits: traitCollection)
        collectionView.reloadData()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        guard let index = pendingScroll, collectionView.bounds.height > 0, rows.indices.contains(index) else { return }
        pendingScroll = nil
        collectionView.layoutIfNeeded()
        let top = layout.top(ofRow: index) - CodeLinesLayout.verticalInset - collectionView.adjustedContentInset.top
        let maxY = max(-collectionView.adjustedContentInset.top,
                       collectionView.contentSize.height + collectionView.adjustedContentInset.bottom - collectionView.bounds.height)
        let offset = CGPoint(x: collectionView.contentOffset.x, y: min(max(top, -collectionView.adjustedContentInset.top), maxY))
        // The system's own scroll, not a motion role: an animator on the offset would skip cell
        // prefetch and show blank rows. It is off under Reduce Motion.
        collectionView.setContentOffset(offset, animated: window != nil && !UIAccessibility.isReduceMotionEnabled)
    }

    // MARK: - Rows

    static func layoutRow(_ row: CodeRow) -> CodeLinesLayout.Row {
        func shown(_ text: String, _ kind: DiffLineKind?, clipped: Bool) -> CodeText {
            kind == .noNewline ? CodeText(CodeCopy.noNewline) : CodeText(text, clipped: clipped)
        }
        switch row {
        case let .line(_, _, text, kind, _, _, clipped):
            return .code(shown(text, kind, clipped: clipped))
        case let .split(left, right):
            return .split(
                left.map { shown($0.text, $0.kind, clipped: false) } ?? CodeText(""),
                right.map { shown($0.text, $0.kind, clipped: false) } ?? CodeText("")
            )
        case let .hunk(text), let .notice(text):
            return .banner(CodeText(text, words: true))
        }
    }

    private static func largestNumber(_ row: CodeRow) -> Int {
        switch row {
        case let .line(number, oldNumber, _, _, _, _, _): max(number ?? 0, oldNumber ?? 0)
        case let .split(left, right): max(left?.number ?? 0, right?.number ?? 0)
        case .hunk, .notice: 0
        }
    }

    private static func isDiff(_ row: CodeRow) -> Bool {
        switch row {
        case let .line(_, _, _, kind, _, _, _): kind != nil
        case .split, .hunk: true
        case .notice: false
        }
    }

    // MARK: - Data source

    func collectionView(_ collectionView: UICollectionView, numberOfItemsInSection section: Int) -> Int {
        rows.count
    }

    func collectionView(_ collectionView: UICollectionView, cellForItemAt indexPath: IndexPath) -> UICollectionViewCell {
        let cell = collectionView.dequeueReusableCell(withReuseIdentifier: CodeLineCell.reuseIdentifier, for: indexPath)
        if let cell = cell as? CodeLineCell, let metrics {
            let render = renderer.render(indexPath.item, layout: layout)
            let tiles = renderer.tileSource(indexPath.item, render: render, height: layout.height(ofRow: indexPath.item))
            cell.configure(row: rows[indexPath.item], render: render, metrics: metrics, tiles: tiles)
            cell.accessibilityCustomActions = lineActions(indexPath.item)
        }
        return cell
    }

    // MARK: - Scrolling

    /// The row under the top edge of the visible area.
    var topRow: Int {
        layout.row(at: collectionView.contentOffset.y + collectionView.adjustedContentInset.top)
    }

    func scrollViewDidScroll(_ scrollView: UIScrollView) {
        guard let onTopRow, scrollView.isTracking || scrollView.isDragging || scrollView.isDecelerating,
              !rows.isEmpty else { return }
        let top = topRow
        guard top != reportedTop else { return }
        reportedTop = top
        onTopRow(top)
    }

    // MARK: - Menu

    func collectionView(
        _ collectionView: UICollectionView, contextMenuConfigurationForItemsAt indexPaths: [IndexPath], point: CGPoint
    ) -> UIContextMenuConfiguration? {
        guard let index = indexPaths.first?.item, rows.indices.contains(index) else { return nil }
        let text: String
        let pick: CodeLinePick
        switch rows[index] {
        case let .line(_, _, _, kind, _, _, _) where kind != .noNewline:
            text = rows[index].copyText
            pick = CodeLinePick(row: index)
        case let .split(left, right):
            let old = point.x < layout.halfWidth
            guard let side = old ? left ?? right : right ?? left else { return nil }
            text = side.text
            pick = CodeLinePick(row: index, old: old)
        default:
            return nil
        }
        let comment = onLongPress
        return UIContextMenuConfiguration(identifier: index as NSNumber, previewProvider: nil) { _ in
            var actions = [
                UIAction(title: CodeCopy.copyLine, image: UIImage(systemName: "doc.on.doc")) { _ in
                    UIPasteboard.general.string = text
                },
            ]
            if let comment {
                actions.append(UIAction(title: CodeCopy.comment, image: UIImage(systemName: "text.bubble")) { _ in
                    comment(pick)
                })
            }
            return UIMenu(children: actions)
        }
    }

    // MARK: - VoiceOver

    /// The line menu as VoiceOver actions: Copy line, and a comment on each half that has a line.
    private func lineActions(_ index: Int) -> [UIAccessibilityCustomAction]? {
        guard rows.indices.contains(index) else { return nil }
        var picks: [(String, CodeLinePick)] = []
        switch rows[index] {
        case let .line(_, _, _, kind, _, _, _) where kind != .noNewline:
            picks = [(CodeCopy.comment, CodeLinePick(row: index))]
        case let .split(left, right):
            let numbered = { (side: CodeRowSide?) in side.map { $0.number != nil && $0.kind != .noNewline } ?? false }
            switch (numbered(left), numbered(right)) {
            case (true, true) where left?.kind != .context:
                picks = [
                    (CodeCopy.commentOld, CodeLinePick(row: index, old: true)),
                    (CodeCopy.commentNew, CodeLinePick(row: index)),
                ]
            case (true, false): picks = [(CodeCopy.comment, CodeLinePick(row: index, old: true))]
            case (_, true): picks = [(CodeCopy.comment, CodeLinePick(row: index))]
            default: return nil
            }
        default:
            return nil
        }
        let text = rows[index].copyText
        var actions = [
            UIAccessibilityCustomAction(name: CodeCopy.copyLine, image: UIImage(systemName: "doc.on.doc")) { _ in
                UIPasteboard.general.string = text
                return true
            },
        ]
        if onLongPress != nil {
            actions += picks.map { name, pick in
                UIAccessibilityCustomAction(name: name, image: UIImage(systemName: "text.bubble")) { [weak self] _ in
                    self?.onLongPress?(pick)
                    return true
                }
            }
        }
        return actions
    }

    /// Hunk headers as headings, found even when they are far off screen.
    private func hunkRotor() -> UIAccessibilityCustomRotor {
        UIAccessibilityCustomRotor(systemType: .heading) { [weak self] predicate in
            MainActor.assumeIsolated { self?.hunk(after: predicate) }
        }
    }

    private func hunk(after predicate: UIAccessibilityCustomRotorSearchPredicate) -> UIAccessibilityCustomRotorItemResult? {
        let forward = predicate.searchDirection == .next
        let current = (predicate.currentItem.targetElement as? UICollectionViewCell)
            .flatMap { collectionView.indexPath(for: $0)?.item } ?? (forward ? -1 : rows.count)
        guard let target = forward ? hunks.first(where: { $0 > current }) : hunks.last(where: { $0 < current }) else {
            return nil
        }
        let path = IndexPath(item: target, section: 0)
        collectionView.scrollToItem(at: path, at: .top, animated: false)
        collectionView.layoutIfNeeded()
        guard let cell = collectionView.cellForItem(at: path) else { return nil }
        return UIAccessibilityCustomRotorItemResult(targetElement: cell, targetRange: nil)
    }
}

#if DEBUG
#Preview("Unified") {
    let file = DiffParser.parse(ReviewFixtures.diffs["src/router.ts"]!).first!
    CodeLinesView(rows: CodeRow.unified(file), wrap: false, showsOldNumbers: true)
        .background(Color.togoInk)
}
#endif
