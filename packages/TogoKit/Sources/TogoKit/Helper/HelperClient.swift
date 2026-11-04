import Foundation

/// A protocol v1 client for `togo serve --stdio` running behind an exec channel.
///
/// Calls are pipelined: each gets the next integer id and waits for the response
/// with that id, so the helper may answer in any order. The client is the single
/// consumer of `channel.incoming`; one reader task decodes frames and resumes the
/// waiting call. One writer task sends frames in id order, so `channel.write` is
/// never called concurrently.
public actor HelperClient {
    private let channel: any ExecChannel
    private let callTimeout: Duration
    private var nextID = 1
    private var pending: [Int: PendingCall] = [:]
    private var capabilities: Set<String>?
    private var isClosed = false
    private var reader: Task<Void, Never>?
    private var writer: Task<Void, Never>?
    private let outbox: AsyncStream<OutgoingFrame>
    private let outboxSink: AsyncStream<OutgoingFrame>.Continuation

    public init(channel: any ExecChannel, callTimeout: Duration = .seconds(10)) {
        self.channel = channel
        self.callTimeout = callTimeout
        (outbox, outboxSink) = AsyncStream.makeStream()
    }

    deinit {
        reader?.cancel()
        writer?.cancel()
        outboxSink.finish()
    }

    public func hello(client: ClientInfo) async throws -> HelloResult {
        let params = HelloParams(protocol: 1, client: client, capabilities: [])
        let result: HelloResult = try await call(.hello, params)
        guard result.protocol == 1 else {
            throw HelperError.rpc(RPCErrorObject(
                code: RPCErrorCode.unsupportedVersion, message: "unsupported_version",
                data: .object(["supported": .array([.number(1)]), "got": .number(Double(result.protocol))])
            ))
        }
        capabilities = Set(result.capabilities)
        return result
    }

    public func listSessions() async throws -> [Session] {
        let list: SessionList = try await readyCall(.sessionList, EmptyParams())
        return list.sessions
    }

    public func createSession(_ params: CreateSessionParams) async throws -> Session {
        try await readyCall(.sessionCreate, params)
    }

    public func killSession(id: String) async throws {
        let _: EmptyResult = try await readyCall(.sessionKill, SessionIDParams(id: id))
    }

    public func renameSession(id: String, name: String) async throws -> Session {
        try await readyCall(.sessionRename, RenameParams(id: id, name: name))
    }

    /// The argv to run on the host. Pass it through `shellQuote` before sending it
    /// as an exec command, because the host's login shell parses that string.
    public func attachCommand(id: String) async throws -> [String] {
        let result: AttachCommandResult = try await readyCall(.sessionAttachCommand, SessionIDParams(id: id))
        return result.argv
    }

    public func capture(id: String, lines: Int, join: Bool) async throws -> CaptureResult {
        try requireReady()
        guard capabilities?.contains("capture.v1") == true else {
            throw HelperError.unsupported(capability: "capture.v1")
        }
        return try await call(.sessionCapture, CaptureParams(id: id, lines: lines, join: join))
    }

    public func agents() async throws -> [AgentInfo] {
        let list: AgentList = try await readyCall(.agentsList, EmptyParams())
        return list.agents
    }

    public func recentDirs() async throws -> [String] {
        let result: RecentDirs = try await readyCall(.fsRecentDirs, EmptyParams())
        return result.dirs
    }

    public func listDir(path: String, showHidden: Bool) async throws -> ListDirResult {
        try await readyCall(.fsListDir, ListDirParams(path: path, showHidden: showHidden))
    }

    /// Fails pending calls with `.closed` and closes the channel. Later calls throw `.closed`.
    public func close() async {
        guard !isClosed else { return }
        shutDown()
        await channel.close()
    }

    // MARK: - Calls

    private func requireReady() throws {
        if isClosed { throw HelperError.closed }
        if capabilities == nil { throw HelperError.notReady }
    }

    private func readyCall<P: Encodable & Sendable, R: Decodable>(_ method: Method, _ params: P) async throws -> R {
        try requireReady()
        return try await call(method, params)
    }

    private func call<P: Encodable & Sendable, R: Decodable>(_ method: Method, _ params: P) async throws -> R {
        if isClosed { throw HelperError.closed }
        startTasks()
        let id = nextID
        nextID += 1
        let frame = try Frame.encode(JSONEncoder().encode(RPCRequest(id: id, method: method.rawValue, params: params)))
        let payload = try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Data, any Error>) in
                // Registering before the frame is queued means a response cannot arrive for
                // an id nobody is waiting on, however the actor interleaves the reader.
                pending[id] = PendingCall(
                    method: method.rawValue,
                    continuation: continuation,
                    timer: Task { [weak self, callTimeout] in
                        guard (try? await Task.sleep(for: callTimeout)) != nil else { return }
                        await self?.finish(id, with: HelperError.timeout(method: method.rawValue))
                    }
                )
                if Task.isCancelled {
                    finish(id, with: CancellationError())
                } else {
                    outboxSink.yield(OutgoingFrame(id: id, bytes: frame))
                }
            }
        } onCancel: {
            Task { await self.finish(id, with: CancellationError()) }
        }
        do {
            return try JSONDecoder().decode(ResultEnvelope<R>.self, from: payload).result
        } catch {
            throw HelperError.malformedResponse
        }
    }

    private func finish(_ id: Int, with error: any Error) {
        guard let call = pending.removeValue(forKey: id) else { return }
        call.timer.cancel()
        call.continuation.resume(throwing: error)
    }

    private func isPending(_ id: Int) -> Bool {
        pending[id] != nil
    }

    // MARK: - Channel tasks

    private func startTasks() {
        guard reader == nil else { return }
        let channel = channel
        let outbox = outbox
        // ExecChannel.write must not be called concurrently (see ExecChannel), so every
        // frame goes through this one task. A call that timed out or was cancelled before
        // its turn is skipped rather than sent for a reply nobody reads.
        writer = Task { [weak self] in
            for await frame in outbox {
                guard let self else { return }
                guard await self.isPending(frame.id) else { continue }
                do {
                    try await channel.write(frame.bytes)
                } catch {
                    await self.channelFailed()
                    return
                }
            }
        }
        let incoming = channel.incoming
        reader = Task { [weak self] in
            var decoder = FrameDecoder()
            for await chunk in incoming {
                let frames: [Data]
                do {
                    frames = try decoder.append(chunk)
                } catch {
                    await self?.channelFailed()
                    return
                }
                for frame in frames {
                    guard let self else { return }
                    await self.receive(frame)
                }
            }
            await self?.channelFailed()
        }
    }

    private func receive(_ frame: Data) async {
        guard let envelope = try? JSONDecoder().decode(Envelope.self, from: frame) else {
            // A frame that is not JSON-RPC means the stream is not protocol v1 (a shell
            // banner, a wrong binary), so nothing after it can be trusted either.
            await channelFailed()
            return
        }
        // Notifications have no id and the client does not act on any yet. A response to an
        // expired call finds no entry and is dropped.
        guard case .int(let id)? = envelope.id, let call = pending.removeValue(forKey: id) else { return }
        call.timer.cancel()
        if let error = envelope.error {
            call.continuation.resume(throwing: HelperError.rpc(error))
        } else {
            call.continuation.resume(returning: frame)
        }
    }

    private func channelFailed() async {
        guard !isClosed else { return }
        shutDown()
        await channel.close()
    }

    private func shutDown() {
        isClosed = true
        reader?.cancel()
        writer?.cancel()
        outboxSink.finish()
        let calls = pending.values
        pending.removeAll()
        for call in calls {
            call.timer.cancel()
            call.continuation.resume(throwing: HelperError.closed)
        }
    }
}

private struct OutgoingFrame: Sendable {
    let id: Int
    let bytes: Data
}

private struct PendingCall {
    let method: String
    let continuation: CheckedContinuation<Data, any Error>
    let timer: Task<Void, Never>
}

/// The routing fields of an incoming message. `result` is decoded later into the type
/// the waiting call expects, so a large capture is parsed once, not via `JSONValue`.
private struct Envelope: Decodable {
    var id: RPCID?
    var error: RPCErrorObject?
}

private struct ResultEnvelope<R: Decodable>: Decodable {
    var result: R
}
