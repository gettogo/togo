#if DEBUG
import ActivityKit
import CryptoKit
import Foundation
import TogoKit
import TogoPush
import UIKit
import UserNotifications

/// `-ui-demo-live`, with `-ui-replay`: the replayed session drives the Live Activity and the
/// finished notification the way the helper's pushes would on a paired phone, for the website's
/// screen recording. The activity's content and its sealed context are what the helper would
/// send; the app starts and updates it locally because a simulator has no relay. Approve and
/// Deny in the Dynamic Island mark the request in the pending store (see `LiveAnswer`), and the
/// demo answers the replay from there.
@MainActor final class LiveDemo {
    static var enabled: Bool { ProcessInfo.processInfo.arguments.contains("-ui-demo-live") }

    /// The shared-group flag `LiveAnswer` reads: answer by marking the pending store only.
    static let localAnswers = "ui.demo.liveAnswers"
    static let hostId = "h_demo"

    private let agent = "claude"
    private let bib = "01"
    private let session: String
    private let directory = SharedGroup.storeDirectory(SharedGroup.ios)
    private let keys = KeychainDeviceKeys(accessGroup: nil)
    /// The activity's id: `Activity` is not `Sendable`, so it is looked up where it is used.
    private var activity: String?
    private var waiting: String?
    private var sends = 0
    private var background: UIBackgroundTaskIdentifier = .invalid
    private var watch: Task<Void, Never>?
    /// The control center answered the request the island approved.
    var onAnswered: ((String, String) -> Void)?

    init(session: String) {
        self.session = session
    }

