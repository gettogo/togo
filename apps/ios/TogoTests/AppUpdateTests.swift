import Foundation
import Testing
@testable import Togo

@MainActor struct AppUpdateTests {
    // Fields captured from Apple's public Togo lookup response, 2026-11-13.
    private let published = Data("""
    {"resultCount":1,"results":[{"bundleId":"app.togo.Togo","minimumOsVersion":"26.0",
    "trackId":6833273877,"version":"1.0.1","trackViewUrl":"https://apps.apple.com/us/app/togo-agent-remote/id6833273877?uo=4"}]}
    """.utf8)

    @Test func aPublishedListingMustBeNewerAndInstallable() throws {
        let parsed = try AppStoreLookup.release(data: published, status: 200)
        let release = try #require(parsed)
        #expect(release.isAvailable(installed: "1.0.0", operatingSystem: "26.0"))
        #expect(!release.isAvailable(installed: "1.0.1", operatingSystem: "26.0"))
        #expect(!release.isAvailable(installed: "1.0.2", operatingSystem: "26.0"))
        #expect(!release.isAvailable(installed: "1.0.0", operatingSystem: "25.9"))
    }

    @Test func versionsCompareNumerically() throws {
        let latest = try #require(AppReleaseVersion("1.10.0"))
        let installed = try #require(AppReleaseVersion("1.9.9"))
        #expect(latest > installed)
        #expect(AppReleaseVersion("1.0") == AppReleaseVersion("1.0.0"))
        #expect(AppReleaseVersion("1.01.0") == AppReleaseVersion("1.1"))
    }

    @Test(arguments: ["", "1..2", "v1.0.2", "1.2-beta", "1.0\n", "１.２", "1.2.3.4", "99999999999999999999999999"])
    func untrustedVersionsCannotBecomeNotices(_ text: String) {
        #expect(AppReleaseVersion(text) == nil)
        #expect(!AppStoreRelease(version: text, minimumOsVersion: "26.0").isAvailable(installed: "1.0.0", operatingSystem: "26.0"))
    }

