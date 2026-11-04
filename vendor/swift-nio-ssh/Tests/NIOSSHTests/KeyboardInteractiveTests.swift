//===----------------------------------------------------------------------===//
//
// This source file is part of the SwiftNIO open source project
//
// Copyright (c) 2026 the SwiftNIO project authors
// Licensed under Apache License v2.0
//
// See LICENSE.txt for license information
// See CONTRIBUTORS.txt for the list of SwiftNIO project authors
//
// SPDX-License-Identifier: Apache-2.0
//
//===----------------------------------------------------------------------===//

import NIOCore
import Crypto
import NIOEmbedded
@testable import NIOSSH
import XCTest

/// Two rounds: a password and a one-time code, then an empty round (as OpenSSH's PAM
/// driver sends) before success.
final class ScriptedKeyboardInteractiveServer: NIOSSHServerKeyboardInteractiveDelegate {
    let supportedAuthenticationMethods: NIOSSHAvailableUserAuthenticationMethods = .keyboardInteractive
    private(set) var received: [[String]?] = []

    func requestReceived(request: NIOSSHUserAuthenticationRequest, responsePromise: EventLoopPromise<NIOSSHUserAuthenticationOutcome>) {
        responsePromise.succeed(.failure)
    }

    func keyboardInteractiveReceived(username: String, responses: [String]?, nextStepPromise: EventLoopPromise<NIOSSHKeyboardInteractiveStep>) {
        self.received.append(responses)
        switch responses {
        case nil:
            nextStepPromise.succeed(.challenge(.init(
                name: "login",
                instruction: "",
                prompts: [.init(text: "Password:", echo: false), .init(text: "Verification code:", echo: true)]
            )))
        case ["hunter2", "123456"]:
            nextStepPromise.succeed(.challenge(.init(prompts: [])))
        case []:
            nextStepPromise.succeed(.outcome(.success))
        default:
            nextStepPromise.succeed(.outcome(.failure))
        }
    }
}

final class ScriptedKeyboardInteractiveClient: NIOSSHClientUserAuthenticationDelegate {
    private let answers: [String]
    private var offered = false
    private(set) var challenges: [NIOSSHKeyboardInteractiveChallenge] = []
    private(set) var availableMethodsSeen: [NIOSSHAvailableUserAuthenticationMethods] = []

    init(answers: [String]) {
        self.answers = answers
    }

    func nextAuthenticationType(availableMethods: NIOSSHAvailableUserAuthenticationMethods, nextChallengePromise: EventLoopPromise<NIOSSHUserAuthenticationOffer?>) {
        self.availableMethodsSeen.append(availableMethods)
        guard !self.offered else {
            nextChallengePromise.succeed(nil)
            return
        }
        self.offered = true
        nextChallengePromise.succeed(.init(username: "foo", serviceName: "", offer: .keyboardInteractive(.init())))
    }

    func keyboardInteractiveChallengeReceived(_ challenge: NIOSSHKeyboardInteractiveChallenge, responsePromise: EventLoopPromise<[String]>) {
        self.challenges.append(challenge)
        responsePromise.succeed(challenge.prompts.isEmpty ? [] : self.answers)
    }
}

/// Offers keyboard-interactive once and keeps each challenge's promise, so a test can
/// answer late.
final class HoldingKeyboardInteractiveClient: NIOSSHClientUserAuthenticationDelegate {
    private var offered = false
    private(set) var pending: [EventLoopPromise<[String]>] = []

    func nextAuthenticationType(availableMethods: NIOSSHAvailableUserAuthenticationMethods, nextChallengePromise: EventLoopPromise<NIOSSHUserAuthenticationOffer?>) {
        guard !self.offered else {
            nextChallengePromise.succeed(nil)
            return
        }
        self.offered = true
        nextChallengePromise.succeed(.init(username: "foo", serviceName: "", offer: .keyboardInteractive(.init())))
    }

    func keyboardInteractiveChallengeReceived(_ challenge: NIOSSHKeyboardInteractiveChallenge, responsePromise: EventLoopPromise<[String]>) {
        self.pending.append(responsePromise)
    }
}

final class KeyboardInteractiveTests: XCTestCase {
    private let challenge = SSHMessage.UserAuthInfoRequestMessage(
        name: "", instruction: "", languageTag: "", prompts: [.init(prompt: "Verification code:", echo: true)]
    )

    /// Drives a client user auth state machine to the point where it has sent a
    /// keyboard-interactive request and received one challenge.
    private func clientInChallenge(
        _ delegate: HoldingKeyboardInteractiveClient, loop: EmbeddedEventLoop
    ) throws -> (UserAuthenticationStateMachine, EventLoopFuture<SSHMessage.UserAuthInfoResponseMessage?>) {
        let configuration = SSHClientConfiguration(userAuthDelegate: delegate, serverAuthDelegate: AcceptAllHostKeysDelegate())
        var machine = UserAuthenticationStateMachine(role: .client(configuration), loop: loop, sessionID: ByteBuffer(string: "session"))
        let serviceRequest = try XCTUnwrap(machine.beginAuthentication())
        machine.sendServiceRequest(serviceRequest)
        var request: SSHMessage.UserAuthRequestMessage?
        try machine.receiveServiceAccept(.init(service: "ssh-userauth"))?.whenSuccess { request = $0 }
        loop.run()
        machine.sendUserAuthRequest(try XCTUnwrap(request))
        let answer = try machine.receiveUserAuthInfoRequest(self.challenge)
        return (machine, answer)
    }

    private func value<T>(_ future: EventLoopFuture<T>, _ loop: EmbeddedEventLoop) throws -> T {
        var result: Result<T, Error>?
        future.whenComplete { result = $0 }
        loop.run()
        return try XCTUnwrap(result).get()
    }

