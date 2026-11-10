import Foundation
import TogoKit

/// A mosh session as a byte transport.
///
/// mosh exposes no datagram events: its only output is a stream of screen diffs. It
/// paints a blank startup frame and its own "mosh: ..." banners without any server, so
/// the first other paint after the startup frame stands for the first server datagram,
/// and the client's own "Last contact" notification bar marks a silent network.
public actor MoshTransport: ByteTransport {
    public nonisolated let incoming = ByteStream()
    public nonisolated let state: AsyncStream<TransportState>
    private nonisolated let stateSink: AsyncStream<TransportState>.Continuation

    private let endpoint: MoshEndpoint
    private let predict: String
    private let firstReplyTimeout: Duration
    private let stopServer: (@Sendable () async -> Void)?
    private var size: (cols: Int, rows: Int)
    private var client: MoshClient?
    private var firstReply: CheckedContinuation<FirstReply, Never>?
    private var phase = Phase.idle
    private var roaming = false
    private var lastOutput = ContinuousClock.now
    private var startupOutput = Data()
    private var sawStartupFrame = false
    private var roamingWatch: Task<Void, Never>?

    private enum Phase { case idle, connecting, live, closing, closed }
    private enum FirstReply { case output, timedOut, exited }

    /// mosh draws this bar itself, white on blue across the top row, once the server has
    /// been silent for several seconds.
    private static let silenceText = Data("mosh: Last contact ".utf8)
    private static let barBackground = Data(";44m".utf8)
    private static let bannerPrefix = Data("mosh: ".utf8)
    /// mosh's notification bar is bold white on blue; every repaint of it, even a diff of a
    /// few characters, sets this rendition first.
    private static let barRendition = Data("37;44m".utf8)
    /// The startup frame ends by switching off every mouse mode, the last being UTF-8 mouse.
    private static let startupFrameEnd = Data("\u{1B}[?1005l".utf8)
    /// While roaming, mosh repaints the banner's seconds counter every second; a pause
    /// longer than this, or a full repaint, means the server is back.
    private static let bannerTick = Duration.milliseconds(2500)
    private static let bannerTickBytes = 32

    public init(
        endpoint: MoshEndpoint, cols: Int, rows: Int, predict: String = "adaptive", firstReplyTimeout: Duration = .seconds(5),
        stopServer: (@Sendable () async -> Void)? = nil
    ) {
        self.endpoint = endpoint
        self.predict = predict
        self.firstReplyTimeout = firstReplyTimeout
        self.stopServer = stopServer
        size = (cols, rows)
        (state, stateSink) = AsyncStream.makeStream(bufferingPolicy: .unbounded)
    }

    public func connect() async throws {
        guard phase == .idle else { return }
        phase = .connecting
        stateSink.yield(.connecting)
        let client: MoshClient
        do {
            client = try MoshClient(cols: size.cols, rows: size.rows)
        } catch {
            finish(with: .failed("mosh pipes: \(error)"))
            throw error
        }
        self.client = client
        let stream = incoming
        client.start(
            host: endpoint.host, port: endpoint.port, key: endpoint.key, predict: predict,
            deliver: { [weak self] chunk in
                await self?.noteOutput(chunk)
                _ = await stream.write(chunk)
            },
            exited: { [weak self] status in
                Task { await self?.clientExited(status: status) }
            }
        )
        let timeout = firstReplyTimeout
        let timer = Task { [weak self] in
            try? await Task.sleep(for: timeout)
            await self?.resolveFirstReply(.timedOut)
        }
        let reply = await withCheckedContinuation { firstReply = $0 }
        timer.cancel()
        guard reply == .output, phase == .connecting else {
            await shutDown(reason: .failed("no reply from mosh-server"))
            // The server this start launched never heard from the client; stop it now
            // instead of leaving it to wait out its network timeout.
            await stopServer?()
            throw MoshConnectError.noUDPReply(address: endpoint.host)
        }
        phase = .live
        startupOutput = Data()
        stateSink.yield(.connected)
    }

    public func send(_ data: Data) async throws {
        guard phase == .live, let client else { throw POSIXError(.ENOTCONN) }
        try await client.send(data)
    }

    public func resize(cols: Int, rows: Int) async throws {
        size = (cols, rows)
        client?.resize(cols: cols, rows: rows)
    }

    public func close() async {
        await shutDown(reason: .requested)
    }

    // MARK: Events from the client threads

    private func noteOutput(_ chunk: Data) {
        let now = ContinuousClock.now
        defer { lastOutput = now }
        if phase == .connecting {
            if isFirstServerPaint(chunk) { resolveFirstReply(.output) }
            return
        }
        guard phase == .live else { return }
        if Self.isSilenceStatusLine(chunk) {
            setRoaming(true)
        } else if roaming, chunk.count > Self.bannerTickBytes {
            setRoaming(false)
        }
    }

    /// True only for mosh's notification bar: the text must follow the SGR that sets the
    /// bar's blue background, so program output quoting the words does not stop the link.
    static func isSilenceStatusLine(_ chunk: Data) -> Bool {
        var searchStart = chunk.startIndex
        while let match = chunk.range(of: silenceText, in: searchStart..<chunk.endIndex) {
            if endsWithBarRendition(chunk[chunk.startIndex..<match.lowerBound]) { return true }
            searchStart = match.upperBound
        }
        return false
    }

    /// True for a chunk that repaints mosh's notification bar, such as its "Nothing received
    /// from server" text changing to a send error or its seconds counter ticking. Such a
    /// diff can leave out the "mosh: " prefix, so it must not pass for the server's output.
    static func paintsNotificationBar(_ chunk: Data) -> Bool {
        var searchStart = chunk.startIndex
        while let match = chunk.range(of: barRendition, in: searchStart..<chunk.endIndex) {
            if endsWithBarRendition(chunk[chunk.startIndex..<match.upperBound]) { return true }
            searchStart = match.upperBound
        }
        return false
    }

    /// Whether `prefix` ends with `ESC [ <digits and ;> 44 m`.
    private static func endsWithBarRendition(_ prefix: Data) -> Bool {
        guard prefix.count >= 4, prefix.suffix(barBackground.count).elementsEqual(barBackground) else { return false }
        var index = prefix.index(prefix.endIndex, offsetBy: -barBackground.count)
        while index > prefix.startIndex {
            index = prefix.index(before: index)
            let byte = prefix[index]
            if byte == UInt8(ascii: "[") {
                return index > prefix.startIndex && prefix[prefix.index(before: index)] == 0x1B
            }
            guard byte == UInt8(ascii: ";") || (UInt8(ascii: "0")...UInt8(ascii: "9")).contains(byte) else { return false }
        }
        return false
    }

    private func isFirstServerPaint(_ chunk: Data) -> Bool {
        startupOutput += chunk
        guard let end = startupOutput.range(of: Self.startupFrameEnd) else { return false }
        sawStartupFrame = true
        return end.upperBound < startupOutput.endIndex && Self.isServerPaint(chunk)
    }

    /// Output after the startup frame that is neither a banner nor a repaint of the bar.
    static func isServerPaint(_ chunk: Data) -> Bool {
        chunk.range(of: bannerPrefix) == nil && !paintsNotificationBar(chunk)
    }

    private func setRoaming(_ value: Bool) {
        guard roaming != value else { return }
        roaming = value
        stateSink.yield(value ? .roaming : .connected)
        roamingWatch?.cancel()
        roamingWatch = value ? Task { [weak self] in await self?.watchBannerTicks() } : nil
    }

    private func watchBannerTicks() async {
        while !Task.isCancelled, roaming, phase == .live {
            try? await Task.sleep(for: Self.bannerTick / 2)
            if ContinuousClock.now - lastOutput > Self.bannerTick { setRoaming(false) }
        }
    }

    private func resolveFirstReply(_ reply: FirstReply) {
        firstReply?.resume(returning: reply)
        firstReply = nil
    }

    private func clientExited(status: Int32) {
        client = nil
        resolveFirstReply(.exited)
        guard phase == .live else { return }
        finish(with: status == 0 ? .remoteExited(status: nil) : .failed("mosh exited with status \(status)"))
    }

    // MARK: Teardown

    /// Finishing `incoming` first lets the reader discard what is left, so mosh cannot
    /// stay blocked on a full screen pipe while it is asked to quit. mosh normally
    /// returns within a tenth of a second of the input closing; SIGTERM covers a
    /// session still waiting on a server that never answered.
    private func shutDown(reason: TransportCloseReason) async {
        guard phase != .closing, phase != .closed else { return }
        phase = .closing
        resolveFirstReply(.exited)
        incoming.finish()
        if let client {
            client.closeInput()
            if await !waitForExit(within: .seconds(1)), sawStartupFrame {
                client.terminate()
                _ = await waitForExit(within: .milliseconds(800))
            }
        }
        finish(with: reason)
    }

    private func waitForExit(within limit: Duration) async -> Bool {
        let deadline = ContinuousClock.now + limit
        while client != nil, ContinuousClock.now < deadline {
            try? await Task.sleep(for: .milliseconds(10))
        }
        return client == nil
    }

    private func finish(with reason: TransportCloseReason) {
        phase = .closed
        roamingWatch?.cancel()
        roamingWatch = nil
        incoming.finish()
        stateSink.yield(.closed(reason))
        stateSink.finish()
    }
}
