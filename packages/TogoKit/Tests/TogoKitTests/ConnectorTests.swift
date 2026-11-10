import Foundation
import Synchronization
import Testing
@testable import TogoKit

struct ConnectorTests {
    final class Calls: Sendable {
        let kinds = Mutex<[TransportKind]>([])
        func record(_ kind: TransportKind) { kinds.withLock { $0.append(kind) } }
        var all: [TransportKind] { kinds.withLock { $0 } }
    }

    static func profile(
        transport: TransportPreference = .auto, jumps: [HostEndpoint] = [], etAvailable: Bool = true
    ) -> HostProfile {
        HostProfile(
            id: UUID(), name: "h", endpoint: HostEndpoint(host: "h", user: "u"), jumps: jumps,
            auth: .password, transport: transport, etAvailable: etAvailable
        )
    }

    static func request(_ profile: HostProfile) -> AttachRequest {
        AttachRequest(profile: profile, connection: ScriptedConnect.connection(), argv: ["tmux"], cols: 80, rows: 24)
    }

    static func factory(
        _ kind: TransportKind, calls: Calls, makeError: (any Error & Sendable)? = nil,
        connectError: (any Error & Sendable)? = nil,
        unavailable: @escaping @Sendable (HostProfile) -> String? = { _ in nil }
    ) -> TransportFactory {
        TransportFactory(kind: kind, unavailableReason: unavailable) { _ in
            calls.record(kind)
            if let makeError { throw makeError }
            return FakeTransport(connectError: connectError)
        }
    }

    @Test func autoOrderMoshSsh() async throws {
        let calls = Calls()
        let connector = Connector(factories: [
            Self.factory(.ssh, calls: calls), Self.factory(.et, calls: calls), Self.factory(.mosh, calls: calls),
        ])
        let attachment = try await connector.attach(Self.request(Self.profile()))
        #expect(attachment.kind == .mosh)
        #expect(attachment.notes.isEmpty)
        #expect(calls.all == [.mosh])
    }

