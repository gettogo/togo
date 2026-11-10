import Foundation

/// A request that needs the user, kept so the app, the Watch and the notification actions
/// can show and answer it. Only structured fields; never screen text.
public struct PendingRequest: Codable, Sendable, Equatable, Identifiable {
    public enum Answered: String, Codable, Sendable {
        case sending, approved, denied, elsewhere
    }

    public var id: String { requestId }
    public var requestId: String
    public var hostId: String
    public var sessionId: String?
    public var bib: String?
    public var agent: String?
    public var host: String?
    public var title: String
    public var subject: PushSubject?
    public var options: [PushOption]
    public var receivedAt: Date
    public var answered: Answered?
    /// The request takes a typed answer, as its push said. Absent in older records.
    public var allowText: Bool?
    /// When this device took the request's one answer (`PendingStore.claim`); set only while
    /// `answered` is `sending`. Absent in older records.
    public var claimedAt: Date?

    /// How long a claim holds. A send is over well before this (the relay post and the live
    /// channel give up sooner), so a claim still sending after it was left by a process that
    /// was stopped mid-send: the app, a notification action or a Live Activity button.
    public static let claimTimeout: TimeInterval = 120

    public init(
        requestId: String, hostId: String, sessionId: String?, bib: String?, agent: String?, host: String?,
        title: String, subject: PushSubject?, options: [PushOption], receivedAt: Date, answered: Answered?,
        allowText: Bool? = nil, claimedAt: Date? = nil
    ) {
        self.requestId = requestId
        self.hostId = hostId
        self.sessionId = sessionId
        self.bib = bib
        self.agent = agent
        self.host = host
        self.title = title
        self.subject = subject
        self.options = options
        self.receivedAt = receivedAt
        self.answered = answered
        self.allowText = allowText
        self.claimedAt = claimedAt
    }

    /// A claim nobody is finishing: still `sending` past `claimTimeout`, or a sending record
    /// from before claims had a time. Left as it is, the request would read "Sending" and
    /// refuse every later answer until it aged out.
    public func claimAbandoned(at now: Date) -> Bool {
        guard answered == .sending else { return false }
        guard let claimedAt else { return true }
        return abs(now.timeIntervalSince(claimedAt)) >= Self.claimTimeout
    }

    /// Nothing has answered it and no answer is on its way: it still waits on the user.
    public func isOpen(at now: Date) -> Bool {
        answered == nil || claimAbandoned(at: now)
    }

    /// A record `claim` made for an id this device never got a push for: nothing to show.
    public var isPlaceholder: Bool {
        title.isEmpty && options.isEmpty && sessionId == nil && bib == nil && agent == nil && host == nil
    }
}

/// The newest "stopped answering" push shown for a machine, so a replay of it is refused.
struct OfflineMark: Codable, Sendable, Equatable {
    var hostId: String
    var ts: Int64
}

public final class PendingStore: Sendable {
    private let blob: SharedBlob<PendingRequest>
    private let offlineMarks: SharedBlob<OfflineMark>
    private let clock: @Sendable () -> Date
    private let onChange: (@Sendable () -> Void)?

    /// Stores requests in `directory`, normally `SharedGroup.storeDirectory(_:)`. Safe to
    /// open from the app and its extensions at once. `onChange` runs after every write, so
    /// the waiting widget can reload.
    public init(directory: URL, clock: @escaping @Sendable () -> Date, onChange: (@Sendable () -> Void)? = nil) {
        blob = SharedBlob(directory: directory, name: "pending")
        offlineMarks = SharedBlob(directory: directory, name: "offline-seen")
        self.clock = clock
        self.onChange = onChange
    }

    private func update(_ change: (inout [PendingRequest]) -> Void) {
        blob.update(change)
        onChange?()
    }

    public func all() -> [PendingRequest] { blob.read() }

    public func request(_ requestId: String) -> PendingRequest? { blob.read().first { $0.requestId == requestId } }

