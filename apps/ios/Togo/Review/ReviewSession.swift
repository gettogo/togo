import SwiftUI
import TogoKit

/// What an open review holds: the screens open in it and the models behind them, among them a
/// commit message being written. Kept by the store, so a fold or unfold that moves the review to
/// another presenter, or rebuilds its screens, picks it up where it was.
@MainActor @Observable final class ReviewSession {
    let subject: ReviewSubject
    var navigation: ReviewNavigation
    let changes: ChangesModel
    let history: HistoryModel
    let files: FilesModel
    /// One per review: the ports screen stops its listener and forwards when it leaves.
    let ports: PortsModel
    /// Whether the review opened as a sheet (regular width) or a full-screen cover. Kept until it
    /// closes, so folding or unfolding does not swap one presentation for the other.
    @ObservationIgnored var regular: Bool?
    /// The models behind the screens open now, by route. A fold or unfold moves an open diff or
    /// file between the detail column and the stack, which builds its screen again; the same
    /// model comes back with it, so the file picked in a commit, the loaded text and any find
    /// stay. Routes no longer open are let go.
    @ObservationIgnored private var screens: [ReviewRoute: AnyObject] = [:]

    init(
        subject: ReviewSubject, services: any ReviewServices, tab: ReviewTab, machine: String,
        navigation: ReviewNavigation? = nil, regular: Bool? = nil
    ) {
        self.subject = subject
        let tab = ReviewRoot.tabs(for: subject).contains(tab) ? tab : .changes
        self.navigation = navigation ?? ReviewNavigation(tab: tab)
        changes = ChangesModel(subject: subject, services: services, machine: machine)
        history = HistoryModel(subject: subject, services: services)
        files = FilesModel(subject: subject, services: services, path: "", ref: nil)
        ports = PortsModel(subject: subject, services: services)
        self.regular = regular
    }

    /// The model for `route`'s screen: the one already made while the route stays open, else
    /// a new one from `make`.
    func screen<Model: AnyObject>(for route: ReviewRoute, make: () -> Model) -> Model {
        let open = Set(navigation.path + [navigation.detail].compactMap(\.self))
        screens = screens.filter { open.contains($0.key) }
        if let kept = screens[route] as? Model { return kept }
        let made = make()
        if open.contains(route) { screens[route] = made }
        return made
    }
}

extension AppStore {
    /// The open review for `subject`, made the first time it is shown. `regular` is how it is
    /// being presented; a review already open keeps the presentation it opened with.
    func keptReview(
        for subject: ReviewSubject, services: any ReviewServices, tab: ReviewTab, machine: String, regular: Bool
    ) -> ReviewSession {
        if let review = openReview, review.subject == subject {
            if review.regular == nil { review.regular = regular }
            return review
        }
        let review = ReviewSession(subject: subject, services: services, tab: tab, machine: machine, regular: regular)
        openReview = review
        return review
    }

    /// The review closed: the next one starts from its first screen.
    func reviewClosed() {
        openReview = nil
    }
}

/// Which presentation the review uses.
enum ReviewPresentation {
    /// A sheet in a regular-width window and a full-screen cover in a compact one, chosen when
    /// the review opens. `kept` is the choice made then, which a fold or unfold leaves as it is.
    static func regular(kept: Bool?, sizeClass: UserInterfaceSizeClass?, width: CGFloat?) -> Bool {
        kept ?? (ReviewRoot.layout(for: sizeClass, width: width) == .split)
    }
}
