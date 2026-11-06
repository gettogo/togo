import Foundation
import Testing
import TogoKit
import TogoTerminal
@testable import Togo

@MainActor struct DiagnosticsHookTests {
    private struct Refused: LocalizedError {
        var errorDescription: String? {
            "Could not reach mac-mini.tail1234.ts.net (100.101.5.7) as jamie@mini; key ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJkGq8c3FfE0Zl1xqf2dQ"
        }
    }

    private let secrets = [
        "mac-mini", "tail1234", "100.101.5.7", "101.5.7", "fe80::1", "jamie", "/Users", "client-portal-rewrite", "Studio Secret",
        "fix the login bug", "AAAAC3Nz", "acme-clients", "Zm9vYmFyYmF6cXV4MTIzNDU2",
    ]

    private func eventually(_ condition: () async -> Bool) async {
        for _ in 0..<400 where await !condition() {
            try? await Task.sleep(for: .milliseconds(10))
        }
    }

    @Test func exportContainsNoFixtureSecrets() async throws {
        let now = Samples.now
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("hooks-\(UUID().uuidString)")
        let recorder = DiagnosticsRecorder(directory: directory)
        let watches = Watches()
        let services = LiveServices(
            hostRecords: SyncedRecordStore(prefix: "host.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { now }),
            settingsRecords: SyncedRecordStore(prefix: "settings.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { now }),
            syncSwitch: SyncSwitch(backend: MemoryBackend()),
            dependencies: LiveServices.Dependencies(
                watch: { record in
                    let watch = FakeWatch(record: record)
                    watches.made.withLock { $0[record.id] = watch }
                    return watch
                },
                helpers: NoHelper(),
                connect: { _, _, _, _ in { _, _, _ in throw Refused() } },
                now: { now },
                diagnostics: recorder
            )
        )
        await services.start()
        let id = Samples.studio
        try await services.saveHost(HostRecord(
            id: id,
            profile: HostProfile(
                id: id, name: "Studio Secret", endpoint: HostEndpoint(host: "mac-mini.tail1234.ts.net", user: "jamie"), auth: .password
            ),
            symbol: "desktop"
        ))
        let watch = try #require(watches[id])
        let session = Session(
            id: "client-portal-rewrite", bib: "01", name: "client-portal-rewrite", agent: "claude", cwd: "/Users/jamie/Dev/acme-clients",
            createdAt: now, status: .needsYou, activity: "fix the login bug in prod", attachedClients: 0, managed: true
        )
        let reaches: [HostSnapshot.Reach] = [
            .connecting, .offline("ssh: connect to host mac-mini.tail1234.ts.net port 22: 100.101.5.7 fe80::1%en0 ::ffff:100.101.5.7 refused"),
            .failed("jamie@mini rejected Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MC1hYmNkZWZnaGk"), .online,
        ]
        for reach in reaches {
            watch.sink.yield(HostSnapshot(
                hostID: id, reach: reach, helper: .present(version: "0.3.0"), sessions: [session], lastSeen: now,
                capabilities: [Capability.events, Capability.upload]
            ))
        }
        let handle = services.terminal(hostID: id, sessionID: session.id, transport: .ssh, userStarted: true)
        await eventually { await recorder.log.events().contains { $0.category == .link } }
        handle.stop()

        let report = await recorder.report(settings: services.settings)
        let events = report.events
        #expect(events.contains { $0.name == "reachability" && $0.fields["state"] == .word("offline") })
        #expect(events.contains { $0.name == "reachability" && $0.fields["state"] == .word("online") })
        #expect(events.contains { $0.name == "phase" && $0.fields["phase"] == .word("failed") })
        #expect(events.contains { $0.name == "helper" && $0.fields["version"] == .word("0.3.0") })
        #expect(report.machines.count == 1)

        let json = String(decoding: try report.json(), as: UTF8.self)
        let text = report.text()
        for secret in secrets {
            #expect(!json.contains(secret), "json has \(secret)")
            #expect(!text.contains(secret), "text has \(secret)")
        }
        let onDisk = try String(contentsOf: directory.appendingPathComponent("diagnostics.jsonl"), encoding: .utf8)
        for secret in secrets { #expect(!onDisk.contains(secret), "log file has \(secret)") }
    }

    @Test func reachabilityIsRecordedOnlyWhenItChanges() async {
        let recorder = DiagnosticsRecorder(directory: FileManager.default.temporaryDirectory.appendingPathComponent("hooks-\(UUID().uuidString)"))
        let id = Samples.studio
        let machine = HostRecord(
            id: id, profile: HostProfile(id: id, name: "Studio", endpoint: HostEndpoint(host: "studio.local", user: "sam"), auth: .password),
            symbol: "desktop"
        )
        for _ in 0..<3 { recorder.reached(HostSnapshot(hostID: id, reach: .online), record: machine) }
        recorder.reached(HostSnapshot(hostID: id, reach: .offline("x")), record: machine)
        await recorder.settle()
        let states = await recorder.log.events().filter { $0.name == "reachability" }.map(\.fields["state"])
        #expect(states == [.word("online"), .word("offline")])
    }

    @Test func errorCodesCarryNoDescription() {
        let code = DiagnosticsRecorder.code(for: Refused())
        #expect(DiagnosticValue.isWord(code))
        #expect(!code.contains("mac-mini") && !code.contains("jamie"))
    }

    @Test func fileNameCarriesTheDate() {
        let date = Date(timeIntervalSince1970: 1_800_000_000)
        #expect(DiagnosticsView.fileName(on: date).hasPrefix("togo-diagnostics-20"))
        #expect(DiagnosticsView.fileName(on: date).hasSuffix(".json"))
    }
}

extension DiagnosticsHookTests {
    @Test func helperVersionsOutsideReleaseShapeAreUnknown() async {
        let recorder = DiagnosticsRecorder(directory: FileManager.default.temporaryDirectory.appendingPathComponent("hooks-\(UUID().uuidString)"))
        let id = Samples.studio
        let machine = HostRecord(
            id: id, profile: HostProfile(id: id, name: "Studio", endpoint: HostEndpoint(host: "studio.local", user: "sam"), auth: .password),
            symbol: "desktop"
        )
        recorder.reached(HostSnapshot(hostID: id, reach: .online, helper: .present(version: "0.3.0-jamie-mbp")), record: machine)
        await recorder.settle()
        let helper = await recorder.log.events().first { $0.name == "helper" }
        #expect(helper?.fields["version"] == nil)
    }

    @Test func pushOutcomesAreRecordedOnlyOnChange() async {
        let recorder = DiagnosticsRecorder(directory: FileManager.default.temporaryDirectory.appendingPathComponent("hooks-\(UUID().uuidString)"))
        for _ in 0..<4 { recorder.push("authorized") }
        recorder.push("token_registered")
        await recorder.settle()
        #expect(await recorder.log.events().count == 2)
    }

    @Test func burstFrameStatsAreNumbersOnly() async {
        let recorder = DiagnosticsRecorder(directory: FileManager.default.temporaryDirectory.appendingPathComponent("hooks-\(UUID().uuidString)"))
        var pacing = FrameStats()
        for frame in 0..<120 { pacing.record(timestamp: Double(frame) / 120, targetTimestamp: Double(frame + 1) / 120) }
        recorder.frames(
            TerminalBinding.Burst(frames: 240, bytes: 4 << 20, p95Work: .microseconds(3_456), maxWork: .microseconds(5_001)),
            pacing: pacing
        )
        await recorder.settle()
        let event = await recorder.log.events().first { $0.name == "frames" }
        #expect(event?.category == .perf)
        #expect(event?.fields["frames"] == .int(240))
        #expect(event?.fields["bytes"] == .int(4 << 20))
        #expect(event?.fields["work_p95_ms"] == .double(3.46))
        #expect(event?.fields["work_max_ms"] == .double(5))
        #expect(event?.fields["dropped"] == .double(0))
        let words = event?.fields.values.contains { if case .int = $0 { false } else if case .double = $0 { false } else { true } }
        #expect(words == false, "frame stats carry only numbers")
    }

    /// iOS can start the process before the tap to warm it up; that launch time counts the wait
    /// for the tap, so it is left out rather than reported as a slow launch.
    @Test func prewarmedLaunchIsNotRecorded() async {
        let recorder = DiagnosticsRecorder(directory: FileManager.default.temporaryDirectory.appendingPathComponent("hooks-\(UUID().uuidString)"))
        recorder.recordLaunch(environment: ["ActivePrewarm": "1"])
        await recorder.settle()
        #expect(await recorder.log.events().allSatisfy { $0.name != "launch" })
        recorder.recordLaunch(environment: [:])
        await recorder.settle()
        #expect(await recorder.log.events().contains { $0.name == "launch" })
    }

    @Test func launchIsRecordedOnlyWhenAsked() async {
        let recorder = DiagnosticsRecorder(directory: FileManager.default.temporaryDirectory.appendingPathComponent("hooks-\(UUID().uuidString)"))
        await recorder.settle()
        #expect(await recorder.log.events().isEmpty, "making the recorder records nothing; the first screen records the launch")
        recorder.recordLaunch()
        await recorder.settle()
        let launch = await recorder.log.events().first { $0.name == "launch" }
        guard case .int(let ms)? = launch?.fields["ms"] else {
            Issue.record("no launch time")
            return
        }
        #expect(ms > 0)
    }

    @Test func clearForgetsMachines() async {
        let recorder = DiagnosticsRecorder(directory: FileManager.default.temporaryDirectory.appendingPathComponent("hooks-\(UUID().uuidString)"))
        let id = Samples.studio
        let machine = HostRecord(
            id: id, profile: HostProfile(id: id, name: "Studio", endpoint: HostEndpoint(host: "studio.local", user: "sam"), auth: .password),
            symbol: "desktop"
        )
        recorder.reached(HostSnapshot(hostID: id, reach: .online), record: machine)
        await recorder.clear()
        let report = await recorder.report(settings: AppSettings())
        #expect(report.machines.isEmpty && report.events.isEmpty)
    }
}

extension DiagnosticsHookTests {
    private func linkRecorder() -> DiagnosticsRecorder {
        DiagnosticsRecorder(directory: FileManager.default.temporaryDirectory.appendingPathComponent("hooks-\(UUID().uuidString)"))
    }

    private func phases(_ recorder: DiagnosticsRecorder) async -> [DiagnosticEvent] {
        await recorder.settle()
        return await recorder.log.events().filter { $0.category == .link }
    }

    @Test func reconnectPhasesRecorded() async {
        let recorder = linkRecorder()
        let observe = recorder.linkStates(host: Samples.studio, preference: .auto)
        observe(.live(.mosh, notes: []))
        observe(.roaming)
        observe(.reconnecting(attempt: 1))
        observe(.reconnecting(attempt: 2))
        let events = await phases(recorder)
        #expect(events.map(\.fields["phase"]) == [.word("roaming"), .word("reconnecting"), .word("reconnecting")])
        #expect(events.map(\.fields["attempt"]) == [nil, .int(1), .int(2)])
        #expect(events.allSatisfy { $0.fields["transport"] == .word(TransportKind.mosh.rawValue) && $0.fields["machine"] != nil })
    }

    @Test func endedRecordedOnce() async {
        let recorder = linkRecorder()
        let observe = recorder.linkStates(host: Samples.studio, preference: .ssh)
        observe(.reconnecting(attempt: 1))
        observe(.ended)
        observe(.ended)
        let events = await phases(recorder)
        #expect(events.filter { $0.fields["phase"] == .word("ended") }.count == 1)
    }

    @Test func noFreeTextFromLinkErrors() async {
        let recorder = linkRecorder()
        let observe = recorder.linkStates(host: Samples.studio, preference: .auto)
        observe(.waking("mac-mini.tail1234.ts.net"))
        observe(.failed("jamie@mini refused 100.101.5.7 /Users/jamie/Dev"))
        observe(.reconnecting(attempt: 3))
        observe(.ended)
        let events = await phases(recorder)
        let dump = String(decoding: (try? JSONEncoder().encode(events)) ?? Data(), as: UTF8.self)
        for secret in ["mac-mini", "jamie", "100.101.5.7", "/Users"] { #expect(!dump.contains(secret)) }
        #expect(!events.contains { $0.fields["phase"] == .word("failed") || $0.fields["phase"] == .word("waking") })
    }
}
