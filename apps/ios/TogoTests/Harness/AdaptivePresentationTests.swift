import Foundation
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// The one presentation policy: content class × size class × container width → presentation.
/// Widths are the rooms the app meets, named by room, never by device: an outer display or a
/// phone (390–466 pt, compact), a Split View half of an unfolded display (about 475 pt, which
/// can report regular), an unfolded display held tall (669 pt) or wide (951 pt), an iPad page
/// sheet (700 pt and more).
@MainActor
struct AdaptivePresentationTests {
    private func style(
        _ content: PresentationContent, _ sizeClass: UserInterfaceSizeClass?, _ width: CGFloat?,
        anchored: Bool = false, inspector: Bool = false
    ) -> PresentationStyle {
        AdaptivePresentation.style(
            for: content, sizeClass: sizeClass, containerWidth: width, anchored: anchored, hostsInspector: inspector
        )
    }

    // MARK: Width

    @Test func aRegularSizeClassInANarrowContainerCountsAsCompact() {
        #expect(AdaptivePresentation.width(sizeClass: .regular, containerWidth: 475) == .compact)
        #expect(AdaptivePresentation.width(sizeClass: .regular, containerWidth: 669) == .regular)
        #expect(AdaptivePresentation.width(sizeClass: .regular, containerWidth: nil) == .regular)
        #expect(AdaptivePresentation.width(sizeClass: .regular, containerWidth: 0) == .regular)
        #expect(AdaptivePresentation.width(sizeClass: .compact, containerWidth: 1194) == .compact)
        #expect(AdaptivePresentation.width(sizeClass: nil, containerWidth: 1194) == .compact)
    }

    // MARK: Style per class

    @Test func quickChoicesAreAlwaysAMenu() {
        for size in [UserInterfaceSizeClass.compact, .regular] {
            #expect(style(.quickChoice, size, 951, anchored: true) == .menu)
            #expect(style(.quickChoice, size, 390) == .menu)
        }
    }

    @Test func smallFormsArePopoversWhenAnchoredOnRegularAndMediumSheetsOnCompact() {
        #expect(style(.smallForm, .regular, 951, anchored: true) == .popover)
        #expect(style(.smallForm, .regular, 951) == .formSheet)
        #expect(style(.smallForm, .compact, 390, anchored: true) == .mediumSheet)
        // A Split View half that reports regular is still too narrow for a popover.
        #expect(style(.smallForm, .regular, 475, anchored: true) == .mediumSheet)
    }

    @Test func largeBrowseListsAreSheetsNeverPopovers() {
        #expect(style(.browse, .regular, 1194, anchored: true) == .pageSheet)
        #expect(style(.browse, .regular, 951, anchored: true) == .pageSheet)
        #expect(style(.browse, .regular, 669, anchored: true) == .pageSheet)
        #expect(style(.browse, .regular, 475, anchored: true) == .fullHeightSheet)
        #expect(style(.browse, .compact, 466, anchored: true) == .fullHeightSheet)
        for width: CGFloat in [390, 466, 475, 669, 951, 1194] {
            for size in [UserInterfaceSizeClass.compact, .regular] {
                #expect(style(.browse, size, width, anchored: true) != .popover)
            }
        }
    }

    @Test func detailsPushAndDestructiveQuestionsAskInTheSystemDialog() {
        for size in [UserInterfaceSizeClass.compact, .regular] {
            #expect(style(.detail, size, 951) == .push)
            #expect(style(.destructive, size, 951, anchored: true) == .confirmationDialog)
        }
    }

    @Test func sideContextIsAnInspectorOnlyBesideContentWithRoom() {
        #expect(style(.sideContext, .regular, 951, inspector: true) == .inspector)
        #expect(style(.sideContext, .regular, 951) == .mediumSheet)
        #expect(style(.sideContext, .regular, 475, inspector: true) == .mediumSheet)
        #expect(style(.sideContext, .compact, 390, inspector: true) == .mediumSheet)
    }

    @Test func sheetStylesAreSheets() {
        #expect(PresentationStyle.mediumSheet.isSheet)
        #expect(PresentationStyle.formSheet.isSheet)
        #expect(PresentationStyle.pageSheet.isSheet)
        #expect(PresentationStyle.fullHeightSheet.isSheet)
        #expect(!PresentationStyle.popover.isSheet)
        #expect(!PresentationStyle.inspector.isSheet)
        #expect(!PresentationStyle.push.isSheet)
    }

