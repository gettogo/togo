import Foundation
import TogoKit
import UIKit

/// The servers listening on a session's machine, and the previews open on them.
///
/// A preview reaches its server through an SSH forward that listens on this device's loopback
/// address. Forwards are short-lived: dismissing a preview stops its forward unless a copied link
/// still uses it, leaving the ports screen stops every one, and so do ten minutes without use.
/// Going to the background pauses them; the services reopen each on the local port it had.
@MainActor @Observable final class PortsModel {
    enum Phase: Equatable {
        case loading, loaded
        case failed(String)
    }

    /// The preview on screen. Its address is built here from the local port, never a host name.
    /// A forward reopened on another local port is a new preview, so the page loads again there.
    struct Preview: Identifiable, Equatable {
        var remote: Int
        var localPort: Int
        var url: URL { PortsModel.loopbackURL(localPort) }
        var id: URL { url }
    }

    let subject: ReviewSubject
    private(set) var phase: Phase = .loading
    /// What the machine reports, plus open previews whose server stopped or was opened by number.
    private(set) var ports: [ListeningPort] = []
    /// Keyed by the port on the machine.
    private(set) var states: [Int: ForwardState] = [:]
    /// What a forward reported on its own, shown under its row.
    private(set) var notes: [Int: String] = [:]
    /// Ports with an open preview that the machine no longer lists.
    private(set) var gone: Set<Int> = []
    private(set) var manualError: String?
    var manualText = ""
    private(set) var shown: Preview?
    /// The sheet's binding. Clearing it is the user dismissing the preview, which stops its
    /// forward unless a copied link still uses it.
    var preview: Preview? {
        get { shown }
        set {
            guard newValue == nil else {
                shown = newValue
                return
            }
            guard let old = shown else { return }
            shown = nil
            Task { await self.dismissed(old) }
        }
    }
    var previewURL: URL? { shown?.url }

    private let services: any ReviewServices
    private let pollInterval: Duration
    private let idleLimit: Duration
    private let copyToPasteboard: (URL) -> Void
    /// Ports opened by number, which the machine's list for this session never includes.
    private var manual: [Int: ListeningPort] = [:]
    /// Ports whose link was copied, so their forward outlives the preview.
    private var linked: Set<Int> = []
    /// Ports whose forward ended while the app was away; they reopen when it comes back.
    private var reopen: [Int: ListeningPort] = [:]
    private var visible = false
    private var foreground = true
    private var poller: Task<Void, Never>?
    private var idleTimer: Task<Void, Never>?
    /// Reads forward events while the screen is up or a forward is open.
    @ObservationIgnored private nonisolated(unsafe) var listener: Task<Void, Never>?

    init(
        subject: ReviewSubject, services: any ReviewServices, pollInterval: Duration = .seconds(5),
        idleLimit: Duration = .seconds(600), copyLink: @escaping (URL) -> Void = { UIPasteboard.general.url = $0 }
    ) {
        self.subject = subject
        self.services = services
        self.pollInterval = pollInterval
        self.idleLimit = idleLimit
        copyToPasteboard = copyLink
    }

    deinit {
        listener?.cancel()
    }

    private func listen() {
        guard listener == nil else { return }
        let events = services.forwardEvents()
        listener = Task { [weak self] in
            for await event in events {
                self?.handle(localPort: event.localPort, message: event.message)
            }
        }
    }

    // MARK: Mapping

    /// A server the machine can reach over loopback: bound to loopback or to every address.
    nonisolated static func isPreviewable(_ port: ListeningPort) -> Bool {
        ForwardTarget(port) != nil
    }

    /// Said only for an address other than loopback or every address.
    nonisolated static func addressNote(_ port: ListeningPort) -> String? {
        isPreviewable(port) ? nil : PortsCopy.notPreviewable(port.address)
    }

    nonisolated static func loopbackURL(_ localPort: Int) -> URL {
        URL(string: "http://127.0.0.1:\(localPort)/")!
    }

