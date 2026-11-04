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

/// One round of keyboard-interactive authentication (RFC 4256): the server's
/// SSH_MSG_USERAUTH_INFO_REQUEST.
public struct NIOSSHKeyboardInteractiveChallenge: Hashable, Sendable {
    public struct Prompt: Hashable, Sendable {
        public var text: String
        /// Whether the answer may be shown while it is typed.
        public var echo: Bool

        public init(text: String, echo: Bool) {
            self.text = text
            self.echo = echo
        }
    }

    public var name: String
    public var instruction: String
    public var prompts: [Prompt]

    public init(name: String = "", instruction: String = "", prompts: [Prompt]) {
        self.name = name
        self.instruction = instruction
        self.prompts = prompts
    }
}

/// What a server does next during keyboard-interactive authentication.
public enum NIOSSHKeyboardInteractiveStep {
    case challenge(NIOSSHKeyboardInteractiveChallenge)
    case outcome(NIOSSHUserAuthenticationOutcome)
}

/// A server user auth delegate that also accepts keyboard-interactive authentication.
///
/// Include ``NIOSSHAvailableUserAuthenticationMethods/keyboardInteractive`` in
/// `supportedAuthenticationMethods` to advertise it.
public protocol NIOSSHServerKeyboardInteractiveDelegate: NIOSSHServerUserAuthenticationDelegate {
    /// Called with `responses == nil` when the client starts keyboard-interactive, then once
    /// per SSH_MSG_USERAUTH_INFO_RESPONSE with the answers to the last challenge.
    func keyboardInteractiveReceived(
        username: String,
        responses: [String]?,
        nextStepPromise: EventLoopPromise<NIOSSHKeyboardInteractiveStep>
    )
}

extension NIOSSHClientUserAuthenticationDelegate {
    public func keyboardInteractiveChallengeReceived(
        _ challenge: NIOSSHKeyboardInteractiveChallenge,
        responsePromise: EventLoopPromise<[String]>
    ) {
        responsePromise.fail(
            NIOSSHError.protocolViolation(
                protocolName: "userauth",
                violation: "keyboard-interactive offered by a delegate that cannot answer prompts"
            )
        )
    }
}

extension NIOSSHKeyboardInteractiveChallenge {
    init(_ message: SSHMessage.UserAuthInfoRequestMessage) {
        self.init(
            name: message.name,
            instruction: message.instruction,
            prompts: message.prompts.map { Prompt(text: $0.prompt, echo: $0.echo) }
        )
    }
}

extension SSHMessage.UserAuthInfoRequestMessage {
    init(_ challenge: NIOSSHKeyboardInteractiveChallenge) {
        self.init(
            name: challenge.name,
            instruction: challenge.instruction,
            languageTag: "",
            prompts: challenge.prompts.map { Prompt(prompt: $0.text, echo: $0.echo) }
        )
    }
}
