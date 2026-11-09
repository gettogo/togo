import Foundation
import Testing
import TogoKit
import UserNotifications
@testable import Togo

/// Each window has its own store over one set of services.
@MainActor struct WindowsTests {
    private let services = FakeAppServices()

    private func snapshot(_ host: HostID, bib: String = "01", at offset: TimeInterval = 0) -> HostViewSnapshot {
        HostViewSnapshot(
            hostID: host, reachability: .online, helper: .present(version: "0.3.0", live: true),
            sessions: [Samples.row(hostID: host, bib: bib, name: "api", cwd: "/srv")], requests: [],
            at: Samples.now.addingTimeInterval(offset)
        )
    }

    private func tab(_ session: String) -> TabID {
        TabID(hostID: Samples.studio, sessionID: session)
    }

    @Test func snapshotsFanOutToEveryWindow() async {
        var first = services.snapshots().makeAsyncIterator()
        var second = services.snapshots().makeAsyncIterator()
        let sent = snapshot(Samples.studio)
        services.publish(sent)
        #expect(await first.next() == sent)
        #expect(await second.next() == sent)
    }

    @Test func lateSubscriberGetsLatestSnapshot() async {
        let other = HostID()
        services.publish(snapshot(Samples.studio, bib: "01"))
        services.publish(snapshot(other, bib: "02"))
        services.publish(snapshot(Samples.studio, bib: "03", at: 5))
        var late = services.snapshots().makeAsyncIterator()
        #expect(await late.next() == snapshot(Samples.studio, bib: "03", at: 5))
        #expect(await late.next() == snapshot(other, bib: "02"))
        let next = snapshot(other, bib: "04", at: 9)
        services.publish(next)
        #expect(await late.next() == next)
    }

    @Test func storesInTwoWindowsBothApplySnapshots() async {
        let composition = AppComposition(services: services)
        let a = composition.makeStore(window: WindowID(), restoring: nil)
        let b = composition.makeStore(window: WindowID(), restoring: nil)
        services.publish(snapshot(Samples.studio))
        for _ in 0..<200 where a.controlCenter.machines.isEmpty || b.controlCenter.machines.isEmpty {
            await Task.yield()
        }
        #expect(a.controlCenter.machines.map(\.id) == [Samples.studio])
        #expect(b.controlCenter.machines.map(\.id) == [Samples.studio])
    }

    @Test func restorationRoundTrips() throws {
        let composition = AppComposition(services: services)
        let store = composition.makeStore(window: WindowID(), restoring: nil)
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        store.openBeside(tab("s-02"))
        let saved = WindowRestoration(store)
        #expect(saved == WindowRestoration(
            tabs: [tab("s-01"), tab("s-04"), tab("s-02")], selected: tab("s-04"), secondary: tab("s-02"), workspaceOpen: true,
            router: Router(workspaceOpen: true)
        ))

        let data = try JSONEncoder().encode(saved)
        let decoded = try JSONDecoder().decode(WindowRestoration.self, from: data)
        #expect(decoded == saved)
        let restored = composition.makeStore(window: WindowID(), restoring: decoded)
        #expect(WindowRestoration(restored) == saved)
    }

    /// The window comes back to the same place: what was pushed, the status display and the
    /// sidebar the user hid. A ⌘K in flight is not saved.
    @Test func restorationKeepsThePlace() throws {
        let composition = AppComposition(services: services)
        let store = composition.makeStore(window: WindowID(), restoring: nil)
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.showArchive()
        store.monitorShown = true
        store.sidebar.hidden = true
        store.sidebar.searchRequests = 3
        let data = try JSONEncoder().encode(WindowRestoration(store))
        let decoded = try JSONDecoder().decode(WindowRestoration.self, from: data)
        let restored = composition.makeStore(window: WindowID(), restoring: decoded)
        #expect(restored.path == [.archive])
        #expect(restored.monitorShown)
        #expect(restored.sidebar.isHidden)
        #expect(restored.sidebar.searchRequests == 0)
        #expect(restored.workspaceOpen)
    }

    @Test func restorationDropsWhatNoLongerFits() {
        let composition = AppComposition(services: services)
        let gone = TabID(hostID: HostID(), sessionID: "s-09")
        let store = composition.makeStore(window: WindowID(), restoring: WindowRestoration(
            tabs: [tab("s-01"), tab("s-01")], selected: gone, secondary: tab("s-01"), workspaceOpen: true
        ))
        #expect(store.workspace.tabs.map(\.id) == [tab("s-01")])
        #expect(store.workspace.selected == tab("s-01"))
        #expect(store.workspace.secondary == nil)
        #expect(store.workspaceOpen)

        let empty = composition.makeStore(window: WindowID(), restoring: WindowRestoration(
            tabs: [], selected: nil, secondary: nil, workspaceOpen: true
        ))
        #expect(!empty.workspaceOpen)
    }

    @Test func deepLinkGoesToActiveWindow() {
        let composition = AppComposition(services: services)
        let left = WindowID(), right = WindowID()
        let a = composition.makeStore(window: left, restoring: nil)
        let b = composition.makeStore(window: right, restoring: nil)
        #expect(composition.activeStore === a)

        composition.activate(right)
        #expect(composition.activeStore === b)
        let link = DeepLink.session(bib: "01", host: "Studio")
        composition.open(link)
        #expect(b.pendingLink == link)
        #expect(a.pendingLink == nil)

        // The window that was in front closes: links go to one still open.
        composition.release(window: right)
        #expect(composition.activeStore === a)
    }

    @Test func notificationTapGoesToActiveWindow() async {
        let composition = AppComposition(services: services)
        let a = composition.makeStore(window: WindowID(), restoring: nil)
        let right = WindowID()
        let b = composition.makeStore(window: right, restoring: nil)
        let router = NotificationRouter()
        router.deliver { composition.activeStore }
        composition.activate(right)

        await router.handle(NotificationResponse(
            actionIdentifier: UNNotificationDefaultActionIdentifier, link: "togo://session/01?host=Studio"
        ))
        #expect(b.pendingLink == .session(bib: "01", host: "Studio"))
        #expect(a.pendingLink == nil)
    }

    @Test func bannerQuietOnlyForTheActiveWindowsSession() {
        let composition = AppComposition(services: services)
        let a = composition.makeStore(window: WindowID(), restoring: nil)
        let right = WindowID()
        let b = composition.makeStore(window: right, restoring: nil)
        let router = NotificationRouter()
        router.deliver { composition.activeStore }
        a.workspace.tabs = [WorkspaceTab(hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", link: .connecting)]
        a.workspace.selected = tab("s-01")
        a.workspaceOpen = true

        #expect(router.presentation(for: "togo://session/01?host=Studio") == [.list])
        composition.activate(right)
        #expect(b.workspace.tabs.isEmpty)
        #expect(router.presentation(for: "togo://session/01?host=Studio") == [.banner, .list, .sound])
    }
}
