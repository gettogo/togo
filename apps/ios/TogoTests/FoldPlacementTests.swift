import CoreGraphics
import Testing
@testable import Togo

/// Where a view nested in the window sees the fold, and where a column of text goes around it.
struct FoldPlacementTests {
    /// An unfolded Duo held as a book, read by the root view below a 24 pt status bar.
    private static let band = CGRect(x: 463, y: 0, width: 25, height: 645)
    private static let book = FoldInfo.from(active: band, inactive: band)
    private static let rootOrigin = CGPoint(x: 0, y: 24)

    // MARK: Nested views

    @Test func detailOnTheTrailingPageSeesNoFold() {
        // The sidebar fills the leading page, so the detail starts after the fold.
        let seen = Self.book.seen(from: Self.rootOrigin, at: CGPoint(x: 488, y: 24), size: CGSize(width: 463, height: 645))
        #expect(seen == .flat)
    }

    @Test func detailBesideANarrowSidebarSeesTheFoldInItsOwnCoordinates() {
        let seen = Self.book.seen(from: Self.rootOrigin, at: CGPoint(x: 320, y: 24), size: CGSize(width: 631, height: 645))
        #expect(seen.posture == .book)
        #expect(seen.division == CGRect(x: 143, y: 0, width: 25, height: 645))
        #expect(seen.inactiveDivision == CGRect(x: 143, y: 0, width: 25, height: 645))
    }

    @Test func viewWhereTheFoldWasReadSeesItUnchanged() {
        let seen = Self.book.seen(from: Self.rootOrigin, at: Self.rootOrigin, size: CGSize(width: 951, height: 645))
        #expect(seen == Self.book)
    }

    /// Tests and previews set the fold from outside, already in the view's own coordinates.
    @Test func foldWithoutAnchorIsTakenAsLocal() {
        let seen = Self.book.seen(from: nil, at: CGPoint(x: 200, y: 50), size: CGSize(width: 951, height: 645))
        #expect(seen == Self.book)
    }

    // MARK: Reading column

    @Test func bookPutsTheColumnOnTheLeadingPage() {
        #expect(Self.book.readingPage(from: Self.rootOrigin, minX: 0, width: 951) == 0...463)
    }

    @Test func bookBesideASidebarPutsTheColumnOnTheWiderPage() {
        // The fold is 143 pt into a 631 pt detail: the page after it is the wider one.
        #expect(Self.book.readingPage(from: Self.rootOrigin, minX: 320, width: 631) == 168...631)
    }

    @Test func columnAwayFromTheFoldKeepsItsWidth() {
        #expect(Self.book.readingPage(from: Self.rootOrigin, minX: 488, width: 463) == nil)
    }

    @Test func flatAndTabletopKeepTheCentredColumn() {
        let open = FoldInfo.from(active: nil, inactive: Self.band)
        #expect(open.readingPage(from: nil, minX: 0, width: 951) == nil)
        let tabletop = FoldInfo.from(active: CGRect(x: 0, y: 451, width: 669, height: 25), inactive: nil)
        #expect(tabletop.readingPage(from: nil, minX: 0, width: 669) == nil)
        #expect(FoldInfo.flat.readingPage(from: nil, minX: 0, width: 951) == nil)
    }
}