    func testLateAnswerAfterFailureIsDropped() throws {
        let loop = EmbeddedEventLoop()
        let delegate = HoldingKeyboardInteractiveClient()
        var (machine, answer) = try self.clientInChallenge(delegate, loop: loop)

        // A hostile server fails the attempt while the user is still typing.
        _ = try machine.receiveUserAuthFailure(.init(authentications: ["keyboard-interactive"], partialSuccess: false))
        loop.run()
        delegate.pending[0].succeed(["123456"])
        let late = try XCTUnwrap(try self.value(answer, loop))

        XCTAssertFalse(machine.sendUserAuthInfoResponse(late))
    }

    func testOnlyAnswerToLatestChallengeIsSent() throws {
        let loop = EmbeddedEventLoop()
        let delegate = HoldingKeyboardInteractiveClient()
        var (machine, first) = try self.clientInChallenge(delegate, loop: loop)
        let second = try machine.receiveUserAuthInfoRequest(self.challenge)

        delegate.pending[0].succeed(["111111"])
        delegate.pending[1].succeed(["222222"])
        let stale = try XCTUnwrap(try self.value(first, loop))
        let current = try XCTUnwrap(try self.value(second, loop))

        XCTAssertFalse(machine.sendUserAuthInfoResponse(stale))
        XCTAssertTrue(machine.sendUserAuthInfoResponse(current))
        XCTAssertFalse(machine.sendUserAuthInfoResponse(current), "a second copy of the same answer is dropped")
    }

    func testConnectionDropsAnswerOutsideUserAuth() throws {
        let configuration = SSHClientConfiguration(userAuthDelegate: HoldingKeyboardInteractiveClient(), serverAuthDelegate: AcceptAllHostKeysDelegate())
        var connection = SSHConnectionStateMachine(role: .client(configuration))
        let allocator = ByteBufferAllocator()
        var buffer = allocator.buffer(capacity: 64)

        XCTAssertNoThrow(try connection.processOutboundMessage(
            .userAuthInfoResponse(.init(responses: ["123456"])), buffer: &buffer, allocator: allocator, loop: EmbeddedEventLoop()
        ))
        XCTAssertEqual(buffer.readableBytes, 0)
    }

    private func run(client: ScriptedKeyboardInteractiveClient, server: ScriptedKeyboardInteractiveServer) throws -> (BackToBackEmbeddedChannel, UserEventExpecter) {
        let channel = BackToBackEmbeddedChannel()
        var harness = TestHarness()
        harness.clientAuthDelegate = client
        harness.serverAuthDelegate = server
        try channel.configureWithHarness(harness)
        let events = UserEventExpecter()
        try channel.client.pipeline.addHandler(events).wait()
        try channel.activate()
        try? channel.interactInMemory()
        return (channel, events)
    }

    func testKeyboardInteractiveTwoRounds() throws {
        let client = ScriptedKeyboardInteractiveClient(answers: ["hunter2", "123456"])
        let server = ScriptedKeyboardInteractiveServer()
        let (channel, events) = try self.run(client: client, server: server)
        defer { try? channel.finish() }

        XCTAssertEqual(client.challenges.map(\.prompts), [
            [.init(text: "Password:", echo: false), .init(text: "Verification code:", echo: true)],
            [],
        ])
        XCTAssertEqual(client.challenges.first?.name, "login")
        XCTAssertEqual(server.received, [nil, ["hunter2", "123456"], []])
        XCTAssertTrue(events.userEvents.contains { $0 is UserAuthSuccessEvent })
    }

    func testKeyboardInteractiveWrongAnswer() throws {
        let client = ScriptedKeyboardInteractiveClient(answers: ["hunter2", "000000"])
        let server = ScriptedKeyboardInteractiveServer()
        let (channel, events) = try self.run(client: client, server: server)
        defer { try? channel.finish() }

        XCTAssertEqual(server.received, [nil, ["hunter2", "000000"]])
        XCTAssertEqual(client.availableMethodsSeen.count, 2, "client should be asked for its next method after the failure")
        XCTAssertEqual(client.availableMethodsSeen.last, .keyboardInteractive)
        XCTAssertFalse(events.userEvents.contains { $0 is UserAuthSuccessEvent })
    }

    func testServerWithoutKeyboardInteractiveRejectsIt() throws {
        let channel = BackToBackEmbeddedChannel()
        defer { try? channel.finish() }
        let client = ScriptedKeyboardInteractiveClient(answers: [])
        var harness = TestHarness()
        harness.clientAuthDelegate = client
        try channel.configureWithHarness(harness)
        try channel.activate()
        try? channel.interactInMemory()

        XCTAssertTrue(client.challenges.isEmpty)
        XCTAssertEqual(client.availableMethodsSeen.count, 2)
    }

    func testInfoRequestRoundTripsAndPKOKStillParses() throws {
        let request = SSHMessage.userAuthInfoRequest(.init(
            name: "n", instruction: "i", languageTag: "",
            prompts: [.init(prompt: "Code:", echo: true)]
        ))
        var buffer = ByteBufferAllocator().buffer(capacity: 64)
        buffer.writeSSHMessage(request)
        XCTAssertEqual(try buffer.readSSHMessage(), request)

        let pkOK = SSHMessage.userAuthPKOK(.init(key: NIOSSHPrivateKey(ed25519Key: .init()).publicKey))
        buffer.clear()
        buffer.writeSSHMessage(pkOK)
        XCTAssertEqual(try buffer.readSSHMessage(), pkOK)

        let response = SSHMessage.userAuthInfoResponse(.init(responses: ["a", "b"]))
        buffer.clear()
        buffer.writeSSHMessage(response)
        XCTAssertEqual(try buffer.readSSHMessage(), response)
    }
}
