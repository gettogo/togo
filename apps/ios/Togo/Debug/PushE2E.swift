// Compiled out of Release, like everything under Debug.
#if DEBUG
import ActivityKit
import CryptoKit
import Foundation
import TogoKit
import TogoPush
import UserNotifications

/// Lets scripts/e2e-push.sh drive the push path on a simulator. Launch arguments:
///
/// - `-push-e2e`: ask for notification permission (a UI test answers), register with APNs, write
///   this device's `push.pair` params to `Library/e2e-pair.json` and keep
///   `Library/e2e-status.json` current.
/// - `-push-e2e-relay <url>`: the relay to pair with; the hosted one otherwise.
/// - `-push-e2e-local`: pushes come from `simctl push`, so a stand-in token is used when APNs
///   gives none.
/// - `-push-e2e-pairing <json>`: store the helper's `push.pair` answer as a pairing.
/// - `-push-e2e-open-delivered`: run delivered pushes still holding their envelope through
///   `NotificationServiceCore`, the code of the notification service extension. `simctl push`
///   never starts the extension, so the local check opens its pushes this way.
/// - `-push-e2e-silent`: hand `Library/e2e-silent.apns` to the app delegate's background push
///   handler, as iOS would; `simctl push` refuses pushes with nothing to show.
/// - `-push-e2e-approve <requestId>` with `-push-e2e-decision <decision>` (default `approve`):
///   answer through `NotificationActionHandler`. `approve` and `deny` go in as the
///   notification's Approve or Deny action, with the `userInfo` the notification carries.
///
/// The status file holds no keys, tokens or notification text: what was shown is reduced to a
/// 6-character hash and whether any box drawing survived.
@MainActor enum PushE2E {
    private static var running = false

    static var enabled: Bool { ProcessInfo.processInfo.arguments.contains("-push-e2e") }

    static func start(_ push: AppPush) {
        guard enabled, !running, let directory = SharedGroup.storeDirectory(SharedGroup.ios) else { return }
        running = true
        let run = Run(push: push, directory: directory)
        Task { await run.go() }
    }

    @MainActor private final class Run {
        let push: AppPush
        let keys = KeychainDeviceKeys(accessGroup: nil)
        let pairings: PairingStore
        let pending: PendingStore
        var status = Status()
        /// What each request's notification carries, from Notification Center or as opened here.
        var contents: [String: UNNotificationContent] = [:]

        init(push: AppPush, directory: URL) {
            self.push = push
            pairings = PairingStore(directory: directory)
            pending = PendingStore(directory: directory, clock: { Date() })
        }

        func go() async {
            Task { await self.report() }
            if let raw = argument("-push-e2e-pairing") { storePairing(raw) }
            status.paired = !pairings.all().isEmpty
            await register()
            if ProcessInfo.processInfo.arguments.contains("-push-e2e-open-delivered") { await openDelivered() }
            if ProcessInfo.processInfo.arguments.contains("-push-e2e-silent") { await deliverSilent() }
            if let requestId = argument("-push-e2e-approve") {
                await approve(requestId, decision: argument("-push-e2e-decision") ?? "approve")
            }
        }

        private func register() async {
            let center = UNUserNotificationCenter.current()
            if await center.notificationSettings().authorizationStatus == .notDetermined {
                // Not provisional: quiet delivery skips the notification service extension.
                _ = try? await center.requestAuthorization(options: [.alert, .sound, .badge])
            }
            await push.registrar.refresh()
            status.authorization = "\(push.registrar.authorization)"
            for _ in 0..<40 where push.registrar.apnsToken == nil {
                try? await Task.sleep(for: .milliseconds(500))
            }
            var token = push.registrar.apnsToken
            status.token = token == nil ? "none" : "real"
            if token == nil, ProcessInfo.processInfo.arguments.contains("-push-e2e-local"),
               let id = try? keys.deviceID() {
                token = SHA256.hash(data: Data(id.utf8)).map { String(format: "%02x", $0) }.joined()
                status.token = "placeholder"
            }
            guard let token else { return }
            writePairParams(apnsToken: token)
        }

        private func writePairParams(apnsToken: String) {
            guard let relay = argument("-push-e2e-relay").flatMap(URL.init(string:)) ?? PushEnvironment.defaultRelay,
                  let params = try? PushPairParams(
                deviceId: keys.deviceID(), name: "Togo E2E",
                x25519: Base64URL.encode(keys.agreementKey().publicKey.rawRepresentation),
                approvalKey: Base64URL.encode(keys.signingKey().publicKey.rawRepresentation),
                apnsToken: apnsToken, env: PushEnvironment.apns, relayUrl: relay.absoluteString, platform: .ios
            ), let data = try? JSONEncoder().encode(params)
            else { return }
            try? data.write(to: URL.libraryDirectory.appending(path: "e2e-pair.json"), options: .atomic)
        }

