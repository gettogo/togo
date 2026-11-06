import Observation
import SwiftUI
import TogoKit

/// The words on a machine's notification settings.
enum MachineNotifyCopy {
    static func title(name: String) -> String {
        settingsText("settings.machineNotify.title", "Notifications from \(name)")
    }

    static func muteFooter(name: String) -> String {
        settingsText(
            "settings.machineNotify.mute.footer",
            "Nothing from \(name), including approval requests, makes a sound or shows a banner. Requests still appear in Togo."
        )
    }

    static func update(name: String) -> String {
        settingsText("settings.machineNotify.update", "Update the helper on \(name) to change these.")
    }

    static func saveFailed(name: String) -> String {
        settingsText("settings.machineNotify.saveFailed", "Couldn't save that change to \(name). Try again.")
    }

    static func loadFailed(name: String) -> String {
        settingsText("settings.machineNotify.loadFailed", "Couldn't read these settings from \(name).")
    }
}

/// One machine's notification settings. A change shows at once, rapid changes go to the
/// helper as one patch, and a refused one puts the old value back with a sentence saying so.
@MainActor @Observable final class MachineNotifyModel {
    struct IdleChoice: Hashable { let seconds: Int; let title: String }
    struct RemindChoice: Hashable { let minutes: Int; let title: String }

    static let idleChoices = [
        IdleChoice(seconds: 6, title: "6 s"), IdleChoice(seconds: 15, title: "15 s"),
        IdleChoice(seconds: 30, title: "30 s"), IdleChoice(seconds: 60, title: "1 min"),
        IdleChoice(seconds: 120, title: "2 min"),
    ]
    static let remindChoices = [
        RemindChoice(minutes: 0, title: settingsText("settings.machineNotify.remind.off", "Off")),
        RemindChoice(minutes: 5, title: "5 min"), RemindChoice(minutes: 10, title: "10 min"),
        RemindChoice(minutes: 30, title: "30 min"),
    ]

    /// What the page shows: the last answer with the changes still on their way applied.
    private(set) var settings: NotifySettings?
    private(set) var error: String?
    private(set) var loading = false

    private let host: HostID
    private let name: String
    private let tools: any MachineTools
    private let supported: Bool
    private let debounce: Duration
    private var confirmed: NotifySettings?
    private var patch = NotifyPatch()
    private var waiting: Task<Void, Never>?
    private var sending: Task<Void, Never>?

    init(
        host: HostID, name: String, tools: any MachineTools, supported: Bool, debounce: Duration = .milliseconds(500),
        loaded: NotifySettings? = nil
    ) {
        settings = loaded
        confirmed = loaded
        self.host = host
        self.name = name
        self.tools = tools
        self.supported = supported
        self.debounce = debounce
    }

    func load() async {
        guard supported, settings == nil else { return }
        loading = true
        error = nil
        defer { loading = false }
        do {
            let loaded = try await tools.call(host) { try await $0.notifySettings() }
            confirmed = loaded
            settings = loaded
            error = nil
        } catch {
            self.error = MachineNotifyCopy.loadFailed(name: name)
        }
    }

    func setMuted(_ muted: Bool) {
        change { $0.muted = muted; $1.muted = muted }
    }

    func setIdle(seconds: Int) {
        change { $0.idleSeconds = seconds; $1.idleSeconds = seconds }
    }

    func setRemind(minutes: Int) {
        change { $0.remindMinutes = minutes; $1.remindMinutes = minutes }
    }

    /// Sends what is waiting now rather than at the end of the quiet time.
    func flush() async {
        waiting?.cancel()
        waiting = nil
        await send()
    }

    private func change(_ apply: (inout NotifySettings, inout NotifyPatch) -> Void) {
        guard var shown = settings else { return }
        apply(&shown, &patch)
        settings = shown
        error = nil
        waiting?.cancel()
        waiting = Task { [debounce] in
            try? await Task.sleep(for: debounce)
            guard !Task.isCancelled else { return }
            await send()
        }
    }