    @Test func autoFallbackReasons() async throws {
        let calls = Calls()
        let connector = Connector(factories: [
            Self.factory(.mosh, calls: calls, makeError: MoshBootstrapError.serverMissing("sh: not found")),
            Self.factory(.et, calls: calls, unavailable: TransportFactory.etUnavailableReason),
            Self.factory(.ssh, calls: calls),
        ])
        let attachment = try await connector.attach(Self.request(Self.profile(etAvailable: false)))
        #expect(attachment.kind == .ssh)
        #expect(attachment.notes == [
            FallbackNote(kind: .mosh, reason: "Mosh isn't installed on h"),
        ])
    }

    @Test func autoNeverSelectsEt() async throws {
        let calls = Calls()
        let connector = Connector(factories: [
            Self.factory(.mosh, calls: calls, makeError: MoshBootstrapError.serverMissing("sh: not found")),
            Self.factory(.et, calls: calls),
            Self.factory(.ssh, calls: calls),
        ])
        let attachment = try await connector.attach(Self.request(Self.profile(etAvailable: true)))
        #expect(attachment.kind == .ssh)
        #expect(calls.all == [.mosh, .ssh])
    }

    @Test func explicitEtStillAttaches() async throws {
        let calls = Calls()
        let connector = Connector(factories: [Self.factory(.et, calls: calls), Self.factory(.ssh, calls: calls)])
        let attachment = try await connector.attach(Self.request(Self.profile(transport: .et, etAvailable: true)))
        #expect(attachment.kind == .et)
    }

    @Test func udpReplyFailureNamesAddress() async throws {
        let calls = Calls()
        let connector = Connector(factories: [
            Self.factory(.mosh, calls: calls, connectError: MoshConnectError.noUDPReply(address: "100.64.0.5")),
            Self.factory(.ssh, calls: calls),
        ])
        let attachment = try await connector.attach(Self.request(Self.profile()))
        #expect(attachment.kind == .ssh)
        #expect(attachment.notes == [FallbackNote(kind: .mosh, reason: "Mosh got no reply from h; the network may block UDP")])
    }

    @Test func linkLocalOnlyReachFallsBackToSSHQuietly() async throws {
        let calls = Calls()
        let connector = Connector(factories: [
            Self.factory(.mosh, calls: calls, makeError: TransportAddressError.linkLocalOnly(host: "h")),
            Self.factory(.ssh, calls: calls),
        ])
        let attachment = try await connector.attach(Self.request(Self.profile()))
        #expect(attachment.kind == .ssh)
        #expect(attachment.notes == [FallbackNote(kind: .mosh, reason: "Mosh can't use h's link-local IPv6 address")])
    }

    @Test func explicitKindOnALinkLocalOnlyReachUsesSSHWithANote() async throws {
        let calls = Calls()
        let connector = Connector(factories: [
            Self.factory(.et, calls: calls, makeError: TransportAddressError.linkLocalOnly(host: "h")),
            Self.factory(.ssh, calls: calls),
        ])
        let attachment = try await connector.attach(Self.request(Self.profile(transport: .et, etAvailable: true)))
        #expect(attachment.kind == .ssh)
        #expect(attachment.notes == [FallbackNote(kind: .et, reason: "ET can't use h's link-local IPv6 address")])
        #expect(calls.all == [.et, .ssh])
    }

    @Test func failedConnectClosesTransport() async throws {
        let failing = FakeTransport(connectError: MoshConnectError.noUDPReply(address: "a"))
        let connector = Connector(factories: [
            TransportFactory(kind: .mosh, unavailableReason: { _ in nil }) { _ in failing },
            Self.factory(.ssh, calls: Calls()),
        ])
        _ = try await connector.attach(Self.request(Self.profile()))
        #expect(await failing.isClosed)
    }

    @Test func jumpHostSkipsMosh() async throws {
        let calls = Calls()
        let connector = Connector(factories: [
            Self.factory(.mosh, calls: calls, unavailable: TransportFactory.moshUnavailableReason),
            Self.factory(.ssh, calls: calls),
        ])
        let profile = Self.profile(jumps: [HostEndpoint(host: "jump", user: "u")])
        let attachment = try await connector.attach(Self.request(profile))
        #expect(attachment.kind == .ssh)
        #expect(attachment.notes == [FallbackNote(kind: .mosh, reason: "Mosh needs a direct route; this machine uses a jump host")])
        #expect(calls.all == [.ssh])
    }

    @Test func explicitPreferenceNeverFallsBack() async {
        let calls = Calls()
        let connector = Connector(factories: [
            Self.factory(.mosh, calls: calls, connectError: MoshConnectError.noUDPReply(address: "a")),
            Self.factory(.ssh, calls: calls),
        ])
        await #expect(throws: MoshConnectError.noUDPReply(address: "a")) {
            try await connector.attach(Self.request(Self.profile(transport: .mosh)))
        }
        #expect(calls.all == [.mosh])
    }

    @Test func explicitUnavailableKindSaysWhy() async {
        let connector = Connector(factories: [
            Self.factory(.mosh, calls: Calls(), unavailable: TransportFactory.moshUnavailableReason),
        ])
        let profile = Self.profile(transport: .mosh, jumps: [HostEndpoint(host: "jump", user: "u")])
        await #expect(throws: ConnectorError.unavailable("Mosh needs a direct route; this machine uses a jump host")) {
            try await connector.attach(Self.request(profile))
        }
    }

    @Test func autoRethrowsTheLastTransportError() async {
        let connector = Connector(factories: [
            Self.factory(.mosh, calls: Calls(), makeError: MoshConnectError.noUDPReply(address: "100.64.0.5")),
            Self.factory(.ssh, calls: Calls(), connectError: SSHError.channelClosed(exitStatus: nil)),
        ])
        // A transient SSH failure stays itself, so the link retries it as it would for explicit SSH.
        await #expect(throws: SSHError.channelClosed(exitStatus: nil)) {
            _ = try await connector.attach(Self.request(Self.profile(etAvailable: false)))
        }
    }

    @Test func autoWithEveryKindSkippedIsUnavailable() async {
        let connector = Connector(factories: [
            Self.factory(.mosh, calls: Calls(), unavailable: TransportFactory.moshUnavailableReason),
            Self.factory(.et, calls: Calls(), unavailable: TransportFactory.etUnavailableReason),
        ])
        let profile = Self.profile(jumps: [HostEndpoint(host: "j", user: "u")], etAvailable: false)
        await #expect(throws: ConnectorError.unavailable("Mosh needs a direct route; this machine uses a jump host")) {
            _ = try await connector.attach(Self.request(profile))
        }
    }
}