    func localPort(_ remote: Int) -> Int? {
        if case .open(let info)? = states[remote] { info.localPort } else { nil }
    }

    // MARK: Visibility

    func appear() {
        visible = true
        listen()
        startPolling()
    }

    /// Leaving the screen stops every forward, copied links included.
    func disappear() {
        visible = false
        poller?.cancel()
        poller = nil
        idleTimer?.cancel()
        idleTimer = nil
        listener?.cancel()
        listener = nil
        shown = nil
        linked = []
        reopen = [:]
        // Forgotten now, so coming straight back can't have a fresh forward closed under it.
        let stopping = states.keys.sorted().compactMap(localPort)
        states = [:]
        ports.removeAll { gone.contains($0.port) }
        gone = []
        let services = services
        Task {
            for localPort in stopping {
                await services.stopForward(localPort: localPort)
            }
        }
    }

    /// The services pause and reopen the forwards themselves; this pauses the list and the timer.
    func setForeground(_ on: Bool) {
        guard on != foreground else { return }
        foreground = on
        if on {
            startPolling()
            touch()
            let waiting = reopen.values.sorted { $0.port < $1.port }
            reopen = [:]
            Task {
                for port in waiting {
                    guard let localPort = await openForward(port) else { continue }
                    if shown?.remote == port.port { shown?.localPort = localPort }
                }
            }
        } else {
            poller?.cancel()
            poller = nil
            idleTimer?.cancel()
            idleTimer = nil
        }
    }

