import Foundation
import Testing
@testable import TogoKit

struct DiagnosticsReportTests {
    private let app = AppFacts(version: "1.0", build: "42", osVersion: "27.2", model: "iPhone19,4", locale: "en_US")

    @Test func exportContainsNoFixtureSecrets() async throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("diag-\(UUID().uuidString)")
        let log = DiagnosticsLog(directory: url, salt: DiagnosticsLog.salt(in: url))
        let secrets = [
            "mac-mini.tail1234.ts.net", "100.101.5.7", "fe80::1%en0", "jamie@mini", "/Users/jamie/Dev/togo",
            "AAAAC3NzaC1lZDI1NTE5AAAAIJkGq8c3FfE0Zl1xqf2dQ", "fix the login bug in prod",
            "Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MC1hYmNkZWZnaGk", "~/clients/acme",
        ]
        // Free text from a failing connection, as an error description would carry it.
        await log.record(DiagnosticEvent(category: .link, name: "failed", fields: [
            "reason": .text("Could not reach mac-mini.tail1234.ts.net (100.101.5.7, fe80::1%en0) as jamie@mini in /Users/jamie/Dev/togo"),
            "prompt": .word("fix the login bug in prod"),
            "key": .text("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJkGq8c3FfE0Zl1xqf2dQ jamie@mini"),
            "token": .text("Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MC1hYmNkZWZnaGk"), "dir": .text("~/clients/acme"),
        ]))
        let report = DiagnosticsReport(
            app: app, settings: DiagnosticsReport.summary(of: AppSettings()),
            machines: [MachineFacts(token: log.token(for: UUID()), helperVersion: "0.3.1", capabilities: ["push", "mac-mini.tail1234.ts.net"], transport: "jamie@mini")],
            events: await log.events()
        )
        let json = String(decoding: try report.json(), as: UTF8.self)
        let text = report.text()
        for secret in secrets {
            #expect(!json.contains(secret), "json has \(secret)")
            #expect(!text.contains(secret), "text has \(secret)")
        }
        #expect(json.contains("<host>") && text.contains("<address>"))
    }

    @Test func settingsSummaryHasNoFreeText() throws {
        var settings = AppSettings()
        settings.fontName = "Secret Font"
        settings.themeID = "client-theme"
        settings.relayURL = URL(string: "https://relay.corp.example/x")
        settings.dictationLocale = "pt-BR"
        settings.whisperModel = "base.en-q5"
        settings.keyBar = Data([1, 2, 3])
        let summary = DiagnosticsReport.summary(of: settings)
        for value in summary.values {
            if case .text = value { Issue.record("free text in summary") }
            #expect(value.sanitized == value)
        }
        let text = String(decoding: try JSONEncoder().encode(summary), as: UTF8.self)
        for leaked in ["Secret Font", "client-theme", "corp", "pt-BR", "base.en"] { #expect(!text.contains(leaked)) }
        #expect(summary["custom_relay"] == .bool(true))
    }

    @Test func jsonRoundTrips() throws {
        let event = DiagnosticEvent(
            at: Date(timeIntervalSince1970: 100), category: .perf, name: "launch", fields: ["ms": .int(420), "cold": .bool(true)]
        )
        let report = DiagnosticsReport(app: app, settings: ["a": .int(1)], machines: [], events: [event])
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let back = try decoder.decode(DiagnosticsReport.self, from: report.json())
        #expect(back.events == [event])
        #expect(back.app == app)
    }

    @Test func textListsEventsOldestFirst() {
        let events = [
            DiagnosticEvent(at: Date(timeIntervalSince1970: 0), category: .link, name: "live", fields: ["transport": .word("ssh")]),
            DiagnosticEvent(at: Date(timeIntervalSince1970: 60), category: .machine, name: "offline"),
        ]
        let text = DiagnosticsReport(app: app, settings: [:], machines: [], events: events).text()
        #expect(text.contains("1970-01-01T00:00:00Z link live transport=ssh"))
        #expect(text.range(of: "link live")!.lowerBound < text.range(of: "machine offline")!.lowerBound)
    }

    @Test func machineCapabilitiesListOnlyKnownNames() {
        let facts = MachineFacts(
            token: MachineToken(value: "m-00000000"), helperVersion: "0.3.0",
            capabilities: ["events.v1", "push.v1", "jamie-mbp.v1", "mystery"], transport: "ssh"
        )
        #expect(facts.capabilities == ["events.v1", "push.v1"])
        #expect(facts.otherCapabilities == 2)
    }

    @Test func factsOutsideTheirShapeBecomeUnknown() {
        let facts = AppFacts(version: "jamie's iPhone", build: "1", osVersion: "27", model: "iPhone19,4", locale: "en_US")
        #expect(facts.version == "unknown")
        #expect(facts.model == "iPhone19,4")
    }
}