    /// Pairs the demo machine and asks for notifications, before anything is recorded.
    func prepare(appHostID: UUID?) {
        guard let directory else { return }
        Task { await Self.endAll() }
        let hostKey = Base64URL.encode(Curve25519.KeyAgreement.PrivateKey().publicKey.rawRepresentation)
        PairingStore(directory: directory).put(Pairing(
            hostId: Self.hostId, hostKey: hostKey, relayURL: URL(string: "https://relay.invalid")!,
            machineName: "Studio", appHostID: appHostID, pairedAt: Date(), tokenHash: "demo00",
            liveActivityCommand: true
        ))
        if let appHostID, let data = try? JSONEncoder().encode([appHostID]) {
            SharedSettings.defaults?.set(data, forKey: SharedSettings.liveActivityCommandMachines)
        }
        SharedSettings.defaults?.set(true, forKey: Self.localAnswers)
        SharedSettings.defaults?.set(true, forKey: SharedSettings.approveWithoutUnlock)
        Task { _ = try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) }
    }

    func playback(_ step: ReplayHarnessServices.Playback) {
        switch step {
        case .released("harness.send"):
            sends += 1
            update(status: "working", context: LiveContext(title: nil, step: nil))
        case .released("approval.answer"):
            waiting = nil
            update(status: "working", context: LiveContext())
        case .event(let event):
            switch event["kind"] as? String {
            case "approval.open": opened(event["approval"])
            case "notify": notified(event["notify"] ?? event["payload"] ?? event)
            default: break
            }
        case .finished(let trigger) where (trigger == "approval.answer" || trigger == "harness.send") && waiting == nil:
            finish()
        default:
            break
        }
    }

    // MARK: The activity

    private func update(status: String, context: LiveContext, requestId: String? = nil, command: String? = nil,
                        alert: AlertConfiguration? = nil) {
        let state = TogoActivityAttributes.ContentState(
            bib: bib, agent: agent, status: status, waiting: status == "needs_you" ? 1 : 0,
            since: Int(Date().timeIntervalSince1970), requestId: requestId, command: command,
            ctx: seal(context)
        )
        let content = ActivityContent(state: state, staleDate: nil)
        if let activity {
            Task { await Self.update(activity, content, alert: alert) }
        } else if UIApplication.shared.applicationState == .active {
            activity = try? Activity.request(attributes: TogoActivityAttributes(hostId: Self.hostId), content: content).id
        }
        keepRunning()
    }

    private func opened(_ raw: Any?) {
        guard let raw, let data = try? JSONSerialization.data(withJSONObject: raw),
              let approval = try? JSONDecoder().decode(Approval.self, from: data) else { return }
        let subject = approval.subject.map { PushSubject(kind: $0.kind.rawValue, text: $0.text) }
        waiting = approval.requestId
        if let directory {
            PendingStore(directory: directory, clock: { Date() }).upsert(PendingRequest(
                requestId: approval.requestId, hostId: Self.hostId, sessionId: session, bib: bib, agent: agent,
                host: "Studio", title: approval.title, subject: subject, options: [], receivedAt: Date(),
                answered: nil
            ))
        }
        let context = LiveContext(ask: approval.title, subject: subject, answer: .approveDeny)
        let alert = AlertConfiguration(
            title: "Claude Code needs you", body: LocalizedStringResource(stringLiteral: subject?.text ?? approval.title),
            sound: .default
        )
        update(status: "needs_you", context: context, requestId: approval.requestId, command: subject?.text, alert: alert)
        watchAnswer(approval.requestId)
    }

    /// Waits for the island's Approve or Deny to mark the request, then answers the replay.
    private func watchAnswer(_ requestId: String) {
        watch?.cancel()
        guard let directory else { return }
        watch = Task { [weak self] in
            let pending = PendingStore(directory: directory, clock: { Date() })
            while !Task.isCancelled {
                if let answered = pending.request(requestId)?.answered, answered == .approved || answered == .denied {
                    self?.onAnswered?(requestId, answered == .denied ? "deny" : "approve")
                    return
                }
                try? await Task.sleep(for: .milliseconds(150))
            }
        }
    }

    private var summary: (title: String, body: String)?

    /// The helper's notify event for the turn's end, as its push would carry it.
    private func notified(_ raw: Any?) {
        guard let notify = raw as? [String: Any] else { return }
        let level = notify["level"] as? String
        guard level == "done" || level == nil else { return }
        let title = notify["title"] as? String ?? ""
        let body = notify["body"] as? String ?? notify["message"] as? String ?? ""
        if !body.isEmpty { summary = (title, body) }
    }

    private func finish() {
        // The notification first, as the helper's done push and its activity update go together.
        defer { update(status: "done", context: LiveContext()) }
        guard UIApplication.shared.applicationState != .active else { return }
        let payload = PushPayload(
            kind: .done, sessionId: session, bib: bib, agent: agent, title: summary?.title ?? "Finished",
            body: summary?.body ?? "Finished", ts: Int64(Date().timeIntervalSince1970 * 1000),
            sessionName: "api"
        )
        guard let text = NotificationTextBuilder.build(payload, hostId: Self.hostId) else { return }
        let content = UNMutableNotificationContent()
        content.title = text.title
        content.subtitle = text.subtitle
        content.body = text.body
        content.threadIdentifier = text.threadID
        content.sound = .default
        content.interruptionLevel = .active
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: "demo-done-\(sends)", content: content, trigger: nil))
    }

    private nonisolated static func endAll() async {
        for old in Activity<TogoActivityAttributes>.activities {
            await old.end(nil, dismissalPolicy: .immediate)
        }
    }

    private nonisolated static func update(
        _ id: String, _ content: ActivityContent<TogoActivityAttributes.ContentState>, alert: AlertConfiguration?
    ) async {
        await Activity<TogoActivityAttributes>.activities.first { $0.id == id }?.update(content, alertConfiguration: alert)
    }

    /// The context sealed to this device, the way the helper seals it.
    private func seal(_ context: LiveContext) -> String? {
        guard let id = try? keys.deviceID(), let key = try? keys.agreementKey(),
              let plain = try? JSONEncoder().encode(context) else {
            UserDefaults.standard.set("no keys", forKey: "ui.demo.seal")
            return nil
        }
        let sealed = try? Seal.seal(to: key.publicKey, plain, label: Labels.live, aad: Data("\(Self.hostId)|\(id)".utf8))
        let opens = sealed.flatMap { LiveContext.open($0, hostId: Self.hostId, keys: keys) } != nil
        UserDefaults.standard.set(sealed == nil ? "seal failed" : opens ? "ok" : "does not open", forKey: "ui.demo.seal")
        return sealed
    }

    /// The replay plays on while the Home Screen is up.
    private func keepRunning() {
        guard background == .invalid else { return }
        background = UIApplication.shared.beginBackgroundTask(withName: "live-demo") { [weak self] in
            guard let self else { return }
            UIApplication.shared.endBackgroundTask(self.background)
            self.background = .invalid
        }
    }
}
#endif
