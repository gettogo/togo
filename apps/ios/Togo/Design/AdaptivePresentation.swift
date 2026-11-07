import SwiftUI
import TogoKit

/// What is being shown, which is what decides how it is presented. Never the device: the same
/// content gets the same presentation wherever the window has the same room (HIG layout,
/// `I2`, Duo `D2`).
enum PresentationContent: String, CaseIterable, Sendable {
    /// About seven options or fewer, no search: Effort, Mode, a sort order.
    case quickChoice
    /// A few fields or a short panel: a setting with a value, a step's details.
    case smallForm
    /// Many rows to browse, search or filter: a Picker with groups, Resume, MCP, the commands.
    case browse
    /// One item's detail or edit, opened from a list that is already on screen.
    case detail
    /// A question before something that cannot be undone.
    case destructive
    /// Context kept beside the main content while the user works: mission control.
    case sideContext
}

/// How content is put on screen. Every presentation is a system one, so it adopts Liquid Glass,
/// moves its bars vertically on iPhone Duo and avoids the fold by itself (`L1`, `D11`).
enum PresentationStyle: Equatable, Sendable {
    /// A system `Menu` anchored to its control.
    case menu
    /// A popover anchored to its control; only where the width is regular.
    case popover
    /// A system confirmation dialog (an action sheet on compact, a popover or alert on regular).
    case confirmationDialog
    /// A resizable sheet starting at the medium detent, with a grabber (`C8`).
    case mediumSheet
    /// A centred form sheet on a regular width.
    case formSheet
    /// A large page sheet on a regular width: room for a split view inside.
    case pageSheet
    /// A full-height sheet on a compact width.
    case fullHeightSheet
    /// Pushed onto the navigation stack already on screen; never another sheet (`C8`).
    case push
    /// A trailing inspector column beside the content.
    case inspector

    var isSheet: Bool {
        switch self {
        case .mediumSheet, .formSheet, .pageSheet, .fullHeightSheet: true
        default: false
        }
    }
}

/// The room a presentation has, from the size class and the measured container. A regular size
/// class in a narrow container (a Duo's Split View half, a slim iPad window) counts as compact.
enum PresentationWidth: Equatable, Sendable {
    case compact
    case regular
}

/// How a browse list lays itself out inside its sheet.
enum BrowseLayout: Equatable, Sendable {
    /// `NavigationSplitView`: groups and filters in a sidebar, the list, an optional detail.
    case split
    /// `NavigationStack`: the groups as sections, filters in a menu, the detail pushed.
    case stack
}

/// What a screen opened from inside a browse sheet does.
enum PresentationOpening: Equatable, Sendable {
    /// Pushed in the same stack, with Back.
    case push
    /// The sheet hands over to the screen's own sheet, which can be a split view: one sheet at
    /// a time, never one stacked on another (`C8`).
    case handOver
}

/// The app's one presentation policy: content class times width, never device. Each row
/// follows the HIG and the Duo rules.
enum AdaptivePresentation {
    /// Narrower than this is compact whatever the size class says: two comfortable phone
    /// columns do not fit (a Duo's Split View half is about 475 pt, an outer display 466 pt).
    static let regularMinimumWidth: CGFloat = 600
    /// The narrowest sheet that shows a sidebar beside its list: an unfolded Duo held tall
    /// (669 pt) and every iPad page sheet qualify.
    static let splitMinimumWidth: CGFloat = 640

    static func width(sizeClass: UserInterfaceSizeClass?, containerWidth: CGFloat?) -> PresentationWidth {
        guard sizeClass == .regular else { return .compact }
        if let containerWidth, containerWidth > 0, containerWidth < regularMinimumWidth { return .compact }
        return .regular
    }

    /// The presentation for `content`. `anchored`: the presenter is the control the user
    /// tapped, so a popover or menu can point at it. `hostsInspector`: the presenter sits beside
    /// main content an inspector can share the window with.
    static func style(
        for content: PresentationContent, sizeClass: UserInterfaceSizeClass?, containerWidth: CGFloat? = nil,
        anchored: Bool = false, hostsInspector: Bool = false
    ) -> PresentationStyle {
        let regular = width(sizeClass: sizeClass, containerWidth: containerWidth) == .regular
        switch content {
        case .quickChoice:
            return .menu
        case .smallForm:
            if regular { return anchored ? .popover : .formSheet }
            return .mediumSheet
        case .browse:
            return regular ? .pageSheet : .fullHeightSheet
        case .detail:
            return .push
        case .destructive:
            return .confirmationDialog
        case .sideContext:
            return regular && hostsInspector ? .inspector : .mediumSheet
        }
    }

    /// The layout of a browse list in its own sheet. A split view needs room, more than one
    /// group to put in its sidebar, and to own the navigation: a list pushed into another stack
    /// stays a stack (a split view never nests in a stack).
    static func browseLayout(
        sizeClass: UserInterfaceSizeClass?, containerWidth: CGFloat?, groups: Int, ownsNavigation: Bool
    ) -> BrowseLayout {
        guard ownsNavigation, groups > 1, width(sizeClass: sizeClass, containerWidth: containerWidth) == .regular
        else { return .stack }
        if let containerWidth, containerWidth > 0, containerWidth < splitMinimumWidth { return .stack }
        return .split
    }

    /// A screen opened from inside a browse sheet: a browse screen hands over to its own sheet
    /// where that sheet would be a split view; anything else, and every compact width, pushes.
    static func opening(
        _ content: PresentationContent, sizeClass: UserInterfaceSizeClass?, containerWidth: CGFloat?
    ) -> PresentationOpening {
        content == .browse && width(sizeClass: sizeClass, containerWidth: containerWidth) == .regular ? .handOver : .push
    }

    /// A harness screen's class, from the primitive the descriptor names before it loads.
    static func content(for primitive: HarnessPrimitive?) -> PresentationContent {
        guard let primitive else { return .browse }
        if [.settings, .status, .confirm].contains(primitive) { return .smallForm }
        if primitive == .detail { return .detail }
        // Pickers, live lists, checklists and primitives this build does not know yet.
        return .browse
    }

    /// A harness screen's class by id, from the session's descriptor.
    static func content(forScreen id: String, in descriptor: HarnessDescriptor?) -> PresentationContent {
        content(for: descriptor?.screens.first { $0.id == id }?.primitive)
    }
}

extension EnvironmentValues {
    /// The window's (or Split View half's) width, measured once at the root, so a presenter
    /// deep in the tree decides by the room the presentation will have. nil before it is read.
    @Entry var windowWidth: CGFloat?
}
