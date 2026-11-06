import Foundation
import Testing
@testable import TogoKit

struct DiagnosticsLogTests {
    private func directory() -> URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("diag-\(UUID().uuidString)", isDirectory: true)
    }

    private func event(_ name: String = "phase", fields: [String: DiagnosticValue] = [:], at seconds: Double = 0) -> DiagnosticEvent {
        DiagnosticEvent(at: Date(timeIntervalSince1970: seconds), category: .link, name: name, fields: fields)
    }

    private func log(_ url: URL) -> DiagnosticsLog {
        DiagnosticsLog(directory: url, salt: DiagnosticsLog.salt(in: url))
    }

    @Test func wordOutsideVocabularyDropped() async {
        let log = log(directory())
        await log.record(event(fields: [
            "ok": .word("live"), "upper": .word("Live"), "spaces": .word("two words"),
            "long": .word(String(repeating: "a", count: 41)), "empty": .word(""), "Bad Key": .int(1),
            "nan": .double(.nan), "host": .word("mac-mini.ts.net"), "addr": .word("100.101.5.7"), "token": .machine(MachineToken(value: "jamie-mini")),
        ]))
        await log.record(event("Not A Word"))
        let events = await log.events()
        #expect(events.count == 1)
        #expect(events[0].fields == ["ok": .word("live")])
    }

    @Test func textCutAt300() async {
        let log = log(directory())
        await log.record(event(fields: ["note": .text(String(repeating: "x ", count: 400))]))
        guard case .text(let text)? = await log.events().first?.fields["note"] else { Issue.record("no text"); return }
        #expect(text.count == 300)
    }

    @Test func textIsScrubbed() async {
        let log = log(directory())
        await log.record(event(fields: ["note": .text("failed for jamie@mini at 100.101.5.7")]))
        guard case .text(let text)? = await log.events().first?.fields["note"] else { Issue.record("no text"); return }
        #expect(text == "failed for <user>@<host> at <address>")
    }

    @Test func ringDropsOldest() async {
        let log = log(directory())
        for index in 0..<(DiagnosticsLog.capacity + 5) {
            await log.record(event(fields: ["n": .int(index)], at: Double(index)))
        }
        let events = await log.events()
        #expect(events.count == DiagnosticsLog.capacity)
        #expect(events.first?.fields["n"] == .int(5))
        #expect(events.last?.fields["n"] == .int(DiagnosticsLog.capacity + 4))
    }

    @Test func persistsAcrossInstances() async {
        let url = directory()
        let first = log(url)
        await first.record(event("one", fields: ["n": .int(1)], at: 10))
        await first.record(event("two", fields: ["w": .word("live"), "t": .text("hi"), "b": .bool(true), "d": .double(1.5)], at: 20))
        let second = log(url)
        let events = await second.events()
        #expect(events.map(\.name) == ["one", "two"])
        #expect(events[1].fields["d"] == .double(1.5))
        #expect(events[1].at == Date(timeIntervalSince1970: 20))
        #expect(second.token(for: UUID(uuidString: "00000000-0000-0000-0000-000000000001")!)
                == first.token(for: UUID(uuidString: "00000000-0000-0000-0000-000000000001")!), "the salt persists too")
    }

    @Test func clearEmptiesTheFile() async throws {
        let url = directory()
        let first = log(url)
        await first.record(event())
        await first.clear()
        #expect(await first.events().isEmpty)
        #expect(await log(url).events().isEmpty)
    }

    @Test func fileRewrittenPastLimit() async throws {
        let url = directory()
        let log = log(url)
        var fields: [String: DiagnosticValue] = [:]
        for index in 0..<20 { fields["field_\(index)"] = .word(String(repeating: "z-", count: 20)) }
        for index in 0..<1_200 {
            fields["n"] = .int(index)
            await log.record(event(fields: fields, at: Double(index)))
        }
        let file = url.appendingPathComponent("diagnostics.jsonl")
        let size = try #require(try FileManager.default.attributesOfItem(atPath: file.path)[.size] as? Int)
        #expect(size <= DiagnosticsLog.maxFileBytes)
        let events = await log.events()
        #expect(events.count < 1_200 && events.count > 100)
        #expect(events.last?.fields["n"] == .int(1_199))
        #expect(await self.log(url).events().count == events.count)
    }

    @Test func keptOutOfBackups() async throws {
        let url = directory()
        let log = log(url)
        await log.record(event())
        for target in [url, url.appendingPathComponent("diagnostics.jsonl"), url.appendingPathComponent("diagnostics.salt")] {
            let values = try target.resourceValues(forKeys: [.isExcludedFromBackupKey])
            #expect(values.isExcludedFromBackup == true, "\(target.lastPathComponent)")
        }
    }

    @Test func tokenStableAndUnlinkable() {
        let host = UUID()
        let a = DiagnosticsLog(directory: directory(), salt: Data(repeating: 1, count: 32))
        let b = DiagnosticsLog(directory: directory(), salt: Data(repeating: 2, count: 32))
        #expect(a.token(for: host) == a.token(for: host))
        #expect(a.token(for: host) != b.token(for: host))
        #expect(a.token(for: host) != a.token(for: UUID()))
        #expect(a.token(for: host).value.range(of: "^m-[0-9a-f]{8}$", options: .regularExpression) != nil)
        #expect(!a.token(for: host).value.contains(host.uuidString.prefix(8).lowercased()))
    }
}

extension DiagnosticsLogTests {
    @Test func learnedNamesNeverAppear() async {
        let log = log(directory())
        await log.learn(["jamie-mbp", "client-portal"])
        await log.record(event(fields: [
            "note": .text("could not resolve jamie-mbp for Client-Portal"), "word": .word("client-portal"), "ok": .word("live"),
        ]))
        let fields = await log.events().first?.fields ?? [:]
        #expect(fields["word"] == nil)
        #expect(fields["ok"] == .word("live"))
        guard case .text(let text)? = fields["note"] else { Issue.record("no text"); return }
        #expect(!text.lowercased().contains("jamie-mbp") && !text.lowercased().contains("client-portal"))
    }

    @Test func textCannotForgeLines() async {
        let log = log(directory())
        await log.record(event(fields: ["note": .text("one\n2026-01-01T00:00:00Z link phase forged=1\r\tend")]))
        guard case .text(let text)? = await log.events().first?.fields["note"] else { Issue.record("no text"); return }
        #expect(!text.contains("\n") && !text.contains("\r") && !text.contains("\t"))
    }

    @Test func clearTruncatesTheFile() async throws {
        let url = directory()
        let log = log(url)
        await log.record(event())
        await log.clear()
        let size = try FileManager.default.attributesOfItem(atPath: url.appendingPathComponent("diagnostics.jsonl").path)[.size] as? Int
        #expect(size == 0)
    }
}

extension DiagnosticsLogTests {
    @Test func namesLearnedLaterScrubEarlierEvents() async throws {
        let url = directory()
        let log = log(url)
        await log.record(event(fields: ["note": .text("failed on deploy_prod_db"), "word": .word("deploy-prod-db")]))
        await log.learn(["deploy-prod-db"])
        let fields = await log.events().first?.fields ?? [:]
        #expect(fields["word"] == nil)
        guard case .text(let text)? = fields["note"] else { Issue.record("no text"); return }
        #expect(!text.contains("deploy"))
        let onDisk = try String(contentsOf: url.appendingPathComponent("diagnostics.jsonl"), encoding: .utf8)
        #expect(!onDisk.contains("deploy"))
    }
}