    /// Adds a request the store has not seen. A request it already holds keeps its state and
    /// receipt time, so a replayed push can neither reopen an answered request nor make an
    /// old one look new.
    public func upsert(_ request: PendingRequest) {
        update { values in
            guard !values.contains(where: { $0.requestId == request.requestId }) else { return }
            values.append(request)
        }
    }

    /// Takes the one answer a request gets: marks it `sending` under the file lock and returns
    /// true, or returns false when it is already sending or answered, from any process. An id
    /// the store never saw is recorded so it too is answered once. A claim abandoned by a
    /// process stopped mid-send (`claimAbandoned`) is taken over.
    public func claim(_ requestId: String, hostId: String) -> Bool {
        var claimed = false
        let now = clock()
        update { values in
            if let index = values.firstIndex(where: { $0.requestId == requestId }) {
                guard values[index].isOpen(at: now) else { return }
                values[index].answered = .sending
                values[index].claimedAt = now
            } else {
                values.append(PendingRequest(
                    requestId: requestId, hostId: hostId, sessionId: nil, bib: nil, agent: nil, host: nil, title: "",
                    subject: nil, options: [], receivedAt: now, answered: .sending, claimedAt: now
                ))
            }
            claimed = true
        }
        return claimed
    }

    /// Undoes `claim` after a send that failed, so the user can answer again.
    public func release(_ requestId: String) {
        update { values in
            guard let index = values.firstIndex(where: { $0.requestId == requestId }),
                  values[index].answered == .sending
            else { return }
            values[index].answered = nil
            values[index].claimedAt = nil
        }
    }

    public func mark(_ requestId: String, _ answered: PendingRequest.Answered) {
        update { values in
            guard let index = values.firstIndex(where: { $0.requestId == requestId }) else { return }
            values[index].answered = answered
            values[index].claimedAt = answered == .sending ? clock() : nil
        }
    }

    /// Gives back every abandoned claim (`claimAbandoned`), as the Watch does at launch: the
    /// request opens again, and a placeholder `claim` made for an id never pushed here goes
    /// rather than open as a blank request. Claims still inside their time are left alone,
    /// since another process may be sending.
    public func releaseAbandonedClaims() {
        let now = clock()
        guard blob.read().contains(where: { $0.claimAbandoned(at: now) }) else { return }
        update { values in
            values.removeAll { $0.claimAbandoned(at: now) && $0.isPlaceholder }
            for index in values.indices where values[index].claimAbandoned(at: now) {
                values[index].answered = nil
                values[index].claimedAt = nil
            }
        }
    }

    /// Records an offline push (sealed at `ts`) as shown for `hostId`. False when this one or a
    /// newer one was already shown: an offline push has no id, so its seal time is its identity,
    /// and every refresh of the will carries a later one.
    public func claimOffline(hostId: String, ts: Int64) -> Bool {
        var fresh = false
        offlineMarks.update { marks in
            if let index = marks.firstIndex(where: { $0.hostId == hostId }) {
                guard marks[index].ts < ts else { return }
                marks[index].ts = ts
            } else {
                marks.append(OfflineMark(hostId: hostId, ts: ts))
            }
            fresh = true
        }
        return fresh
    }

    /// Forgets the offline push shown for `hostId`: the machine has been heard from since, so
    /// the next time it goes quiet is a new outage even when the helper reuses the same seal.
    public func clearOfflineMark(hostId: String) {
        offlineMarks.update { $0.removeAll { $0.hostId == hostId } }
    }

    public func remove(_ requestId: String) {
        update { $0.removeAll { $0.requestId == requestId } }
    }

    /// Drops requests received more than `age` seconds ago. The helper refuses answers to them
    /// by then anyway.
    public func prune(olderThan age: TimeInterval = 600) {
        let cutoff = clock().addingTimeInterval(-age)
        update { $0.removeAll { $0.receivedAt < cutoff } }
    }
}
