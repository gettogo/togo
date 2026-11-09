import Foundation
import Testing
@testable import TogoPush

struct WatchLinkTests {
    @Test func linksRoundTrip() throws {
        for link in [WatchLink.waiting, .request("r_1-a.B"), .sessions] {
            #expect(WatchLink(url: link.url) == link)
        }
        #expect(WatchLink.request("r_1").url.absoluteString == "togo://waiting/r_1")
        #expect(WatchLink.waiting.url.absoluteString == "togo://waiting")
        #expect(WatchLink.sessions.url.absoluteString == "togo://sessions")
    }

    /// Only plain identifiers are taken from a link; anything else is not a Watch link.
    @Test func rejectsWhatIsNotAWatchLink() throws {
        for string in [
            "togo://session/04?host=studio", "togo://new", "https://waiting/r_1", "togo://waiting/a/b",
            "togo://waiting/%2E%2E%2Fx", "togo://sessions/x", "togo://waiting/r%20_1",
        ] {
            #expect(WatchLink(url: try #require(URL(string: string))) == nil, "\(string)")
        }
        #expect(!WatchLink.isRequestID(""))
        #expect(!WatchLink.isRequestID(String(repeating: "a", count: 129)))
        #expect(!WatchLink.isRequestID("é"))
        #expect(!WatchLink.isRequestID(".."))
        #expect(WatchLink(url: try #require(URL(string: "togo://waiting/.."))) == nil)
        // An id that is not plain falls back to the oldest request rather than a bad link.
        #expect(WatchLink.request("a/b").url.absoluteString == "togo://waiting")
    }

    @Test func inboxGivesAFreshLinkOnce() throws {
        let defaults = try #require(UserDefaults(suiteName: "WatchLinkTests-\(UUID().uuidString)"))
        let now = Date(timeIntervalSince1970: 1_000_000)
        WatchLinkInbox.post(.request("r_1"), to: defaults, now: now)
        #expect(WatchLinkInbox.take(from: defaults, now: now.addingTimeInterval(2)) == .request("r_1"))
        #expect(WatchLinkInbox.take(from: defaults, now: now.addingTimeInterval(3)) == nil)
    }

    @Test func inboxDropsAStaleLink() throws {
        let defaults = try #require(UserDefaults(suiteName: "WatchLinkTests-\(UUID().uuidString)"))
        let now = Date(timeIntervalSince1970: 1_000_000)
        WatchLinkInbox.post(.waiting, to: defaults, now: now)
        #expect(WatchLinkInbox.take(from: defaults, now: now.addingTimeInterval(WatchLinkInbox.maxAge + 1)) == nil)
        #expect(WatchLinkInbox.take(from: nil) == nil)
    }
}
