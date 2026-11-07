import Observation
import SwiftUI
import TogoKit

/// The words of a machine's agent integrations.
enum IntegrationsCopy {
    static var title: String { settingsText("settings.integrations.title", "Agent integrations") }

    static var footer: String {
        settingsText(
            "settings.integrations.footer",
            "Set up lets an agent's chat, requests and status reach this device. The helper backs up each file before changing it; removing the setup puts it back."
        )
    }

    static var noSetupTitle: String { settingsText("settings.integrations.noSetup", "No setup needed") }
    static var setUp: String { settingsText("settings.integrations.setUp", "Set up") }
    static var update: String { settingsText("settings.integrations.update", "Update") }
    static var setUpAll: String { settingsText("settings.integrations.setUpAll", "Set up all") }
    static var later: String { settingsText("settings.integrations.later", "Later") }
    static var remove: String { settingsText("settings.integrations.remove", "Remove setup") }
    static var retry: String { settingsText("settings.integrations.retry", "Couldn't read the integrations. Try again.") }
    static var unsupported: String {
        settingsText("settings.integrations.unsupported", "Update the Togo helper on this machine to set up agents from here.")
    }

    static func removeTitle(_ name: String) -> String {
        settingsText("settings.integrations.remove.title", "Remove \(name)'s setup?")
    }

    static var removeMessage: String {
        settingsText(
            "settings.integrations.remove.message",
            "Its chat and requests stop reaching this device. The terminal keeps working."
        )
    }

    static func failed(_ name: String) -> String {
        settingsText("settings.integrations.failed", "Couldn't set up \(name). Try again.")
    }

    /// The state word on a row.
    static func word(_ state: IntegrationState) -> String {
        switch state {
        case .ready: settingsText("settings.integrations.ready", "Ready")
        case .needsSetup, .unknown: settingsText("settings.integrations.needsSetup", "Needs setup")
        case .outdated: settingsText("settings.integrations.outdated", "Outdated")
        case .notNeeded: settingsText("settings.integrations.notNeeded", "Not needed")
        case .notSupported: settingsText("settings.integrations.notSupported", "Not supported yet")
        case .notInstalled: settingsText("settings.integrations.notInstalled", "Not installed")
        }
    }

    /// A line under a ready row: whether anything has arrived yet.
    static func detail(_ report: IntegrationReport) -> String? {
        if report.statusLine == "chained" {
            return settingsText("settings.integrations.chained", "Your own status line still runs")
        }
        if report.state == .ready, report.lastHookAt == nil {
            return settingsText("settings.integrations.waiting", "Starts with its next session")
        }
        return nil
    }
}

/// One machine's agent integrations: which installed harnesses need setup, and the helper's
/// installer behind one tap per row or all at once.
@MainActor @Observable final class IntegrationsModel {
    enum Phase: Equatable {
        case loading
        case loaded
        case failed
        /// The machine's helper has no integrations.v1.
        case unsupported
    }

    private(set) var phase: Phase = .loading
    private(set) var reports: [IntegrationReport] = []
    /// Harness ids with an install or uninstall in flight.
    private(set) var busy: Set<String> = []
    /// A failed change's message, per harness id.
    private(set) var failures: [String: String] = [:]

    private let host: HostID
    private let tools: any MachineTools

    init(host: HostID, tools: any MachineTools, reports: [IntegrationReport]? = nil) {
        self.host = host
        self.tools = tools
        if let reports {
            self.reports = reports
            phase = .loaded
        }
    }

    /// Installed harnesses that take hooks, in the helper's order.
    var setupRows: [IntegrationReport] {
        reports.filter { $0.installed && Self.takesSetup($0) }
    }

    /// Installed harnesses that need nothing (or whose hooks aren't confirmed yet).
    var notNeededRows: [IntegrationReport] {
        reports.filter { $0.installed && !Self.takesSetup($0) }
    }

    /// Rows a Set up all would install.
    var needing: [IntegrationReport] {
        setupRows.filter { Self.needsAction($0.state) }
    }

    var showsSetUpAll: Bool { needing.count > 1 }

    var isEmpty: Bool { setupRows.isEmpty && notNeededRows.isEmpty }

    static func takesSetup(_ report: IntegrationReport) -> Bool {
        if case .hooks = report.setup { return true }
        return false
    }

    static func needsAction(_ state: IntegrationState) -> Bool {
        switch state {
        case .needsSetup, .outdated, .unknown: true
        case .ready, .notNeeded, .notSupported, .notInstalled: false
        }
    }

    func load() async {
        do {
            let status = try await tools.call(host) { try await $0.integrationsStatus() }
            reports = status.integrations
            phase = .loaded
        } catch HelperError.unsupported {
            phase = .unsupported
        } catch {
            if reports.isEmpty { phase = .failed }
        }
    }

    func setUp(_ id: String) async {
        await change([id], install: true)
    }

    func setUpAll() async {
        await change(needing.map(\.id), install: true)
    }

    func uninstall(_ id: String) async {
        await change([id], install: false)
    }

    private func change(_ ids: [String], install: Bool) async {
        let ids = ids.filter { !busy.contains($0) }
        guard !ids.isEmpty else { return }
        busy.formUnion(ids)
        for id in ids { failures[id] = nil }
        defer { busy.subtract(ids) }
        do {
            let result = try await tools.call(host) { client in
                install ? try await client.installIntegrations(ids) : try await client.uninstallIntegrations(ids)
            }
            reports = result.integrations
            for outcome in result.results where outcome.state == "refused" {
                failures[outcome.id] = outcome.error.map(Self.firstLine) ?? IntegrationsCopy.failed(name(outcome.id))
            }
        } catch {
            for id in ids { failures[id] = IntegrationsCopy.failed(name(id)) }
        }
    }

    private func name(_ id: String) -> String {
        reports.first { $0.id == id }?.name ?? id
    }

    private static func firstLine(_ text: String) -> String {
        text.split(whereSeparator: \.isNewline).first.map(String.init) ?? text
    }
}