        private func storePairing(_ raw: String) {
            struct Answer: Decodable {
                var hostId: String
                var hostKey: String
                var relayUrl: URL
            }
            guard let answer = try? JSONDecoder().decode(Answer.self, from: Data(raw.utf8)) else { return }
            pairings.put(Pairing(
                hostId: answer.hostId, hostKey: answer.hostKey, relayURL: answer.relayUrl, machineName: "E2E Mac",
                appHostID: nil, pairedAt: Date(), tokenHash: "", liveActivityCommand: false
            ))
        }

        /// Waits for the request's Live Activity, records what the notification showed, then
        /// answers as the notification's action would.
        private func approve(_ requestId: String, decision: String) async {
            for _ in 0..<20 where Activity<TogoActivityAttributes>.activities.isEmpty {
                try? await Task.sleep(for: .milliseconds(500))
            }
            await recordDelivered()
            guard let hostId = pending.request(requestId)?.hostId ?? pairings.all().first?.hostId else {
                status.answer = "failed"
                return
            }
            let handler = NotificationActionHandler(dependencies: .init(
                pairings: pairings, pending: pending,
                relay: RelayAnswerSender(keys: keys, pending: pending, clock: { Date() }),
                live: nil, notifications: SystemNotifications()
            ))
            let action = [
                "approve": NotificationActionID.approve, "deny": NotificationActionID.deny,
            ][decision]
            if let action, let content = contents[requestId] {
                // The lock-screen action: the same entry point and userInfo iOS hands the app.
                await handler.handle(actionIdentifier: action, userInfo: content.userInfo, target: nil)
                status.answer = pending.request(requestId)?.answered.map { "action \($0.rawValue)" } ?? "action failed"
            } else {
                let outcome = await handler.answer(hostId: hostId, requestId: requestId, decision: decision)
                status.answer = "\(outcome)"
            }
        }

        private func openDelivered() async {
            let core = NotificationServiceCore(keys: keys, pairings: pairings, pending: pending, clock: { Date() })
            for note in await UNUserNotificationCenter.current().deliveredNotifications()
            where note.request.content.userInfo[NotificationServiceCore.envelopeKey] != nil {
                let content = core.process(note.request.content).content
                if let shown = Self.shown(content) {
                    status.opened.append(shown)
                    contents[shown.requestId] = content
                }
            }
        }

        private func deliverSilent() async {
            let url = URL.libraryDirectory.appending(path: "e2e-silent.apns")
            guard let data = try? Data(contentsOf: url),
                  let payload = try? JSONSerialization.jsonObject(with: data) as? [AnyHashable: Any],
                  let clear = AppDelegate.clear
            else {
                status.silent = "unreadable"
                return
            }
            status.silent = await clear.didReceiveRemoteNotification(payload) ? "handled" : "ignored"
        }

        private func recordDelivered() async {
            for note in await UNUserNotificationCenter.current().deliveredNotifications() {
                guard let shown = Self.shown(note.request.content) else { continue }
                status.delivered.append(shown)
                contents[shown.requestId] = contents[shown.requestId] ?? note.request.content
            }
        }

        static func shown(_ content: UNNotificationContent) -> Shown? {
            guard let info = content.userInfo[NotificationServiceCore.infoKey] as? [String: Any],
                  let requestId = info["requestId"] as? String
            else { return nil }
            let text = [content.title, content.subtitle, content.body].joined(separator: "\n")
            return Shown(
                requestId: requestId, subtitleHash: hash(content.subtitle),
                boxDrawing: text.unicodeScalars.contains { (0x2500...0x259F).contains($0.value) },
                category: content.categoryIdentifier
            )
        }

        /// Rewrites the status file twice a second for as long as the app runs.
        private func report() async {
            let url = URL.libraryDirectory.appending(path: "e2e-status.json")
            while !Task.isCancelled {
                let live = Activity<TogoActivityAttributes>.activities.count
                status.activities = live
                status.maxActivities = max(status.maxActivities, live)
                if let data = try? JSONEncoder().encode(status) { try? data.write(to: url, options: .atomic) }
                try? await Task.sleep(for: .milliseconds(500))
            }
        }

        /// The value after `name` on the command line. Read directly: the defaults argument
        /// domain would parse a JSON value as a property list.
        private func argument(_ name: String) -> String? {
            let args = ProcessInfo.processInfo.arguments
            guard let i = args.firstIndex(of: name), i + 1 < args.count else { return nil }
            return args[i + 1]
        }

        static func hash(_ text: String) -> String {
            SHA256.hash(data: Data(text.utf8)).prefix(3).map { String(format: "%02x", $0) }.joined()
        }
    }

    struct Shown: Encodable {
        var requestId: String
        var subtitleHash: String
        var boxDrawing: Bool
        var category: String
    }

    struct Status: Encodable {
        var token = "pending"
        var authorization = "unknown"
        var paired = false
        var activities = 0
        var maxActivities = 0
        var delivered: [Shown] = []
        var opened: [Shown] = []
        var answer = "none"
        var silent = "none"
    }
}
#endif
