import Foundation
import TogoKit

/// One helper connection per machine for requests the monitor does not make: agents,
/// folders, capture and upload. A connection is opened on first use and kept, because the
/// new session sheet asks for several folders in a row; it is dropped when it fails and
/// when the app goes to the background. A saved password or key signs in on its own; only a
/// request given a prompter can ask the user for anything else.
///
/// A network change drops every connection, since one made over the old network (a home LAN
/// address after walking out of Wi-Fi) is dead without knowing it; chat feeds run on these
/// connections, so they end too and reopen from their cursor over a new one. A call that times
/// out is followed by a ping, and a connection that does not answer it is dropped as well.
actor HelperChannels {
    private struct Channel {
        let connection: SSHConnection
        let client: HelperClient
    }

    private static let client = ClientInfo(name: "togo-ios", version: "0.1.0")
    private let env: SessionEnvironment
    private var channels: [UUID: Task<Channel, any Error>] = [:]
    private let path: any NetworkPathSource
    private var pathTask: Task<Void, Never>?

    init(env: SessionEnvironment, path: any NetworkPathSource = SystemPathSource()) {
        self.env = env
        self.path = path
    }

    /// Follows network changes from the first connection on.
    private func watchPath() {
        guard pathTask == nil else { return }
        let changes = path.changes
        pathTask = Task { [weak self] in
            for await _ in changes { await self?.dropAll() }
        }
    }

    deinit {
        pathTask?.cancel()
    }

    /// A connection that closed under a request is opened again once, since the host may
    /// have dropped an idle channel.
    func call<T: Sendable>(
        _ record: HostRecord, prompter: (any ConnectionPrompter)?, _ body: @Sendable (HelperClient) async throws -> T
    ) async throws -> T {
        let opened: Channel
        do {
            opened = try await channel(for: record, prompter: prompter)
        } catch HelperError.closed {
            await drop(record.id)
            opened = try await channel(for: record, prompter: prompter)
        }
        do {
            return try await body(opened.client)
        } catch HelperError.closed {
            await drop(record.id, ifStill: opened)
            return try await body(try await channel(for: record, prompter: prompter).client)
        } catch HelperError.timeout(let method) {
            // Slow, or dead without knowing it: one more ping tells which.
            if await HelperLiveness.isDead(ping: { try await opened.client.keepalive() }) {
                await drop(record.id, ifStill: opened)
            }
            throw HelperError.timeout(method: method)
        } catch {
            if error is SSHError { await drop(record.id, ifStill: opened) }
            throw error
        }
    }

    /// Drops the machine's channel only if it is still the one that failed, so a request that
    /// failed late cannot close a newer connection another request just opened.
    private func drop(_ id: UUID, ifStill failed: Channel) async {
        guard let task = channels[id], let current = try? await task.value, current.client === failed.client else { return }
        await drop(id)
    }

    /// The SSH connection behind the machine's helper channel, opened if there is none. Port
    /// previews open their connections over it, so they never sign in a second time. It never
    /// asks the user anything: the review call that came first already could.
    func connection(for record: HostRecord) async throws -> SSHConnection {
        try await channel(for: record, prompter: nil).connection
    }

    func drop(_ id: UUID) async {
        guard let task = channels.removeValue(forKey: id) else { return }
        if let channel = try? await task.value {
            await channel.client.close()
            await channel.connection.close()
        }
    }

    func dropAll() async {
        for id in Array(channels.keys) {
            await drop(id)
        }
    }

    private func channel(for record: HostRecord, prompter: (any ConnectionPrompter)?) async throws -> Channel {
        watchPath()
        if let task = channels[record.id] {
            do {
                return try await task.value
            } catch where prompter != nil {
                // Joined an open that could not ask; this request can, so it tries itself.
                if channels[record.id] == task { channels[record.id] = nil }
            }
        }
        var env = self.env
        if let prompter { env.prompter = prompter }
        let task = Task { try await Self.open(record, env: env) }
        channels[record.id] = task
        do {
            return try await task.value
        } catch {
            // A drop and a newer open may have replaced this entry while it was failing.
            if channels[record.id] == task { channels[record.id] = nil }
            throw error
        }
    }

    private static func open(_ record: HostRecord, env: SessionEnvironment) async throws -> Channel {
        let command = record.helperPath.map { shellQuote([$0, "serve", "--stdio"]) } ?? HelperCommand.serveStdio
        let connection = env.connection(for: record.profile)
        do {
            try await connection.connect()
            let client = HelperClient(channel: try await connection.openExec(command))
            do {
                _ = try await client.hello(client: Self.client)
            } catch {
                await client.close()
                throw error
            }
            return Channel(connection: connection, client: client)
        } catch {
            await connection.close()
            throw error
        }
    }
}
