import CoreGraphics
import SwiftUI
import Testing
@testable import Togo

struct PaneLayoutTests {
    private let ipad = CGSize(width: 1032, height: 1376)
    private let duoOpen = CGSize(width: 951, height: 669)
    private let band = CGRect(x: 463, y: 0, width: 25, height: 669)

    @Test func compactIsAlwaysSingle() {
        for fold in [FoldInfo.flat, .from(active: band, inactive: band)] {
            let arrangement = PaneLayout.arrange(
                size: ipad, sizeClass: .compact, fold: fold, hasSecondary: true, primaryFraction: 0.5
            )
            #expect(arrangement == .single)
        }
    }

    @Test func regularWithSecondarySplits() {
        let arrangement = PaneLayout.arrange(
            size: ipad, sizeClass: .regular, fold: .flat, hasSecondary: true, primaryFraction: 0.5
        )
        #expect(arrangement == .sideBySide(primaryWidth: 515.5, gap: nil))
    }

    @Test func narrowRegularStaysSingle() {
        let arrangement = PaneLayout.arrange(
            size: CGSize(width: 700, height: 1000), sizeClass: .regular, fold: .flat,
            hasSecondary: true, primaryFraction: 0.5
        )
        #expect(arrangement == .single)
    }

    @Test func bookSplitsAtFold() {
        let arrangement = PaneLayout.arrange(
            size: duoOpen, sizeClass: .regular, fold: .from(active: band, inactive: band),
            hasSecondary: true, primaryFraction: 0.5
        )
        #expect(arrangement == .sideBySide(primaryWidth: 463, gap: band))
    }

    @Test func bookWithoutSecondaryIsSingle() {
        let arrangement = PaneLayout.arrange(
            size: duoOpen, sizeClass: .regular, fold: .from(active: band, inactive: band),
            hasSecondary: false, primaryFraction: 0.5
        )
        #expect(arrangement == .single)
    }

    @Test func tabletopUsesTopRegion() {
        let flatBand = CGRect(x: 0, y: 320, width: 951, height: 25)
        let arrangement = PaneLayout.arrange(
            size: duoOpen, sizeClass: .regular, fold: .from(active: flatBand, inactive: flatBand),
            hasSecondary: false, primaryFraction: 0.5
        )
        #expect(arrangement == .tabletop(topHeight: 320, gap: flatBand))
    }

    @Test func tabletopComesBeforeWidthRules() {
        let band = CGRect(x: 0, y: 451, width: 669, height: 25)
        let arrangement = PaneLayout.arrange(
            size: CGSize(width: 669, height: 951), sizeClass: .compact, fold: .from(active: band, inactive: band),
            hasSecondary: false, primaryFraction: 0.5
        )
        #expect(arrangement == .tabletop(topHeight: 451, gap: band))
    }

    @Test func fractionSnapsAndRespectsMinimum() {
        #expect(PaneLayout.snap(0.3, width: 900) == 0.5)
        #expect(PaneLayout.snap(0.3, width: 1376) == 1.0 / 3)
        #expect(PaneLayout.snap(0.7, width: 1376) == 2.0 / 3)
    }

    @Test func foldFromBandShape() {
        #expect(FoldInfo.from(active: band, inactive: band).posture == .book)
        let wide = CGRect(x: 0, y: 320, width: 951, height: 25)
        #expect(FoldInfo.from(active: wide, inactive: wide).posture == .tabletop)
        let flat = FoldInfo.from(active: nil, inactive: band)
        #expect(flat.posture == .flat)
        #expect(flat.division == nil)
        #expect(flat.inactiveDivision == band)
        #expect(FoldInfo.from(active: nil, inactive: nil) == .flat)
    }
}
