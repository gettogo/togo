import SwiftUI
import TogoKit
import UIKit

/// A sign-in started from a picker's "Not set up" entry, pushed inside the picker's own sheet:
/// the command goes to the session, the agent's page shows here (not as a second sheet, which
/// the picker's would take down with it), opens in the system's authentication browser over
/// it, and the outcome stays until the user leaves. When the provider can show a code instead,
/// or the agent asks for one, a Paste code field takes it.
@MainActor @Observable final class SignInPageModel: Hashable {
    /// The provider, as the picker lists it ("Anthropic").
    let provider: String
    /// The agent, as the app names it ("OMP").
    let agent: String
    let hostID: HostID
    let sessionID: String
    private(set) var progress = SignInProgress()
    /// The web sign-in under way: the authentication browser and its callback forward.
    private(set) var runner: SignInRunner?
    /// The Paste code field, once there is somewhere to send a code.
    private(set) var paste: PasteCodeModel?
    /// Runs once the agent lists the provider as set up and the screen under this page was
    /// read again: the picker opens the provider's group.
    @ObservationIgnored var onSetUp: (@MainActor () -> Void)?

    @ObservationIgnored private let itemID: String
    @ObservationIgnored private let send: @MainActor () async -> HarnessActionResult?
    @ObservationIgnored private let read: (@MainActor () async -> HarnessScreen?)?
    @ObservationIgnored private let refresh: (@MainActor () async -> Void)?
    @ObservationIgnored private var watching: Task<Void, Never>?
    /// The page left its stack.
    @ObservationIgnored private var left = false

    /// How often the agent's list is read for the provider while the sign-in runs.
    static let checkEvery: Duration = .seconds(3)

    init(
        provider: String, itemID: String, agent: String, hostID: HostID, sessionID: String,
        send: @escaping @MainActor () async -> HarnessActionResult?,
        read: (@MainActor () async -> HarnessScreen?)?, refresh: (@MainActor () async -> Void)?
    ) {
        self.provider = provider
        self.itemID = itemID
        self.agent = agent
        self.hostID = hostID
        self.sessionID = sessionID
        self.send = send
        self.read = read
        self.refresh = refresh
    }

    nonisolated static func == (lhs: SignInPageModel, rhs: SignInPageModel) -> Bool { lhs === rhs }
    nonisolated func hash(into hasher: inout Hasher) { hasher.combine(ObjectIdentifier(self)) }

    /// Claims the session's pages before the command goes, so the agent's page comes here
    /// however fast it arrives, then sends the command and watches the agent's list.
    func start() {
        left = false
        makePaste()
        let requests = LinkCenter.shared.requests
        if let waiting = requests.wait(hostID: hostID, sessionID: sessionID, owner: self, deliver: { [weak self] in
            self?.progress.arrived($0)
        }) {
            progress.arrived(waiting)
        }
        Task {
            let result = await send()
            progress.sent(result)
            if let screen = result?.screen { check(screen) }
        }
        watch()
    }

    func retry() {
        progress.retry()
        start()
    }

    // MARK: The page

    /// Opens the agent's page in the system's authentication browser, after the forward for
    /// its callback when it returns to the machine.
    func openHere() {
        guard let open = progress.open else { return }
        LinkCenter.shared.requests.resolve(open.id)
        runner?.finish()
        let runner = LinkCenter.shared.makeRunner(open.url, hostID: hostID, sessionID: sessionID)
        runner.onDelivered = { [weak self] in self?.callbackDelivered() }
        runner.onBrowserClosed = { [weak self] in self?.browserClosed() }
        self.runner = runner
        progress.browsing()
        runner.begin()
    }

    func openOnMac() async throws {
        guard let open = progress.open else { return }
        try await LinkCenter.shared.sendToMac(open)
        progress.openedElsewhere()
    }

    var canOpenOnMac: Bool { LinkCenter.shared.openOnMac != nil }

    /// Whether the Paste code field shows: the provider can show a code, the callback can't
    /// come back on its own, or the agent waits for a code on screen.
    var showsPaste: Bool {
        guard progress.watching, LinkCenter.shared.codeSender != nil else { return false }
        if runner?.phase == .needsCode { return true }
        if let open = progress.open, SignInURL(open.url).offersCode { return true }
        return LinkCenter.shared.awaitsCode?(hostID, sessionID) ?? false
    }

    /// Makes the Paste code field's model, once.
    private func makePaste() {
        guard paste == nil, let sender = LinkCenter.shared.codeSender else { return }
        let model = PasteCodeModel(hostID: hostID, sessionID: sessionID, sender: sender)
        model.onSent = { [weak self] in
            self?.runner?.codeSent()
            self?.progress.callbackDelivered()
            Task { await self?.checkNow() }
        }
        paste = model
    }

    /// Why the callback can't come back on its own, when it can't.
    var runnerNote: String? { runner?.phase == .needsCode ? runner?.note : nil }

    /// The authentication browser closed: after the callback, wait for the agent; before it,
    /// offer the page again (and the code field, when there is one).
    private func browserClosed() {
        guard let runner else { return }
        if left {
            runner.finish()
            return
        }
        switch runner.phase {
        case .delivered, .signedIn: callbackDelivered()
        default: progress.browserClosed()
        }
    }

    private func callbackDelivered() {
        progress.callbackDelivered()
        Task { await checkNow() }
    }