    /// The guide fills a compact window and floats as a page over a wide one, never stretched.
    @Test func aWalkthroughIsACoverWhenCompactAndAPageWhenWide() {
        #expect(style(.walkthrough, .compact, 390) == .fullScreenCover)
        #expect(style(.walkthrough, .regular, 475) == .fullScreenCover)
        #expect(style(.walkthrough, .regular, 669) == .pageSheet)
        #expect(style(.walkthrough, .regular, 1194) == .pageSheet)
        #expect(PresentationCoordinator.kind(of: .fullScreenCover) == .cover)
        #expect(PresentationCoordinator.kind(of: .pageSheet) == .sheet)
    }

    // MARK: Browse layout

    @Test func aBrowseListIsASplitViewOnlyAsTheWideRootOfItsSheetWithGroups() {
        func layout(_ size: UserInterfaceSizeClass?, _ width: CGFloat?, groups: Int = 12, owns: Bool = true) -> BrowseLayout {
            AdaptivePresentation.browseLayout(sizeClass: size, containerWidth: width, groups: groups, ownsNavigation: owns)
        }
        #expect(layout(.regular, 951) == .split)
        #expect(layout(.regular, 669) == .split)
        #expect(layout(.regular, 1194) == .split)
        #expect(layout(.regular, 620) == .stack)
        #expect(layout(.regular, 475) == .stack)
        #expect(layout(.compact, 951) == .stack)
        // One group has nothing for a sidebar; a list pushed into another stack stays a stack.
        #expect(layout(.regular, 951, groups: 1) == .stack)
        #expect(layout(.regular, 951, owns: false) == .stack)
    }

    @Test func aLargeListOpenedFromTheWidePaletteHandsOverElsePushes() {
        #expect(AdaptivePresentation.opening(.browse, sizeClass: .regular, containerWidth: 951) == .handOver)
        #expect(AdaptivePresentation.opening(.browse, sizeClass: .regular, containerWidth: 475) == .push)
        #expect(AdaptivePresentation.opening(.browse, sizeClass: .compact, containerWidth: 390) == .push)
        #expect(AdaptivePresentation.opening(.smallForm, sizeClass: .regular, containerWidth: 951) == .push)
        #expect(AdaptivePresentation.opening(.detail, sizeClass: .regular, containerWidth: 951) == .push)
    }

    // MARK: Screen classes

    @Test func harnessScreensAreClassedByTheirPrimitiveBeforeTheyLoad() {
        #expect(AdaptivePresentation.content(for: .picker) == .browse)
        #expect(AdaptivePresentation.content(for: .liveList) == .browse)
        #expect(AdaptivePresentation.content(for: .checklist) == .browse)
        #expect(AdaptivePresentation.content(for: .settings) == .smallForm)
        #expect(AdaptivePresentation.content(for: .status) == .smallForm)
        #expect(AdaptivePresentation.content(for: .confirm) == .smallForm)
        #expect(AdaptivePresentation.content(for: .detail) == .detail)
        #expect(AdaptivePresentation.content(for: nil) == .browse)
        #expect(AdaptivePresentation.content(for: HarnessPrimitive(rawValue: "future")) == .browse)

        let descriptor = HarnessDescriptor(screens: [
            .init(id: "model", primitive: .picker, title: "Model", section: "composer"),
            .init(id: "effort", primitive: .settings, title: "Effort", section: "composer"),
        ])
        #expect(AdaptivePresentation.content(forScreen: "model", in: descriptor) == .browse)
        #expect(AdaptivePresentation.content(forScreen: "effort", in: descriptor) == .smallForm)
        #expect(AdaptivePresentation.content(forScreen: "gone", in: descriptor) == .browse)
    }

    @Test func missionControlFollowsThePolicy() {
        #expect(MissionPresentation.choose(.regular, windowWidth: 951) == .inspector)
        #expect(MissionPresentation.choose(.regular, windowWidth: 475) == .sheet)
        #expect(MissionPresentation.choose(.compact, windowWidth: 951) == .sheet)
    }
}