    /// Runs one sender at a time. The sender clears itself in the same stretch of code in which
    /// it sees nothing left to send, so a change made afterwards starts a new one.
    private func send() async {
        if let sending {
            await sending.value
            return
        }
        let run = Task { await drain() }
        sending = run
        await run.value
    }

    private func drain() async {
        defer { sending = nil }
        while patch != NotifyPatch() {
            let outgoing = patch
            patch = NotifyPatch()
            do {
                let result = try await tools.call(host) { try await $0.setNotify(outgoing) }
                confirmed = result
            } catch {
                // Only the failed values go back; a newer change made meanwhile stays and is sent next.
                self.error = MachineNotifyCopy.saveFailed(name: name)
            }
            settings = confirmed.map(applyingPending)
        }
    }

    private func applyingPending(_ base: NotifySettings) -> NotifySettings {
        var shown = base
        if let idle = patch.idleSeconds { shown.idleSeconds = idle }
        if let muted = patch.muted { shown.muted = muted }
        if let remind = patch.remindMinutes { shown.remindMinutes = remind }
        return shown
    }
}

/// The section of a machine's page that holds its notification settings.
struct MachineNotifySection: View {
    let name: String
    let supported: Bool
    let model: MachineNotifyModel

    var body: some View {
        Section {
            if !supported {
                Text(MachineNotifyCopy.update(name: name))
                    .foregroundStyle(Color.togoMute)
                    .frame(minHeight: 44, alignment: .leading)
            } else if let settings = model.settings {
                Toggle(
                    settingsText("settings.machineNotify.mute", "Mute this machine"),
                    isOn: Binding { settings.muted } set: { model.setMuted($0) }
                )
                .frame(minHeight: 44)
                Picker(
                    settingsText("settings.machineNotify.idle", "Count as done after"),
                    selection: Binding { settings.idleSeconds } set: { model.setIdle(seconds: $0) }
                ) {
                    ForEach(idleChoices(including: settings.idleSeconds), id: \.seconds) { Text($0.title).tag($0.seconds) }
                }
                .frame(minHeight: 44)
                Picker(
                    settingsText("settings.machineNotify.remind", "Remind me about a request"),
                    selection: Binding { settings.remindMinutes } set: { model.setRemind(minutes: $0) }
                ) {
                    ForEach(remindChoices(including: settings.remindMinutes), id: \.minutes) { Text($0.title).tag($0.minutes) }
                }
                .frame(minHeight: 44)
            } else if model.loading {
                ProgressView().frame(maxWidth: .infinity, minHeight: 44)
            } else if model.error != nil {
                Button(settingsText("settings.machineNotify.retry", "Try again")) { Task { await model.load() } }
                    .frame(minHeight: 44)
            }
        } header: {
            Text(MachineNotifyCopy.title(name: name))
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                .textCase(nil)
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                if let error = model.error {
                    Label(error, systemImage: "exclamationmark.circle")
                        .foregroundStyle(Color.togoSnow)
                }
                if supported, model.settings != nil {
                    Text(MachineNotifyCopy.muteFooter(name: name))
                }
            }
        }
        .groupedRows()
    }

    /// A value the helper holds that is not one of the choices still shows, so the picker
    /// never reads as something it is not.
    private func idleChoices(including seconds: Int) -> [MachineNotifyModel.IdleChoice] {
        let all = MachineNotifyModel.idleChoices
        return all.contains { $0.seconds == seconds } ? all : all + [.init(seconds: seconds, title: "\(seconds) s")]
    }

    private func remindChoices(including minutes: Int) -> [MachineNotifyModel.RemindChoice] {
        let all = MachineNotifyModel.remindChoices
        return all.contains { $0.minutes == minutes } ? all : all + [.init(minutes: minutes, title: "\(minutes) min")]
    }
}