    // MARK: Watching the agent's list

    private func watch() {
        watching?.cancel()
        watching = Task { [weak self] in
            // The agent stops listening for the sign-in long before this.
            for _ in 0..<200 {
                try? await Task.sleep(for: Self.checkEvery)
                guard !Task.isCancelled, let self, self.progress.watching else { return }
                await self.checkNow()
            }
        }
    }

    private func checkNow() async {
        guard progress.watching, let read, let screen = await read() else { return }
        check(screen)
    }

    private func check(_ screen: HarnessScreen) {
        guard let notSetUp = SignInProgress.stillNotSetUp(itemID, in: screen) else { return }
        progress.listed(setUp: !notSetUp)
        guard progress.phase == .signedIn else { return }
        // Whatever the browser still shows, the agent has the provider now.
        runner?.providerSetUp()
        runner?.finish()
        watching?.cancel()
        LinkCenter.shared.requests.stopWaiting(self)
        // Pages the agent asked for in this sign-in are done with.
        for open in LinkCenter.shared.requests.pending where open.hostID == hostID && open.sessionID == sessionID {
            LinkCenter.shared.requests.resolve(open.id)
        }
        // The screen under this page reads the agent's list again now, so the provider's
        // models are there when the user goes back (the helper skips its cache while a
        // sign-in runs).
        Task {
            await refresh?()
            onSetUp?()
        }
    }

    /// The page shows again, as after the authentication browser covered it.
    func returned() {
        guard left else { return }
        left = false
        if progress.watching { watch() }
    }

    /// The user left the page.
    func leave() {
        left = true
        // The authentication browser can hide this page as it comes up; it ends the sign-in
        // itself when it closes (`browserClosed`).
        if runner?.flow.browserOpen != true { runner?.finish() }
        watching?.cancel()
        LinkCenter.shared.requests.stopWaiting(self)
    }
}

/// "Sign in to Anthropic": progress, the agent's page as a card, and the outcome.
struct SignInPage: View {
    @Environment(\.dismiss) private var dismiss
    let model: SignInPageModel

    var body: some View {
        List {
            Section {
                status
            } footer: {
                if let footer { Text(footer) }
            }
            if let open = model.progress.open, showsCard {
                Section {
                    SignInLinkCard(open: open, agent: model.agent, model: model)
                }
                .groupedRows()
            }
            if model.showsPaste, let paste = model.paste {
                Section {
                    if let note = model.runnerNote {
                        Text(note)
                            .font(.footnote)
                            .foregroundStyle(Color.togoMute)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    PasteCodeField(model: paste)
                }
                .groupedRows()
            }
        }
        .groupedList()
        .navigationTitle(SignInPageCopy.title(model.provider))
        .navigationBarTitleDisplayMode(.inline)
        // motion: state change — each step of the sign-in replaces the last in place.
        .motion(.fade, value: SignInPageCopy.key(model.progress.phase))
        .haptic(.done, trigger: model.progress.phase, when: { $0 == .signedIn })
        .onAppear { model.returned() }
        .onDisappear { model.leave() }
    }

    private var showsCard: Bool {
        switch model.progress.phase {
        case .link, .browsing, .confirming: true
        default: false
        }
    }

    @ViewBuilder private var status: some View {
        switch model.progress.phase {
        case .sending:
            SignInStatusRow(kind: .working, text: SignInPageCopy.sending(model.agent))
        case .waiting:
            SignInStatusRow(kind: .working, text: SignInPageCopy.waiting(model.agent))
        case .link:
            SignInStatusRow(kind: .waiting, text: SignInPageCopy.ready)
        case .browsing:
            SignInStatusRow(kind: .working, text: SignInPageCopy.browsing)
        case .confirming:
            SignInStatusRow(kind: .working, text: SignInPageCopy.confirming(model.agent))
        case .signedIn:
            SignInStatusRow(kind: .done, text: SignInPageCopy.signedIn(model.provider))
            Button(LinkCopy.done) { dismiss() }
                .buttonStyle(.primary)
                .listRowBackground(Color.clear)
        case .failed(let message):
            SignInStatusRow(kind: .failed, text: message.isEmpty ? SignInPageCopy.failed(model.agent) : message)
            Button(PrimitiveCopy.tryAgain) { model.retry() }
                .buttonStyle(.secondary)
                .listRowBackground(Color.clear)
        }
    }

    private var footer: String? {
        switch model.progress.phase {
        case .waiting: SignInPageCopy.waitingDetail
        case .confirming: SignInPageCopy.confirmingDetail
        default: nil
        }
    }
}

/// One line of where the sign-in stands, with its mark.
private struct SignInStatusRow: View {
    enum Kind { case working, waiting, done, failed }
    let kind: Kind
    let text: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Group {
                switch kind {
                case .working: ProgressView().controlSize(.small)
                case .waiting: Image(systemName: "arrow.up.forward.app").foregroundStyle(Color.togoMute)
                case .done: Image(systemName: "checkmark.circle.fill").foregroundStyle(Color.togoSnow)
                case .failed: Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(Color.togoMute)
                }
            }
            .accessibilityHidden(true)
            Text(text)
                .font(kind == .done ? .headline : .body)
                .foregroundStyle(Color.togoSnow)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
        // On the row itself rather than its section: the Done and Try again rows under it
        // keep their clear background.
        .groupedRows()
    }
}
