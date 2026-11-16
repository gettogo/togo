import Foundation
import Synchronization
import Testing
import TogoKit
@testable import Togo

@MainActor struct HelperRequestExecutionTests {
    @Test func lostReplyNeverReplaysTheBody() async {
        let calls = Mutex(0)
        let drops = Mutex(0)
        do {
            _ = try await HelperRequestExecution.once({ () async throws -> Int in
                calls.withLock { $0 += 1 }
                throw HelperError.closed
            }, onClosed: { drops.withLock { $0 += 1 } })
            Issue.record("Expected uncertain delivery")
        } catch {
            #expect(error is HelperCallInterrupted)
            #expect(!CommandLineLogic.notSentNotice(error).hasPrefix("Not sent"))
            #expect(CommandLineLogic.notSentNotice(error).contains("may have completed"))
        }
        #expect(calls.withLock { $0 } == 1)
        #expect(drops.withLock { $0 } == 1)
    }

    @Test func ordinaryErrorsDoNotBecomeUncertainOrDropTheChannel() async {
        let drops = Mutex(0)
        await #expect(throws: HelperError.refused("busy")) {
            _ = try await HelperRequestExecution.once({ () async throws -> Int in
                throw HelperError.refused("busy")
            }, onClosed: { drops.withLock { $0 += 1 } })
        }
        #expect(drops.withLock { $0 } == 0)
    }
}
