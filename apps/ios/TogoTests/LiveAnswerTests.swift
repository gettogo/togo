import Foundation
import Synchronization
import Testing
import TogoPush
@testable import Togo

private let answerHost = "h_mmg42klgyqzwneiskrelxms3j7"
private let answerRequest = "r_aaaaaaaaaa"

private final class AnswerTrace: Sendable {
    struct State { var sends = 0; var feedback: [LiveAnswerFeedback] = [] }
    let state = Mutex(State())
}

private func answerStore() -> (PendingStore, URL) {
    let directory = FileManager.default.temporaryDirectory.appending(path: "live-answer-\(UUID())")
    let pending = PendingStore(directory: directory, clock: { Date() })
    pending.upsert(PendingRequest(
        requestId: answerRequest, hostId: answerHost, sessionId: nil, bib: "03", agent: "shell", host: nil,
        title: "Run this check?", subject: nil,
        options: [PushOption(n: 1, label: "Approve", role: "approve"), PushOption(n: 2, label: "Deny", role: "deny")],
        receivedAt: Date(), answered: nil
    ))
    return (pending, directory)
}

struct LiveAnswerTests {
    enum Failure: Error { case offline }

    @Test(arguments: ["approve", "deny"])
    func answerShowsDeliveryProgressBeforeAndAfterTheSend(_ decision: String) async throws {
        let (pending, directory) = answerStore()
        defer { try? FileManager.default.removeItem(at: directory) }
        let trace = AnswerTrace()
        try await LiveAnswer.perform(decision, hostId: answerHost, requestId: answerRequest, pending: pending) {
            #expect(pending.request(answerRequest)?.answered == .sending)
            #expect(trace.state.withLock { $0.feedback } == [.sending])
            trace.state.withLock { $0.sends += 1 }
        } refresh: { feedback in trace.state.withLock { $0.feedback.append(feedback) } }
        #expect(pending.request(answerRequest)?.answered == (decision == "deny" ? .denied : .approved))
        #expect(trace.state.withLock { $0.sends } == 1)
        #expect(trace.state.withLock { $0.feedback } == [.sending, .sent])
    }

    @Test func failedSendShowsFailureAndCanBeRetried() async throws {
        let (pending, directory) = answerStore()
        defer { try? FileManager.default.removeItem(at: directory) }
        let trace = AnswerTrace()
        do {
            try await LiveAnswer.perform("approve", hostId: answerHost, requestId: answerRequest, pending: pending) {
                trace.state.withLock { $0.sends += 1 }
                throw Failure.offline
            } refresh: { feedback in trace.state.withLock { $0.feedback.append(feedback) } }
            Issue.record("The failed send returned success")
        } catch Failure.offline {}
        #expect(pending.request(answerRequest)?.answered == nil)
        #expect(trace.state.withLock { $0.feedback } == [.sending, .failed])
        try await LiveAnswer.perform("approve", hostId: answerHost, requestId: answerRequest, pending: pending) {
            trace.state.withLock { $0.sends += 1 }
        } refresh: { feedback in trace.state.withLock { $0.feedback.append(feedback) } }
        #expect(trace.state.withLock { $0.sends } == 2)
        #expect(trace.state.withLock { $0.feedback } == [.sending, .failed, .sending, .sent])
    }

    @Test func duplicateTapRestoresFeedbackWithoutAnotherSend() async throws {
        let (pending, directory) = answerStore()
        defer { try? FileManager.default.removeItem(at: directory) }
        pending.mark(answerRequest, .approved)
        let trace = AnswerTrace()
        try await LiveAnswer.perform("deny", hostId: answerHost, requestId: answerRequest, pending: pending) {
            trace.state.withLock { $0.sends += 1 }
        } refresh: { feedback in trace.state.withLock { $0.feedback.append(feedback) } }
        #expect(trace.state.withLock { $0.sends } == 0)
        #expect(trace.state.withLock { $0.feedback } == [.sent])
        #expect(pending.request(answerRequest)?.answered == .approved)
    }

    @Test func mismatchedMachineCannotClaimTheRequest() async throws {
        let (pending, directory) = answerStore()
        defer { try? FileManager.default.removeItem(at: directory) }
        let trace = AnswerTrace()
        do {
            try await LiveAnswer.perform("approve", hostId: "h_other", requestId: answerRequest, pending: pending) {
                trace.state.withLock { $0.sends += 1 }
            } refresh: { feedback in trace.state.withLock { $0.feedback.append(feedback) } }
            Issue.record("Another machine claimed the request")
        } catch LiveAnswer.Failure.unavailable {}
        #expect(pending.request(answerRequest)?.answered == nil)
        #expect(trace.state.withLock { $0.sends } == 0)
    }

    @Test func deliveryWordsDoNotReplaceNewHelperStatus() {
        #expect(LiveStatus("needs_you", feedback: .sent) == .sent)
        #expect(!LiveStatus("needs_you", feedback: .sent).showsElapsed)
        #expect(LiveStatus("needs_you", feedback: .failed) == .sendFailed)
        #expect(LiveStatus("working", feedback: .sent) == .working)
        #expect(LiveStatus("needs_you", stale: true, feedback: .sending) == .expired)
    }

    @Test func localFeedbackRoundTripsAndOldPushClearsIt() throws {
        typealias State = TogoActivityAttributes.ContentState
        let state = State(
            bib: "03", agent: "shell", status: "needs_you", waiting: 1, since: 10, requestId: answerRequest,
            command: nil, answerFeedback: .sent
        )
        #expect(try JSONDecoder().decode(State.self, from: JSONEncoder().encode(state)).answerFeedback == .sent)
        let old = Data(#"{"status":"needs_you","requestId":"r_aaaaaaaaaa"}"#.utf8)
        #expect(try JSONDecoder().decode(State.self, from: old).answerFeedback == nil)
    }
}
