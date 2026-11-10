import Foundation

public struct AppFacts: Codable, Equatable, Sendable {
    public var version: String
    public var build: String
    public var osVersion: String
    /// The hardware model, such as "iPhone19,4".
    public var model: String
    public var locale: String

    public init(version: String, build: String, osVersion: String, model: String, locale: String) {
        self.version = DiagnosticsReport.fact(version)
        self.build = DiagnosticsReport.fact(build)
        self.osVersion = DiagnosticsReport.fact(osVersion)
        self.model = DiagnosticsReport.fact(model)
        self.locale = DiagnosticsReport.fact(locale)
    }
}

public struct MachineFacts: Codable, Equatable, Sendable {
    public var token: MachineToken
    public var helperVersion: String
    /// Only capability names this app knows; the number of others is kept as a count.
    public var capabilities: [String]
    public var otherCapabilities: Int
    public var transport: String

    private static let knownCapabilities: Set<String> = [
        Capability.events, Capability.approvals, Capability.mute, Capability.upload, Capability.git,
        Capability.gitWrite, Capability.ports, Capability.push, Capability.capture,
    ]

    public init(token: MachineToken, helperVersion: String, capabilities: [String], transport: String) {
        self.token = token
        self.helperVersion = DiagnosticsReport.word(helperVersion)
        let known = capabilities.filter(Self.knownCapabilities.contains)
        self.capabilities = known.sorted()
        otherCapabilities = capabilities.count - known.count
        self.transport = DiagnosticsReport.word(transport)
    }
}

/// What a person can send when something is wrong: facts about the app and the machines by
/// token, a summary of settings, and the recorded events. Built only from typed values.
public struct DiagnosticsReport: Codable, Sendable {
    public var app: AppFacts
    public var settings: [String: DiagnosticValue]
    public var machines: [MachineFacts]
    public var events: [DiagnosticEvent]

    public init(app: AppFacts, settings: [String: DiagnosticValue], machines: [MachineFacts], events: [DiagnosticEvent]) {
        self.app = app
        var kept: [String: DiagnosticValue] = [:]
        for (key, value) in settings where DiagnosticValue.isWord(key) {
            if let clean = value.sanitized { kept[key] = clean }
        }
        self.settings = kept
        self.machines = machines
        self.events = events.compactMap(\.sanitized)
    }

    public func json() throws -> Data {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        return try encoder.encode(self)
    }

    public func text() -> String {
        var lines = ["Togo diagnostics", ""]
        lines.append("app \(app.version) (\(app.build))")
        lines.append("os \(app.osVersion)")
        lines.append("model \(app.model)")
        lines.append("locale \(app.locale)")
        if !settings.isEmpty {
            lines.append("")
            lines.append("settings")
            for key in settings.keys.sorted() { lines.append("  \(key) \(settings[key]?.display ?? "")") }
        }
        if !machines.isEmpty {
            lines.append("")
            lines.append("machines")
            for machine in machines {
                let capabilities = machine.capabilities.isEmpty ? "none" : machine.capabilities.joined(separator: ",")
                lines.append("  \(machine.token.value) helper \(machine.helperVersion) transport \(machine.transport) capabilities \(capabilities)")
            }
        }
        lines.append("")
        lines.append("events \(events.count)")
        let stamp = ISO8601DateFormatter()
        for event in events {
            let fields = event.fields.keys.sorted().map { "\($0)=\(event.fields[$0]?.display ?? "")" }
            lines.append(([stamp.string(from: event.at), event.category.rawValue, event.name] + fields).joined(separator: " "))
        }
        return lines.joined(separator: "\n") + "\n"
    }

    /// Settings as typed values only. Names, paths, addresses and themes the person typed or
    /// imported are reduced to whether they are set.
    public static func summary(of settings: AppSettings) -> [String: DiagnosticValue] {
        return [
            "font_size": .double(settings.fontSize),
            "cursor": DiagnosticValue.word(lowercasing: settings.cursor) ?? .word("other"),
            "transport": .word(settings.transport.rawValue),
            "sync": .bool(settings.syncEnabled),
            "resume_lock": .bool(settings.resumeLock),
            "live_activities": .bool(settings.liveActivities),
            "approve_without_unlock": .bool(settings.approveWithoutUnlock),
            "dictation_engine": .word(settings.dictationEngine.rawValue),
            "custom_relay": .bool(settings.relayURL != nil),
            "custom_key_bar": .bool(settings.keyBar != nil),
            "imported_themes": .bool(settings.importedThemes != nil),
            "dictation_language_set": .bool(settings.dictationLocale != nil),
            "whisper_model_set": .bool(settings.whisperModel != nil),
            "command_text_machines": .int(settings.liveActivityCommandMachines.count),
        ]
    }

    static func fact(_ raw: String) -> String {
        raw.range(of: #"^[A-Za-z0-9_.,:-]{1,40}$"#, options: .regularExpression) != nil ? raw : "unknown"
    }

    static func word(_ raw: String) -> String {
        if case .word(let word)? = DiagnosticValue.word(lowercasing: raw) { return word }
        return "unknown"
    }
}
