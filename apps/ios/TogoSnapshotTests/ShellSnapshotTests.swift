import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// The regular-width shell, the roster column beside the terminal column, across the sizes that
/// show it, and the sheets it presents, floating over it.
@MainActor
struct ShellSnapshotTests {
    private static let shellSizes: [ScreenSize] = [.duoOpen, .duoBook, .iPadPro13Landscape, .iPadHalf]
    private static let lab = HostID(uuidString: "6F1C2D3E-0000-4000-8000-000000000003")!

    /// Studio and Build box from the samples, then a third machine with no sessions yet.
    private static func machines(_ count: Int) -> [MachineSection] {
        var machines = Samples.controlCenter.machines
        machines.append(MachineSection(
            id: lab, name: "Lab", symbol: .laptop, reachability: .online,
            helper: .present(version: "1.0.0", live: true), sessions: []
        ))
        return Array(machines.prefix(count))
    }

    private func window(machines count: Int, sidebarShown: Bool = true) -> some View {
        let machines = Self.machines(count)
        let hosts = Samples.hosts.filter { host in machines.contains { $0.id == host.id } }
            + (count >= 3 ? [HostRecordView(id: Self.lab, name: "Lab", symbol: .laptop, address: "lab.local")] : [])
        var state = Samples.controlCenter
        state.machines = machines
        if count == 0 { state.requests = [] }
        let services = FakeAppServices(hosts: hosts)
        services.terminalPhases = []
        let store = AppStore(services: services, controlCenter: state)
        store.sidebar.hidden = !sidebarShown
        let terminals = TerminalPool()
        terminals.sync(with: store)
        return AdaptiveRoot(store: store, terminals: terminals, now: Samples.now) { _ in EmptyView() }
            .readsFold()
            .togoControls()
    }

    @Test func noMachines() {
        assertScreen(named: "shell-0", sizes: Self.shellSizes) { window(machines: 0) }
    }

    @Test func oneMachine() {
        assertScreen(named: "shell-1", sizes: Self.shellSizes) { window(machines: 1) }
    }

    @Test func threeMachines() {
        assertScreen(named: "shell-3", sizes: Self.shellSizes) { window(machines: 3) }
    }

    /// With the roster hidden, the empty detail offers it back and centres on the whole window.
    @Test func emptyDetailSidebarHidden() {
        assertScreen(named: "shell-hidden", sizes: [.duoOpen, .duoBook, .iPadPro13Landscape]) {
            window(machines: 2, sidebarShown: false)
        }
    }

    /// A trailing system bar of 84 pt doesn't pull the empty detail off centre.
    @Test func emptyDetailIgnoresASideBar() {
        assertScreen(named: "empty-detail", sizes: [.duoFolded]) {
            RosterEmptyDetail()
        }
    }

    /// Something waits: the empty detail says so, and the bar at its foot holds the request.
    @Test func emptyDetailWithARequest() {
        assertScreen(named: "empty-detail-waiting", sizes: [.duoFolded]) {
            RosterEmptyDetail(waiting: true)
        }
    }

    // MARK: Sheets

    /// The new-session sheet as presented: floating at its first height on a phone and a folded
    /// Duo, a card in a regular window.
    @Test func newSessionSheetFloats() {
        assertScreen(named: "new-session-sheet", sizes: [.iPhone17, .duoFolded, .duoOpen]) {
            Color.togoInk.sheet(isPresented: .constant(true)) {
                NewSessionSheet(model: NewSessionModel(
                    services: FakeAppServices(), hostID: nil, machines: Samples.controlCenter.machines
                ))
            }
        }
    }
}
