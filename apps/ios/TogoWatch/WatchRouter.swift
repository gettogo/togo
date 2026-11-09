import Observation
import TogoPush

/// Where the Watch app is: the page on screen, the request a complication, control or
/// notification asked for, and what is pushed over the pages. Kept apart from `WatchStore`,
/// which holds what the screens show.
@MainActor @Observable final class WatchRouter {
    /// The vertical pages, in order: Waiting first, Sessions a swipe below.
    enum Page: Hashable { case waiting, sessions }

    var page: Page = .waiting
    /// The waiting request to show first. One that is no longer waiting falls back to the oldest.
    var focus: String?
    var path: [WatchRoute] = []

    /// Lands on the exact screen a surface was about, from wherever the app was.
    func open(_ link: WatchLink) {
        path = []
        switch link {
        case .waiting:
            page = .waiting
            focus = nil
        case .request(let id):
            page = .waiting
            focus = id
        case .sessions:
            page = .sessions
        }
    }
}

/// A screen pushed over the pages.
enum WatchRoute: Hashable {
    case reply(ReplyRoute)
}