    private func startPolling() {
        guard visible, foreground, poller == nil else { return }
        let interval = pollInterval
        poller = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { return }
                await self.refresh()
                try? await Task.sleep(for: interval)
            }
        }
    }

    // MARK: List

    func refresh() async {
        do {
            let listed = try await services.ports(subject)
            apply(listed)
            phase = .loaded
        } catch {
            if phase != .loaded { phase = .failed(error.localizedDescription) }
        }
    }

    private func apply(_ listed: [ListeningPort]) {
        let numbers = Set(listed.map(\.port))
        var rows = listed
        var missing: Set<Int> = []
        for (remote, state) in states where !numbers.contains(remote) && state != .idle {
            guard let old = port(remote) else { continue }
            rows.append(old)
            if manual[remote] == nil { missing.insert(remote) }
        }
        for (remote, port) in manual where !numbers.contains(remote) && states[remote] == nil {
            rows.append(port)
        }
        ports = rows.sorted { ($0.port, $0.address) < ($1.port, $1.address) }
        gone = missing
    }

    private func port(_ remote: Int) -> ListeningPort? {
        ports.first { $0.port == remote && Self.isPreviewable($0) } ?? manual[remote]
    }

    // MARK: Previews

    func open(_ port: ListeningPort) async {
        guard let localPort = await openForward(port) else { return }
        shown = Preview(remote: port.port, localPort: localPort)
    }

    /// A port typed by hand, for a server this session didn't start.
    func openManual(_ text: String) async {
        let trimmed = text.trimmingCharacters(in: .whitespaces)
        guard trimmed.allSatisfy(\.isASCII), let number = Int(trimmed), (1...65_535).contains(number) else {
            manualError = PortsCopy.manualRange
            return
        }
        let listed = ports.filter { $0.port == number }
        if !listed.isEmpty, !listed.contains(where: Self.isPreviewable), let note = Self.addressNote(listed[0]) {
            manualError = note
            return
        }
        manualError = nil
        manualText = ""
        let port = ports.first { $0.port == number && Self.isPreviewable($0) }
            ?? ListeningPort(port: number, address: "127.0.0.1", pid: 0, command: "")
        if !ports.contains(where: { $0.port == number }) {
            manual[number] = port
            ports = (ports + [port]).sorted { $0.port < $1.port }
        }
        await open(port)
    }

    /// Copies the preview's address, opening its forward if needed. The link works while this
    /// screen is open.
    func copyLink(_ port: ListeningPort) async {
        guard let localPort = await openForward(port) else { return }
        linked.insert(port.port)
        copyToPasteboard(Self.loopbackURL(localPort))
    }

    /// The user closed this preview; the sheet already cleared `preview`.
    private func dismissed(_ old: Preview) async {
        touch()
        guard !linked.contains(old.remote), shown?.remote != old.remote else { return }
        await close(old.remote)
    }

    func close(_ remote: Int) async {
        linked.remove(remote)
        reopen[remote] = nil
        if shown?.remote == remote { shown = nil }
        let localPort = localPort(remote)
        states[remote] = nil
        if gone.contains(remote) {
            gone.remove(remote)
            ports.removeAll { $0.port == remote }
        }
        if let localPort { await services.stopForward(localPort: localPort) }
        if states.isEmpty {
            idleTimer?.cancel()
            idleTimer = nil
        }
    }

    /// The forward's local port, opening it first if needed. Nil when it can't be opened.
    @discardableResult private func openForward(_ port: ListeningPort) async -> Int? {
        guard Self.isPreviewable(port) else { return nil }
        listen()
        touch()
        if let localPort = localPort(port.port) { return localPort }
        states[port.port] = .opening
        notes[port.port] = nil
        do {
            let info = try await services.forward(subject, port: port)
            guard states[port.port] == .opening else {
                // Closed or left while it opened.
                await services.stopForward(localPort: info.localPort)
                return nil
            }
            states[port.port] = .open(info)
            touch()
            return info.localPort
        } catch {
            if states[port.port] == .opening { states[port.port] = .failed(error.localizedDescription) }
            return nil
        }
    }

    // MARK: Idle

    /// Use of a preview or this screen; ten minutes without any stops every forward.
    func touch() {
        idleTimer?.cancel()
        idleTimer = nil
        guard foreground, !states.isEmpty else { return }
        let limit = idleLimit
        idleTimer = Task { [weak self] in
            try? await Task.sleep(for: limit)
            guard !Task.isCancelled else { return }
            await self?.idleOut()
        }
    }

    private func idleOut() async {
        let open = states.compactMap { $0.value == .opening ? nil : $0.key }.sorted()
        for remote in open {
            let wasOpen = localPort(remote) != nil
            await close(remote)
            if wasOpen { notes[remote] = PortsCopy.idleStopped }
        }
    }

    // MARK: Events

    private func handle(localPort: Int, message: String) {
        let match = states.first { self.localPort($0.key) == localPort }
        guard let remote = match?.key, case .open(let info)? = match?.value else { return }
        if message == ReviewCopy.nothingListening(remote) {
            // The forward is still there; the server behind it is not.
            notes[remote] = message
            return
        }
        linked.remove(remote)
        if foreground {
            states[remote] = .failed(message)
            notes[remote] = nil
        } else {
            states[remote] = nil
            reopen[remote] = info.remote
        }
    }
}

#if DEBUG
extension PortsModel {
    /// A loaded screen in a fixed state. Its list never refreshes, so previews and snapshots hold.
    static func fixture(
        ports: [ListeningPort] = ReviewFixtures.ports, states: [Int: ForwardState] = [:],
        notes: [Int: String] = [:], gone: Set<Int> = [], manualText: String = "", manualError: String? = nil
    ) -> PortsModel {
        let services = FakeReviewServices()
        services.delays[.ports] = .seconds(86_400)
        let subject = ReviewSubject(
            hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
            managed: true, capabilities: [Capability.git, Capability.ports]
        )
        let model = PortsModel(subject: subject, services: services, copyLink: { _ in })
        model.phase = .loaded
        model.ports = ports
        model.states = states
        model.notes = notes
        model.gone = gone
        model.manualText = manualText
        model.manualError = manualError
        return model
    }
}
#endif
