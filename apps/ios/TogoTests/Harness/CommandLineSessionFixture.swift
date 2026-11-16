import Foundation
import TogoKit
@testable import Togo

extension CommandLineModelTests {
    /// Stands in for the session: records every call and answers from the samples.
    @MainActor final class Session {
        var working = false
        var mode = "default" {
            didSet { if oldValue != mode { modeRevision &+= 1 } }
        }
        var modeRevision: UInt64 = 0
        var caps: Set<String> = ["commands", "files", "mode", "interrupt", "send.queue"]
        var commands = HarnessSamples.commands
        var completeError: (any Error)?
        var sendErrors: [any Error] = []
        var runResult = HarnessActionResult(result: .applied)
        private(set) var completes: [(HarnessCompleteParams.Kind, String)] = []
        private(set) var sends: [(String, HarnessSendParams.Mode)] = []
        private(set) var runs: [(String, String?, [String: JSONValue]?)] = []
        var interruptResult = HarnessActionResult(result: .applied)
        private(set) var interrupts = 0

        func routes() -> CommandLineModel.Routes {
            CommandLineModel.Routes(
                has: { self.caps.contains($0) },
                working: { self.working },
                mode: { self.mode },
                modes: { HarnessSamples.descriptor.modes },
                queued: { [] },
                scope: { "test" },
                complete: { kind, query in
                    self.completes.append((kind, query))
                    if let error = self.completeError { throw error }
                    return CommandLineLogic.filter(kind == .command ? self.commands : HarnessSamples.files, query: query)
                },
                send: { text, mode in
                    self.sends.append((text, mode))
                    if !self.sendErrors.isEmpty { throw self.sendErrors.removeFirst() }
                    return HarnessItem(id: "q\(self.sends.count)", seq: 1, at: HarnessSamples.at,
                                       body: .user(.init(text: text, queued: mode == .queue, source: "phone")))
                },
                interrupt: {
                    self.interrupts += 1
                    return self.interruptResult
                },
                run: { action, target, args in
                    self.runs.append((action, target, args))
                    return self.runResult
                },
                modeRevision: { self.modeRevision }
            )
        }
    }

}
