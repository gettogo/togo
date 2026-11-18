import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct LinkIdentityTests {
    @Test func bannerSuppressionUsesStableIdentityRatherThanADuplicateName() {
        let router = NotificationRouter()
        router.frontSession = { (bib: "04", machine: "Studio") }
        router.frontIdentity = { TabID(hostID: Samples.studio, sessionID: "s-live") }
        let same = "togo://session/04?host=old&hostID=\(Samples.studio)&sessionID=s-live"
        let other = "togo://session/04?host=Studio&hostID=\(Samples.build)&sessionID=s-live"
        #expect(router.presentation(for: same) == [.list])
        #expect(router.presentation(for: other) == [.banner, .list, .sound])
    }

    @Test func stableIdentityWinsOverRenamedOrDuplicateDisplayNames() {
        let services = FakeAppServices()
        services.hosts[1].name = "Studio"
        var state = Samples.controlCenter
        state.machines[1].reachability = .online
        state.machines[1].sessions = [Samples.row(hostID: Samples.build, bib: "01", name: "second", cwd: "/srv")]
        let store = AppStore(services: services, controlCenter: state)
        store.open(.session(bib: "01", host: "old name", hostID: Samples.build))
        #expect(store.workspace.selected == TabID(hostID: Samples.build, sessionID: "s-01"))
    }

    @Test func anAmbiguousLegacyBibDoesNotPickTheFirstMachine() {
        var state = Samples.controlCenter
        state.machines[1].reachability = .online
        state.machines[1].sessions = [Samples.row(hostID: Samples.build, bib: "01", name: "second", cwd: "/srv")]
        let store = AppStore(services: FakeAppServices(), controlCenter: state)
        store.open(.session(bib: "01", host: nil))
        #expect(store.workspace.tabs.isEmpty)
        #expect(store.notice?.text == AppStore.ambiguousLinkNotice)
        #expect(store.pendingLink == nil)
    }

    @Test func missingSessionIdentityNeverFallsBackToAReusedBib() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(.session(bib: "01", host: "Studio", hostID: Samples.studio, sessionID: "s-old"))
        #expect(store.workspace.tabs.isEmpty)
        #expect(store.notice?.text == AppStore.endedNotice("01"))
    }

    @Test func anOldTimerCannotExpireARepeatedTap() {
        let store = AppStore(services: FakeAppServices())
        let link = DeepLink.session(bib: "01", host: "Studio")
        store.open(link)
        let first = store.linkGeneration
        store.open(link)
        let second = store.linkGeneration
        #expect(first != second)
        store.expirePendingLink(generation: first)
        #expect(store.pendingLink == link)
        #expect(store.notice == nil)
        store.expirePendingLink(generation: second)
        #expect(store.pendingLink == nil)
        #expect(store.notice?.text.contains("connecting") == true)
    }

    @Test func aColdNewSessionLinkRetainsItsMachineFolderAndPreset() {
        let services = FakeAppServices(hosts: [])
        services.hostsLoaded = false
        let store = AppStore(services: services)
        let preset = UUID()
        store.open(.newSession(host: "old name", folder: "/srv", preset: preset, hostID: Samples.studio))
        #expect(store.sheet == nil)
        #expect(store.pendingLink != nil)
        services.hosts = Samples.hosts
        services.hostsLoaded = true
        store.resolvePendingLink()
        #expect(store.sheet == .newSession(Samples.studio))
        #expect(store.takeNewSessionPrefill(for: Samples.studio) == NewSessionPrefill(hostID: Samples.studio, folder: "/srv", presetID: preset))
    }

    @Test func explicitUnknownMachineNeverFallsBackToTheNamedOne() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(.session(bib: "01", host: "Studio", hostID: UUID()))
        #expect(store.workspace.tabs.isEmpty)
        #expect(store.notice != nil)
    }

    @Test func stableLinkParsingRejectsMalformedIdentityRatherThanFallingBack() throws {
        let id = UUID()
        let url = try #require(URL(string: "togo://session/01?host=old&hostID=\(id)&sessionID=s_exact"))
        #expect(DeepLink(url: url) == .session(bib: "01", host: "old", hostID: id, sessionID: "s_exact"))
        for text in ["togo://session/01?hostID=broken", "togo://session/01?sessionID=", "togo://session/01?sessionID=a%0Ab"] {
            #expect(DeepLink(url: try #require(URL(string: text))) == nil)
        }
    }
}
