import Observation
import SwiftUI
import TogoKit

/// The words on a machine's keep-awake setting.
enum AwakeCopy {
    struct Choice: Equatable {
        let mode: AwakeMode
        let title: String
    }

    static let command = "togo awake battery"

    static var choices: [Choice] {
        [
            Choice(mode: .off, title: settingsText("settings.awake.off", "Off")),
            Choice(mode: .plugged, title: settingsText("settings.awake.plugged", "When plugged in")),
            Choice(mode: .battery, title: settingsText("settings.awake.battery", "Also on battery")),
        ]
    }

    static var title: String { settingsText("settings.awake.title", "Keep awake") }

    /// The words after a machine's name while it is held awake; nil otherwise.
    static func heading(_ status: AwakeStatus) -> String? {
        status.held ? settingsText("awake.heading.held", "Held awake") : nil
    }

    /// Reachability words win: a machine that cannot be reached is not being held.
    static func caption(reach: String?, awake: AwakeStatus?) -> String? {
        reach ?? awake.flatMap(heading)
    }

    /// The line under the picker, or empty when the reason needs none.
    static func reason(_ status: AwakeStatus) -> String {
        switch status.reason {
        case .onBattery:
            return settingsText("awake.reason.onBattery", "On battery a closed lid still sleeps.")
        case .grace:
            return settingsText("awake.reason.grace", "Held for 2 minutes after the last session stops working.")
        case .batteryFloor:
            let above = status.batteryFloor + 5
            return settingsText("awake.reason.batteryFloor", "Paused until the battery is above \(above)%.")
        case .thermal:
            return settingsText("awake.reason.thermal", "Paused while the machine is hot.")
        case .unsupported:
            return settingsText("awake.reason.unsupported", "This machine can't be kept awake by Togo.")
        case .failed:
            let detail = status.detail.flatMap { $0.split(whereSeparator: \.isNewline).first.map(String.init) } ?? ""
            let prefix = settingsText("awake.reason.failed", "Couldn't keep it awake")
            return detail.isEmpty ? prefix + "." : prefix + ": " + detail
        case .off, .idle, .active, .needsAdminSetup, .unknown:
            return ""
        }
    }

    static var copied: String { settingsText("awake.sheet.copied", "Copied") }

    static var retry: String { settingsText("awake.retry", "Couldn't read keep awake. Try again.") }

    static func changeFailed(name: String) -> String {
        settingsText("awake.changeFailed", "Couldn't change keep awake on \(name). Try again.")
    }

    static func sheetTitle(name: String) -> String {
        settingsText("awake.sheet.title", "One step on \(name)")
    }

    static func sheetBody(name: String) -> String {
        settingsText(
            "awake.sheet.body",
            "Run this once on \(name) to keep it awake on battery with the lid closed. It asks for your password."
        )
    }
}

/// One machine's keep-awake choice. A pick shows at once and goes back if the machine refuses;
/// battery mode that needs the one-time setup opens the sheet that explains it.
@MainActor @Observable final class MachineAwakeModel {
    private(set) var status: AwakeStatus?
    private(set) var error: String?
    /// True after a read with no status yet failed, so the page can offer a retry.
    private(set) var loadFailed = false
    var showsBatterySheet = false

    private let host: HostID
    private let name: String
    private let tools: any MachineTools
    private var latest = 0
    private var picks = 0

    init(host: HostID, name: String, tools: any MachineTools, status: AwakeStatus?) {
        self.host = host
        self.name = name
        self.tools = tools
        self.status = status
    }

    /// Takes a newer state the machine reported while the page is open. A pick in flight wins;
    /// its answer replaces the state.
    func adopt(_ fresh: AwakeStatus?) {
        guard let fresh, picks == 0 else { return }
        status = fresh
    }

    /// Reads the current state, which may have moved since the page was built. A pick made
    /// while the read ran is newer, so the read's answer is dropped.
    func load() async {
        let token = latest
        loadFailed = false
        guard let fresh = try? await tools.call(host, { try await $0.awakeStatus() }) else {
            loadFailed = status == nil
            return
        }
        if token == latest, picks == 0 { status = fresh }
    }

    func choose(_ mode: AwakeMode) async {
        guard let previous = status, previous.mode != mode else { return }
        latest += 1
        picks += 1
        defer { picks -= 1 }
        let token = latest
        var shown = previous
        shown.mode = mode
        status = shown
        error = nil
        do {
            let result = try await tools.call(host) { try await $0.setAwake(mode: mode, batteryFloor: nil) }
            if token == latest { status = result }
        } catch HelperError.refused(let reason) where reason == "needs_admin_setup" {
            if token == latest { status = previous }
            showsBatterySheet = true
        } catch {
            if token == latest { status = previous }
            self.error = AwakeCopy.changeFailed(name: name)
        }
    }
}

/// The keep-awake section of a machine's page. It owns its model, which needs the tools
/// from the environment.
struct MachineAwakeSection: View {
    let machine: NotificationMachine

    @Environment(\.machineTools) private var tools

    var body: some View {
        Loaded(machine: machine, tools: tools)
    }

    private struct Loaded: View {
        let name: String
        let awake: AwakeStatus?
        @State private var model: MachineAwakeModel

        init(machine: NotificationMachine, tools: any MachineTools) {
            name = machine.name
            awake = machine.awake
            _model = State(
                initialValue: MachineAwakeModel(host: machine.id, name: machine.name, tools: tools, status: machine.awake)
            )
        }

        var body: some View {
            MachineAwakeRows(model: model, name: name)
                .onChange(of: awake) { _, fresh in model.adopt(fresh) }
        }
    }
}

/// The rows themselves, over a model the caller supplies.
struct MachineAwakeRows: View {
    let model: MachineAwakeModel
    let name: String

    var body: some View {
        Section {
            if let status = model.status {
                Picker(
                    AwakeCopy.title,
                    selection: Binding { status.mode } set: { mode in Task { await model.choose(mode) } }
                ) {
                    ForEach(AwakeCopy.choices, id: \.mode) { choice in
                        Text(choice.title).frame(minHeight: 44, alignment: .leading).tag(choice.mode)
                    }
                }
                .pickerStyle(.inline)
                .labelsHidden()
                .accessibilityLabel(AwakeCopy.title)
            } else if model.loadFailed {
                Button(AwakeCopy.retry) { Task { await model.load() } }
                    .frame(minHeight: 44, alignment: .leading)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .accessibilityLabel(AwakeCopy.title)
            }
        } header: {
            // The header is the one view of this section that is always there, so the read and
            // the sheet hang from it rather than from rows that come and go with the status.
            Text(AwakeCopy.title)
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                .textCase(nil)
                .task { await model.load() }
                .sheet(isPresented: Binding(get: { model.showsBatterySheet }, set: { model.showsBatterySheet = $0 })) {
                    AwakeBatterySheet(name: name)
                }
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                if let error = model.error {
                    Label(error, systemImage: "exclamationmark.circle")
                        .foregroundStyle(Color.togoSnow)
                }
                if let status = model.status {
                    let reason = AwakeCopy.reason(status)
                    if !reason.isEmpty {
                        Text(verbatim: reason)
                    }
                }
            }
        }
        .groupedRows()
        // Loading → the choices, or → Try again, and the footer's lines: rows arrive and leave
        // on `resize`.
        .resizesOnRows([
            model.status != nil, model.loadFailed, model.error != nil,
            model.status.map { !AwakeCopy.reason($0).isEmpty } ?? false,
        ])
    }
}
