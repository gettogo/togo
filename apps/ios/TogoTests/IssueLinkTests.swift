import Foundation
import Testing
@testable import Togo

struct IssueLinkTests {
    private func query(_ url: URL, _ name: String) -> String? {
        URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == name }?.value
    }

    private let secrets = [
        "mac-mini", "tail1234", "100.101.5.7", "jamie", "/Users/jamie/client-portal", "Studio Secret", "fix the login bug",
        "ssh-ed25519", "build.example.com",
    ]

    @Test func issueURLHasNoPersonalData() throws {
        let url = IssueLink.url(
            app: "1.0 (42)", os: "27.2", device: "iPhone",
            helpers: ["0.3.0", "mac-mini/0.2.4", "jamie@100.101.5.7", "/Users/jamie/client-portal", "Studio Secret", "0.3.0 fix the login bug"]
        )
        let text = url.absoluteString
        let body = try #require(query(url, "versions"))
        for secret in secrets {
            #expect(!text.contains(secret), "url holds \(secret)")
            #expect(!body.contains(secret), "body holds \(secret)")
        }
        #expect(!text.contains("/Users"))
        #expect(body.contains("App 1.0 (42)"))
        #expect(body.contains("27.2"))
        #expect(body.contains("0.3.0"))
    }

    @Test func urlIsTheIssueFormOnTheRepository() throws {
        let url = IssueLink.url(app: "1.0", os: "27.2", device: "iPhone", helpers: [])
        let parts = try #require(URLComponents(url: url, resolvingAgainstBaseURL: false))
        #expect(parts.scheme == "https")
        #expect(parts.host == "github.com")
        #expect(parts.path == "/gettogo/togo/issues/new")
        #expect(query(url, "template") == "bug.yml")
        #expect(query(url, "title") == "")
        #expect(url.absoluteString.hasPrefix("https://github.com/gettogo/togo/issues/new?template=bug.yml&title=&versions="))
    }

    @Test func bodyIsPercentEncoded() throws {
        let url = IssueLink.url(app: "1.0", os: "27.2", device: "iPhone", helpers: ["0.3.0"])
        let tail = try #require(url.absoluteString.components(separatedBy: "&versions=").last)
        #expect(!tail.contains(" "))
        #expect(!tail.contains("&"))
        #expect(!tail.contains("#"))
        #expect(!tail.contains("+"))
        #expect(tail.contains("%2C%20"))
    }

    @Test func bodyCapped() throws {
        let long = String(repeating: "9", count: 5000)
        let url = IssueLink.url(
            app: long, os: long, device: "iPad", helpers: Array(repeating: long, count: 500)
        )
        let body = try #require(query(url, "versions"))
        #expect(body.count <= 6000)
        #expect(body.contains("iPad"))
    }

    @Test func helperVersionsAreCleanedAndLimited() throws {
        let helpers = (0..<20).map { "0.\($0).0" }
        #expect(IssueLink.cleanHelpers(helpers) == (0..<8).map { "0.\($0).0" })
        #expect(IssueLink.cleanHelpers(["1" + String(repeating: "9", count: 99)]) == ["1" + String(repeating: "9", count: 31)])
        #expect(IssueLink.cleanHelpers(["v1.2.3-rc.1", "1.0 beta", "///", "", "mac-mini", "1.0;rm", "1.0-mac-mini", "jamie"]) == ["v1.2.3-rc.1", "1.0", "1.0"])
    }

    @Test func deviceFamilyOnly() throws {
        for (given, shown) in [("iPhone", "iPhone"), ("iPad", "iPad"), ("iPhone19,4", "iPhone"), ("iPad16,5", "iPad"), ("Jamie's iPhone", "Unknown")] {
            let body = try #require(query(IssueLink.url(app: "1.0", os: "27.2", device: given, helpers: []), "versions"))
            #expect(body.contains(", \(shown), "), "\(given)")
            #expect(!body.contains("19,4"))
            #expect(!body.contains("Jamie"))
        }
    }

    @Test func personalTextInAppOrOSBecomesUnknown() throws {
        let url = IssueLink.url(app: "Jamie's build", os: "mac-mini", device: "iPhone", helpers: [])
        let body = try #require(query(url, "versions"))
        #expect(body == "App unknown, iOS unknown, iPhone, helper none")
    }

    @Test func familyFollowsTheInterfaceIdiom() {
        #expect(IssueLink.family(isPad: true) == "iPad")
        #expect(IssueLink.family(isPad: false) == "iPhone")
    }
}
