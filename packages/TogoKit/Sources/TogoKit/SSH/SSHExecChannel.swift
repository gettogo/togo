import Foundation
import Synchronization
import NIOCore
@preconcurrency import NIOSSH

enum SessionEvent: Sendable {
    case stdout(Data)
    case stderr(Data)
    case exit(Int32)
    /// The channel is gone; a preceding `exit` says whether the command finished.
    case closed
}

/// One SSH session channel opened with our own handler, because Citadel offers a PTY only
/// together with a login shell and hides exit status 0 from its streams.
///
/// Reads are paced by the consumer: autoRead is off and `requestMore()` asks NIO for the
/// next batch only after the previous one was accepted, so a full `ByteStream` stops
/// reading from the socket and SSH flow control throttles the sender.
final class SessionChannel: Sendable {
    let channel: any Channel
    let events: AsyncStream<SessionEvent>

    private init(channel: any Channel, events: AsyncStream<SessionEvent>) {
        self.channel = channel
        self.events = events
    }

    static func open(on root: any Channel, pty: SSHChannelRequestEvent.PseudoTerminalRequest?, command: String) async throws -> SessionChannel {
        let (events, continuation) = AsyncStream<SessionEvent>.makeStream()
        let channel = try await root.eventLoop.flatSubmit { () -> EventLoopFuture<any Channel> in
            let promise = root.eventLoop.makePromise(of: (any Channel).self)
            do {
                let ssh = try root.pipeline.syncOperations.handler(type: NIOSSHHandler.self)
                ssh.createChannel(promise, channelType: .session) { child, _ in
                    child.setOption(ChannelOptions.autoRead, value: false).flatMap {
                        child.setOption(ChannelOptions.allowRemoteHalfClosure, value: true)
                    }.flatMap {
                        child.pipeline.addHandler(SessionHandler(continuation: continuation))
                    }
                }
            } catch {
                promise.fail(error)
            }
            return promise.futureResult
        }.get()
        do {
            if let pty {
                try await channel.triggerUserOutboundEvent(pty)
            }
            try await channel.triggerUserOutboundEvent(SSHChannelRequestEvent.ExecRequest(command: command, wantReply: true))
        } catch {
            try? await channel.close()
            throw error
        }
        channel.read()
        return SessionChannel(channel: channel, events: events)
    }

    func requestMore() {
        channel.read()
    }

    func write(_ data: Data) async throws {
        try await channel.writeAndFlush(SSHChannelData(type: .channel, data: .byteBuffer(ByteBuffer(bytes: data))))
    }

    func windowChange(cols: Int, rows: Int) async throws {
        try await channel.triggerUserOutboundEvent(SSHChannelRequestEvent.WindowChangeRequest(
            terminalCharacterWidth: cols, terminalRowHeight: rows, terminalPixelWidth: 0, terminalPixelHeight: 0
        ))
    }

    func close() async {
        try? await channel.close()
    }
}

private final class SessionHandler: ChannelInboundHandler, Sendable {
    typealias InboundIn = SSHChannelData

    private let continuation: AsyncStream<SessionEvent>.Continuation

    init(continuation: AsyncStream<SessionEvent>.Continuation) {
        self.continuation = continuation
    }

    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        let message = unwrapInboundIn(data)
        guard case .byteBuffer(let buffer) = message.data else { return }
        let bytes = Data(buffer.readableBytesView)
        switch message.type {
        case .channel: continuation.yield(.stdout(bytes))
        case .stdErr: continuation.yield(.stderr(bytes))
        default: break
        }
    }

    func userInboundEventTriggered(context: ChannelHandlerContext, event: Any) {
        switch event {
        case let status as SSHChannelRequestEvent.ExitStatus:
            continuation.yield(.exit(Int32(truncatingIfNeeded: status.exitStatus)))
        case is ChannelFailureEvent:
            // The server refused the exec request; nothing will run on this channel.
            context.close(promise: nil)
        default:
            context.fireUserInboundEventTriggered(event)
        }
    }

    func channelInactive(context: ChannelHandlerContext) {
        continuation.yield(.closed)
        continuation.finish()
    }

    func errorCaught(context: ChannelHandlerContext, error: any Error) {
        context.close(promise: nil)
    }
}

/// Raw stdio of a command without a PTY, 8-bit clean: the helper's JSON-RPC pipe.
/// stderr is discarded, because frames travel only on stdout.
final class SSHExecChannel: ExecChannel {
    let incoming = ByteStream()
    private let session: SessionChannel
    private let status = ExitStatusBox()

    var exitStatus: Int32? { status.value.withLock { $0 } }

    init(session: SessionChannel) {
        self.session = session
        let incoming = self.incoming
        let status = self.status
        Task {
            for await event in session.events {
                // Recorded before `incoming` finishes, so a reader that sees the end can ask why.
                if case .exit(let code) = event { status.value.withLock { $0 = code } }
                if case .stdout(let data) = event, !(await incoming.write(data)) { break }
                session.requestMore()
            }
            incoming.finish()
            await session.close()
        }
    }

    func write(_ data: Data) async throws {
        do {
            try await session.write(data)
        } catch {
            throw SSHError.channelClosed(exitStatus: nil)
        }
    }

    func close() async {
        await session.close()
        incoming.finish()
    }
}

/// Shared between the channel and the task that reads its events.
private final class ExitStatusBox: Sendable {
    let value = Mutex<Int32?>(nil)
}