    @Test func rejectWrongAppsErrorsAndInvalidListings() throws {
        let text = String(decoding: published, as: UTF8.self)
        #expect(throws: (any Error).self) { try AppStoreLookup.release(data: published, status: 503) }
        #expect(throws: (any Error).self) {
            try AppStoreLookup.release(data: Data(text.replacingOccurrences(of: "app.togo.Togo", with: "another.app").utf8), status: 200)
        }
        #expect(throws: (any Error).self) {
            try AppStoreLookup.release(data: Data(text.replacingOccurrences(of: "6833273877", with: "1").utf8), status: 200)
        }
        #expect(throws: (any Error).self) { try AppStoreLookup.release(data: Data("not json".utf8), status: 200) }
        #expect(throws: (any Error).self) {
            try AppStoreLookup.release(data: Data(text.replacingOccurrences(of: "1.0.1", with: "next").utf8), status: 200)
        }
        #expect(try AppStoreLookup.release(data: Data("{\"resultCount\":0,\"results\":[]}".utf8), status: 200) == nil)
    }

    @Test func lookupUsesTheStorefrontAndAPinnedDestination() throws {
        #expect(AppStoreLookup.countryCode("USA") == "US")
        #expect(AppStoreLookup.countryCode("GBR") == "GB")
        #expect(AppStoreLookup.countryCode("DEU") == "DE")
        #expect(AppStoreLookup.countryCode("JPN") == "JP")
        #expect(AppStoreLookup.countryCode("ZZZ") == nil)
        #expect(AppStoreLookup.countryCode("US&country=GB") == nil)
        let request = AppStoreLookup.request(country: "GB")
        let url = try #require(request.url)
        let query = try #require(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems)
        #expect(url.host == "itunes.apple.com")
        #expect(query.contains(URLQueryItem(name: "country", value: "GB")))
        #expect(query.contains(URLQueryItem(name: "id", value: "6833273877")))
        #expect(AppStoreLookup.storeURL.host == "apps.apple.com")
        #expect(AppStoreLookup.storeURL.path.hasSuffix("id6833273877"))
        #expect(request.timeoutInterval == 15)
    }

    @MainActor private final class Probe {
        var time = Date(timeIntervalSince1970: 1_800_000_000)
        var release: AppStoreRelease? = AppStoreRelease(version: "1.0.2", minimumOsVersion: "26.0")
        var calls = 0
        var country = "US"
        var fails = false
        var defaults: UserDefaults
        let suite = "AppUpdateTests.\(UUID())"

        init() { defaults = UserDefaults(suiteName: suite)! }
        func clean() { defaults.removePersistentDomain(forName: suite) }
        func updates(installed: String = "1.0.1") -> AppUpdates {
            AppUpdates(installed: installed, operatingSystem: "26.0", defaults: defaults, now: { self.time },
                       country: { self.country }, lookup: { _ in
                self.calls += 1
                if self.fails { throw AppStoreLookup.Failure.response }
                return self.release
            })
        }
    }

    @Test func successfulChecksAreDailyAndResumeAfterClockChanges() async {
        let probe = Probe()
        defer { probe.clean() }
        let updates = probe.updates()
        await updates.checkIfDue()
        #expect(updates.availableVersion == "1.0.2")
        probe.time += AppUpdates.checkInterval - 1
        await updates.checkIfDue()
        #expect(probe.calls == 1)
        probe.time += 1
        await updates.checkIfDue()
        #expect(probe.calls == 2)
        probe.time -= 60
        await updates.checkIfDue()
        #expect(probe.calls == 3)
    }

    @Test func dismissalSurvivesRelaunchAndANewerReleaseShowsAgain() async {
        let probe = Probe()
        defer { probe.clean() }
        let first = probe.updates()
        await first.checkIfDue()
        first.dismiss()
        #expect(first.availableVersion == nil)
        let relaunched = probe.updates()
        await relaunched.checkIfDue()
        #expect(relaunched.availableVersion == nil)
        #expect(probe.calls == 1)
        probe.time += AppUpdates.checkInterval
        probe.release = AppStoreRelease(version: "1.0.3", minimumOsVersion: "26.0")
        await relaunched.checkIfDue()
        #expect(relaunched.availableVersion == "1.0.3")
    }

    @Test func installingTheOfferedReleaseClearsTheNoticeWithoutAnotherLookup() async {
        let probe = Probe()
        defer { probe.clean() }
        await probe.updates().checkIfDue()
        let upgraded = probe.updates(installed: "1.0.2")
        await upgraded.checkIfDue()
        #expect(upgraded.availableVersion == nil)
        #expect(probe.calls == 1)
        let otherDevice = AppUpdates()
        #expect(otherDevice.availableVersion == nil)
    }

    @Test func failedChecksRetrySoonerThanSuccessfulChecksAndKeepConfirmedNews() async {
        let probe = Probe()
        defer { probe.clean() }
        let updates = probe.updates()
        probe.fails = true
        await updates.checkIfDue()
        #expect(updates.availableVersion == nil)
        probe.fails = false
        probe.time += AppUpdates.retryInterval - 1
        await updates.checkIfDue()
        #expect(probe.calls == 1)
        probe.time += 1
        await updates.checkIfDue()
        #expect(updates.availableVersion == "1.0.2")
        #expect(probe.calls == 2)
        probe.time += AppUpdates.checkInterval
        probe.fails = true
        await updates.checkIfDue()
        #expect(updates.availableVersion == "1.0.2")
        probe.time += AppUpdates.retryInterval
        probe.fails = false
        probe.release = nil
        await updates.checkIfDue()
        #expect(updates.availableVersion == nil)
    }

    @Test func changingStorefrontClearsThePreviousStoresOffer() async {
        let probe = Probe()
        defer { probe.clean() }
        let updates = probe.updates()
        await updates.checkIfDue()
        probe.country = "FR"
        probe.release = nil
        await updates.checkIfDue()
        #expect(updates.availableVersion == nil)
        #expect(probe.calls == 2)
    }

    @Test func anUnknownStorefrontDoesNotFallBackToTheUSListing() async {
        let probe = Probe()
        defer { probe.clean() }
        let updates = AppUpdates(installed: "1.0.1", operatingSystem: "26.0", defaults: probe.defaults,
                                 country: { throw AppStoreLookup.Failure.storefront }, lookup: { _ in
            probe.calls += 1
            return probe.release
        })
        await updates.checkIfDue()
        #expect(probe.calls == 0)
        #expect(updates.availableVersion == nil)
    }

    @Test func cancellingACheckAllowsAnotherWindowToRetry() async {
        let probe = Probe()
        defer { probe.clean() }
        var calls = 0
        let updates = AppUpdates(installed: "1.0.1", operatingSystem: "26.0", defaults: probe.defaults,
                                 country: { "US" }, lookup: { _ in
            calls += 1
            if calls == 1 { throw CancellationError() }
            return probe.release
        })
        await updates.checkIfDue()
        await updates.checkIfDue()
        #expect(calls == 2)
        #expect(updates.availableVersion == "1.0.2")
    }

    @Test func simultaneousWindowsShareOneCheck() async {
        let probe = Probe()
        defer { probe.clean() }
        var finish: CheckedContinuation<AppStoreRelease?, Never>?
        var calls = 0
        let updates = AppUpdates(installed: "1.0.1", operatingSystem: "26.0", defaults: probe.defaults,
                                 country: { "US" }, lookup: { _ in
            calls += 1
            return await withCheckedContinuation { finish = $0 }
        })
        let first = Task { await updates.checkIfDue() }
        while finish == nil { await Task.yield() }
        await updates.checkIfDue()
        #expect(calls == 1)
        finish?.resume(returning: probe.release)
        await first.value
        #expect(updates.availableVersion == "1.0.2")
    }

    @Test func aCancelledNetworkRequestDoesNotPostponeTheNextWindowsCheck() async {
        let probe = Probe()
        defer { probe.clean() }
        var calls = 0
        let updates = AppUpdates(installed: "1.0.1", operatingSystem: "26.0", defaults: probe.defaults,
                                 country: { "US" }, lookup: { _ in
            calls += 1
            if calls == 1 {
                do { try await Task.sleep(for: .seconds(30)) }
                catch { throw URLError(.cancelled) }
            }
            return probe.release
        })
        let first = Task { await updates.checkIfDue() }
        while calls == 0 { await Task.yield() }
        first.cancel()
        await first.value
        await updates.checkIfDue()
        #expect(calls == 2)
        #expect(updates.availableVersion == "1.0.2")
    }
}
