import Foundation
import Testing
@testable import TogoKit

struct ErrorTextTests {
    static let errors: [any Error] = [
        SSHError.hostKeyChanged(host: "[h]:22", expected: ["SHA256:old"], presented: "SHA256:new"),
        SSHError.hostKeyRejected,
        SSHError.authFailed(tried: ["publickey", "password"]),
        SSHError.authFailed(tried: []),
        SSHError.cancelled,
        SSHError.unreachable("h did not answer for 30 s; it may be asleep"),
        SSHError.timeout,
        SSHError.channelClosed(exitStatus: nil),
        SSHError.channelClosed(exitStatus: 1),
        MoshBootstrapError.serverMissing("sh: mosh-server: not found"),
        MoshBootstrapError.noConnectLine(""),
        MoshBootstrapError.malformedKey,
        MoshConnectError.noUDPReply(address: "100.64.0.5"),
        HelperError.rpc(RPCErrorObject(code: RPCErrorCode.notFound, message: "not_found")),
        HelperError.rpc(RPCErrorObject(code: RPCErrorCode.tmuxUnavailable, message: "tmux_unavailable")),
        HelperError.rpc(RPCErrorObject(code: -1, message: "weird_thing")),
        HelperError.timeout(method: "session.list"),
        HelperError.closed,
        HelperError.notReady,
        HelperError.unsupported(capability: "capture.v1"),
        HelperError.malformedResponse,
        HelperError.conflict("already_answered"),
        HelperError.partialWrite(changed: ["App.swift"]),
        HelperError.racedCommit(hash: String(repeating: "a", count: 40)),
        HelperError.rpc(RPCErrorObject(code: RPCErrorCode.invalidParams, message: "invalid params")),
        HelperError.tooLarge(800_000),
        HelperError.refused("needs_admin_setup"),
        HelperError.refused("other"),
        HelperError.conflict("running"),
        HelperError.conflict("shell"),
        HelperError.conflict("ended"),
        HelperError.conflict("needs_you"),
        HelperError.conflict("symlink"),
        HelperError.conflict("missing"),
        HelperError.conflict("not_regular"),
        HelperError.refused(""),
        HelperError.rpc(RPCErrorObject(code: RPCErrorCode.refused, message: "refused")),
        HostKeyStoreError.unreadable("/var/mobile/known_hosts.json"),
        KeyError.badPassphrase,
        KeyError.unsupportedKeyType("ssh-dss"),
        KeyError.malformed,
    ]

    @Test(arguments: errors.indices)
    func errorsReadAsPlainSentences(_ index: Int) {
        let text = Self.errors[index].localizedDescription
        #expect(!text.contains("couldn’t be completed") && !text.contains("couldn't be completed"), "\(text)")
        #expect(!text.contains("\u{2014}"), "\(text)")
        #expect(!text.contains("-32"), "\(text)")
        #expect(!text.contains("_"), "\(text)")
        #expect(text.hasSuffix("."), "\(text)")
        #expect(!text.contains("TogoKit.") && !text.contains("error 0"), "\(text)")
    }

    @Test func changedHostKeyShowsBothFingerprints() {
        let error = SSHError.hostKeyChanged(host: "[box.local]:22", expected: ["SHA256:trustedAAA"], presented: "SHA256:newBBB")
        let text = error.localizedDescription
        #expect(text.contains("SHA256:trustedAAA"))
        #expect(text.contains("SHA256:newBBB"))
        #expect(text.contains("box.local"))
        #expect(!text.contains("[box.local]"))
    }

    @Test func moshFailuresSayWhy() {
        #expect(MoshBootstrapError.serverMissing("x").localizedDescription.contains("mosh-server was not found"))
        #expect(MoshConnectError.noUDPReply(address: "10.0.0.2").localizedDescription.contains("UDP"))
        #expect(MoshConnectError.noUDPReply(address: "10.0.0.2").localizedDescription.contains("10.0.0.2"))
    }

    @Test func goneSessionSaysEnded() {
        let error = HelperError.rpc(RPCErrorObject(code: RPCErrorCode.notFound, message: "not_found"))
        #expect(error.localizedDescription == "This session has ended.")
    }
}
