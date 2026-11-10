import Foundation

/// A protocol v1 client for `togo serve --stdio` running behind an exec channel.
///
/// Calls are pipelined: each gets the next integer id and waits for the response
/// with that id, so the helper may answer in any order. The client is the single
/// consumer of `channel.incoming`; one reader task decodes frames and resumes the
/// waiting call. One writer task sends frames in id order, so `channel.write` is
/// never called concurrently.
///
/// `event` notifications, sent once `subscribe` was called, go to `notifications`.
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
    private let eventSink: AsyncStream<EventGate.Tagged>.Continuation
    private let eventGate = EventGate()
    private let harnessSink: AsyncStream<HarnessNotification>.Continuation

    /// Events after the latest `subscribe` snapshot, in seq order. Single consumer.
    /// An event is checked against the newest snapshot when it is read, so after a
    /// resubscribe nothing the new snapshot already contains comes out.
    /// Holds the newest 256 when the consumer falls behind, so a seq gap means
    /// events were lost and the consumer should subscribe again. Finishes when the
    /// client closes.
    public nonisolated let notifications: AsyncStream<EventEnvelope>

    /// `harness` notifications for opened sessions; see `HelperClient+Harness.swift`.
    public nonisolated let harnessNotifications: AsyncStream<HarnessNotification>

    public init(channel: any ExecChannel, callTimeout: Duration = .seconds(10)) {
        self.channel = channel
        self.callTimeout = callTimeout
        (outbox, outboxSink) = AsyncStream.makeStream()
        let (events, eventSink) = AsyncStream<EventGate.Tagged>.makeStream(bufferingPolicy: .bufferingNewest(256))
        self.eventSink = eventSink
        notifications = eventGate.filtered(events)
        (harnessNotifications, harnessSink) = Self.makeHarnessStream()
    }

    deinit {
        reader?.cancel()
        writer?.cancel()
        outboxSink.finish()
        eventSink.finish()
        harnessSink.finish()
    }

    /// The protocol this app speaks.
    public static let protocolVersion = 1

    public func hello(client: ClientInfo) async throws -> HelloResult {
        let params = HelloParams(protocol: Self.protocolVersion, client: client, capabilities: [])
        let result: HelloResult = try await call(.hello, params)
        guard result.protocol == Self.protocolVersion else {
            // Shaped like the helper's own refusal: `supported` names what the other side speaks.
            throw HelperError.rpc(RPCErrorObject(
                code: RPCErrorCode.unsupportedVersion, message: "unsupported_version",
                data: .object(["supported": .array([.number(Double(result.protocol))])])
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
        try await readyCall(.sessionCapture, CaptureParams(id: id, lines: lines, join: join), needs: Capability.capture)
    }

    public func setMute(id: String, muted: Bool) async throws -> Session {
        try await readyCall(.sessionSetMute, SetMuteParams(id: id, muted: muted), needs: Capability.mute)
    }

    public func agents() async throws -> [AgentInfo] {
        let list: AgentList = try await readyCall(.agentsList, EmptyParams())
        return list.agents
    }

    public func recentDirs() async throws -> [String] {
        let result: RecentDirs = try await readyCall(.fsRecentDirs, EmptyParams())
        return result.dirs
    }

    /// Folders only, unless `includeFiles`; an older helper ignores it and lists folders only.
    public func listDir(path: String, showHidden: Bool, includeFiles: Bool = false) async throws -> ListDirResult {
        try await readyCall(.fsListDir, ListDirParams(path: path, showHidden: showHidden, includeFiles: includeFiles ? true : nil))
    }

    /// Lists the tree at `ref`; `path` is relative to the session cwd.
    public func listDir(
        path: String, showHidden: Bool, includeFiles: Bool = false, sessionID: String, ref: String
    ) async throws -> ListDirResult {
        try await readyCall(
            .fsListDir,
            ListDirParams(path: path, showHidden: showHidden, includeFiles: includeFiles ? true : nil, id: sessionID, ref: ref),
            needs: Capability.git
        )
    }

    /// Lists a folder of the repository holding the session cwd, `path` relative to its root as
    /// in `GitStatus`: on disk, or the tree at `ref`.
    public func listRepoDir(
        path: String, showHidden: Bool, includeFiles: Bool = false, sessionID: String, ref: String?
    ) async throws -> ListDirResult {
        try await readyCall(
            .fsListDir,
            ListDirParams(
                path: path, showHidden: showHidden, includeFiles: includeFiles ? true : nil, id: sessionID, ref: ref, root: true
            ),
            needs: Capability.gitWrite
        )
    }

    /// Writes a file for the session, at most `UploadParams.maxBytes`. A larger
    /// payload throws `.tooLarge` without sending anything.
    public func upload(_ params: UploadParams) async throws -> UploadResult {
        guard params.data.count <= UploadParams.maxBytes else { throw HelperError.tooLarge(params.data.count) }
        return try await readyCall(.fsUpload, params, needs: Capability.upload)
    }

    // MARK: - Events and approvals

    /// Starts or refreshes event delivery on this connection and returns the state the
    /// events continue from. Calling it again does not start a second stream.
    /// `foreground` suppresses pushes for sessions the app shows; the helper treats it
    /// as a lease that lapses 45 s after the last request, so an idle app on screen
    /// calls `keepalive()` about every 20 s.
    public func subscribe(foreground: Bool) async throws -> EventSnapshot {
        try await readyCall(.eventsSubscribe, EventsSubscribeParams(foreground: foreground), needs: Capability.events)
    }

    /// Renews the foreground lease; any other request renews it too.
    public func keepalive() async throws {
        let _: EmptyResult = try await readyCall(.eventsKeepalive, EmptyParams(), needs: Capability.events)
    }

    /// Pending approvals, oldest first, for one session or all of them.
    public func approvals(sessionId: String?) async throws -> [Approval] {
        let list: ApprovalList = try await readyCall(
            .approvalList, ApprovalListParams(sessionId: sessionId), needs: Capability.approvals
        )
        return list.approvals
    }

    /// Throws `.conflict` when the approval was answered elsewhere, its prompt is gone
    /// or it expired; the helper then sent no keys.
    public func answer(requestId: String, decision: Decision) async throws {
        do {
            let _: ApprovalAnswerResult = try await readyCall(
                .approvalAnswer, ApprovalAnswerParams(requestId: requestId, decision: decision),
                needs: Capability.approvals
            )
        } catch HelperError.rpc(let error) where error.code == RPCErrorCode.conflict {
            throw Self.conflict(error)
        }
    }

    static func conflict(_ error: RPCErrorObject) -> HelperError {
        if let reason = error.reason, case .object(let data)? = error.data {
            switch GitConflict(rawValue: reason) {
            case .partial?:
                guard case .array(let list)? = data["changed"] else { return .partialWrite(changed: []) }
                return .partialWrite(changed: list.compactMap { if case .string(let path) = $0 { path } else { nil } })
            case .raced?:
                guard case .string(let hash)? = data["hash"] else { return .racedCommit(hash: "") }
                return .racedCommit(hash: hash)
            default:
                return .conflict(reason)
            }
        }
        return .conflict(error.message)
    }

    // MARK: - Git, files and ports

    public func gitStatus(id: String) async throws -> GitStatus {
        try await readyCall(.gitStatus, SessionIDParams(id: id), needs: Capability.git)
    }

    /// A diff of one commit needs `gitwrite.v1`: an older helper would ignore `commit`
    /// and answer with the working tree instead.
    public func gitDiff(_ params: GitDiffParams) async throws -> GitDiff {
        try await readyCall(.gitDiff, params, needs: params.commit == nil ? Capability.git : Capability.gitWrite)
    }

    /// A graph log needs `gitfiles.v1`; asked of an older helper the flag is dropped, so the
    /// log still comes, in date order and without parents.
    public func gitLog(_ params: GitLogParams) async throws -> [GitCommit] {
        var params = params
        if params.graph != nil, !(capabilities?.contains(Capability.gitFiles) ?? false) { params.graph = nil }
        let log: GitLog = try await readyCall(.gitLog, params, needs: Capability.git)
        return log.commits
    }

    public func gitBranches(id: String) async throws -> GitBranches {
        try await readyCall(.gitBranches, SessionIDParams(id: id), needs: Capability.git)
    }

    public func gitFiles(_ params: GitFilesParams) async throws -> GitFiles {
        try await readyCall(.gitFiles, params, needs: Capability.gitFiles)
    }

    // Writes throw `.conflict` with the helper's reason (see `HelperError.gitConflict`)
    // when they were refused and nothing changed. Each returns the status after the write.

    // A path that is empty, longer than `GitPathsParams.maxPathBytes`, holds a NUL or ends
    // in `/`, and a commit that amends without a full `head` hash, throw the invalid
    // params error the helper would send, without sending anything.

    public func gitStage(id: String, paths: [String], seen: [String: GitSeen]? = nil) async throws -> GitStatus {
        try await gitPaths(.gitStage, GitPathsParams(id: id, paths: paths, seen: seen))
    }

    public func gitUnstage(id: String, paths: [String], seen: [String: GitSeen]? = nil) async throws -> GitStatus {
        try await gitPaths(.gitUnstage, GitPathsParams(id: id, paths: paths, seen: seen))
    }

    public func gitDiscard(id: String, paths: [String], seen: [String: GitSeen]? = nil) async throws -> GitStatus {
        try await gitPaths(.gitDiscard, GitPathsParams(id: id, paths: paths, seen: seen))
    }

    /// Stages these status entries, refused as stale if any changed state since they were read.
    public func gitStage(id: String, files: [GitStatus.File]) async throws -> GitStatus {
        try await gitPaths(.gitStage, GitPathsParams(id: id, files: files))
    }

    public func gitUnstage(id: String, files: [GitStatus.File]) async throws -> GitStatus {
        try await gitPaths(.gitUnstage, GitPathsParams(id: id, files: files))
    }

    public func gitDiscard(id: String, files: [GitStatus.File]) async throws -> GitStatus {
        try await gitPaths(.gitDiscard, GitPathsParams(id: id, files: files))
    }

    public func gitCommit(_ params: GitCommitParams) async throws -> GitCommitResult {
        guard params.isValid else { throw Self.invalidParams("amend needs the full hash of the commit it replaces") }
        return try await gitWrite(.gitCommit, params)
    }

    private func gitPaths(_ method: Method, _ params: GitPathsParams) async throws -> GitStatus {
        let keys = params.seen.map { Array($0.keys) } ?? []
        guard !params.paths.isEmpty, (params.paths + keys).allSatisfy(GitPathsParams.isValidPath) else {
            throw Self.invalidParams("each path names one file of at most 4096 bytes with no NUL")
        }
        return try await gitWrite(method, params)
    }

    static func invalidParams(_ reason: String) -> HelperError {
        .rpc(RPCErrorObject(
            code: RPCErrorCode.invalidParams, message: "invalid params", data: .object(["reason": .string(reason)])
        ))
    }

    private func gitWrite<P: Encodable & Sendable, R: Decodable>(_ method: Method, _ params: P) async throws -> R {
        do {
            return try await readyCall(method, params, needs: Capability.gitWrite)
        } catch HelperError.rpc(let error) where error.code == RPCErrorCode.conflict {
            throw Self.conflict(error)
        }
    }

    /// The root form needs `gitwrite.v1`: an older helper would read the path from the session cwd.
    public func readFile(_ params: FsReadParams) async throws -> FsReadResult {
        try await readyCall(.fsRead, params, needs: params.root == true ? Capability.gitWrite : Capability.git)
    }

    public func ports(id: String) async throws -> [ListeningPort] {
        let list: PortList = try await readyCall(.portsList, SessionIDParams(id: id), needs: Capability.ports)
        return list.ports
    }

    // MARK: - Push pairing

    public func pair(_ params: PushPairParams) async throws -> PushPairResult {
        try await readyCall(.pushPair, params, needs: Capability.push)
    }

    public func unpair(deviceId: String) async throws {
        let _: EmptyResult = try await readyCall(.pushUnpair, PushUnpairParams(deviceId: deviceId), needs: Capability.push)
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

    func readyCall<P: Encodable & Sendable, R: Decodable>(
        _ method: Method, _ params: P, needs capability: String? = nil
    ) async throws -> R {
        try requireReady()
        if let capability, capabilities?.contains(capability) != true {
            throw HelperError.unsupported(capability: capability)
        }
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
                // The call's own timeout starts once its frame is written (`sent`); until then
                // a longer one covers the wait behind other frames, such as a large upload.
                pending[id] = PendingCall(
                    method: method.rawValue,
                    continuation: continuation,
                    timer: timer(for: id, method: method.rawValue, after: callTimeout * Self.queuedTimeoutFactor)
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

    /// How much longer than the call timeout a frame may wait to be written.
    static let queuedTimeoutFactor = 3

    private func timer(for id: Int, method: String, after duration: Duration) -> Task<Void, Never> {
        Task { [weak self] in
            guard (try? await Task.sleep(for: duration)) != nil else { return }
            await self?.finish(id, with: HelperError.timeout(method: method))
        }
    }

    /// The frame is on the wire: the call timeout counts from now, not from when it was queued.
    private func sent(_ id: Int) {
        guard let call = pending[id] else { return }
        call.timer.cancel()
        pending[id]?.timer = timer(for: id, method: call.method, after: callTimeout)
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
                await self.sent(frame.id)
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
        if envelope.id == nil {
            if envelope.method == "event" { deliverEvent(frame) }
            if envelope.method == "harness", let note = Self.harnessNotification(frame) { harnessSink.yield(note) }
            return
        }
        // A response to an expired call finds no entry and is dropped.
        guard case .int(let id)? = envelope.id, let call = pending.removeValue(forKey: id) else { return }
        call.timer.cancel()
        if let error = envelope.error {
            call.continuation.resume(throwing: HelperError.rpc(error))
            return
        }
        // The helper writes the snapshot before any event it does not contain. The seq
        // is taken here, before the next frame is read, because the subscribing caller
        // resumes later and the events behind the snapshot may already be routed by then.
        if call.method == Method.eventsSubscribe.rawValue,
           let snapshot = try? JSONDecoder().decode(ResultEnvelope<SnapshotSeq>.self, from: frame) {
            eventGate.startGeneration(snapshotSeq: snapshot.result.seq)
        }
        call.continuation.resume(returning: frame)
    }

    /// An event that does not decode is dropped; the seq gap it leaves tells the
    /// consumer to subscribe again.
    private func deliverEvent(_ frame: Data) {
        guard let notification = try? JSONDecoder().decode(EventNotification.self, from: frame),
              let tagged = eventGate.tag(notification.params)
        else { return }
        eventSink.yield(tagged)
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
        eventSink.finish()
        harnessSink.finish()
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
    var timer: Task<Void, Never>
}

/// The routing fields of an incoming message. `result` is decoded later into the type
/// the waiting call expects, so a large capture is parsed once, not via `JSONValue`.
private struct Envelope: Decodable {
    var id: RPCID?
    var method: String?
    var error: RPCErrorObject?
}

private struct EventNotification: Decodable {
    var params: EventEnvelope
}

private struct SnapshotSeq: Decodable {
    var seq: Int
}

private struct ResultEnvelope<R: Decodable>: Decodable {
    var result: R
}
