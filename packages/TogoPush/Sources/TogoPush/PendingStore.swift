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

    public init(
        requestId: String, hostId: String, sessionId: String?, bib: String?, agent: String?, host: String?,
        title: String, subject: PushSubject?, options: [PushOption], receivedAt: Date, answered: Answered?
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
    }
}

public final class PendingStore: Sendable {
    private let blob: SharedBlob<PendingRequest>
    private let clock: @Sendable () -> Date
    private let onChange: (@Sendable () -> Void)?

    /// Stores requests in `directory`, normally `SharedGroup.storeDirectory(_:)`. Safe to
    /// open from the app and its extensions at once. `onChange` runs after every write, so
    /// the waiting widget can reload.
    public init(directory: URL, clock: @escaping @Sendable () -> Date, onChange: (@Sendable () -> Void)? = nil) {
        blob = SharedBlob(directory: directory, name: "pending")
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
    /// the store never saw is recorded so it too is answered once.
    public func claim(_ requestId: String, hostId: String) -> Bool {
        var claimed = false
        update { values in
            if let index = values.firstIndex(where: { $0.requestId == requestId }) {
                guard values[index].answered == nil else { return }
                values[index].answered = .sending
            } else {
                values.append(PendingRequest(
                    requestId: requestId, hostId: hostId, sessionId: nil, bib: nil, agent: nil, host: nil, title: "",
                    subject: nil, options: [], receivedAt: clock(), answered: .sending
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
        }
    }

    public func mark(_ requestId: String, _ answered: PendingRequest.Answered) {
        update { values in
            guard let index = values.firstIndex(where: { $0.requestId == requestId }) else { return }
            values[index].answered = answered
        }
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
