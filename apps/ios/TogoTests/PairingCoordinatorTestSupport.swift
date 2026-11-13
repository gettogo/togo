import Foundation
import Synchronization
import Testing
import TogoKit
import TogoPush
@testable import Togo

/// Answers for iOS: permission as set, and whether the question was shown.
final class FakePermissions: NotificationPermissions {
    let state: Mutex<PushRegistrar.Authorization>
    let answer: PushRegistrar.Authorization
    let asked = Mutex(0)

    init(_ state: PushRegistrar.Authorization, answer: PushRegistrar.Authorization = .authorized) {
        self.state = Mutex(state)
        self.answer = answer
    }

    func authorization() async -> PushRegistrar.Authorization { state.withLock { $0 } }

    func request() async throws -> Bool {
        asked.withLock { $0 += 1 }
        state.withLock { $0 = answer }
        return answer == .authorized
    }
}

final class PairingRecordingRegister: PushTokenRegistering {
    let sent = Mutex<[(hostId: String, apns: String?)]>([])
    let live = Mutex<[String: String?]>([:])
    let commands = Mutex<[String: Bool]>([:])
    let commandLog = Mutex<[Bool]>([])
    let ended = Mutex<[(hostId: String, token: String)]>([])

    func send(
        _ tokens: (apns: String?, liveActivity: String?, liveActivityStart: String?), liveActivityCommand: Bool,
        liveActivityEnded: String?, pairing: Pairing
    ) async throws {
        if let liveActivityEnded { ended.withLock { $0.append((pairing.hostId, liveActivityEnded)) } }
        sent.withLock { $0.append((pairing.hostId, tokens.apns)) }
        commands.withLock { $0[pairing.hostId] = liveActivityCommand }
        commandLog.withLock { $0.append(liveActivityCommand) }
        live.withLock { $0[pairing.hostId] = tokens.liveActivity }
    }
}

@MainActor final class PairingCoordinatorHelperStub {
    var pairs: [PushPairParams] = []
    var unpaired: [String] = []

    func pair(_ params: PushPairParams) async throws -> PushPairResult {
        pairs.append(params)
        return PushPairResult(hostId: "h_studio", hostKey: Base64URL.encode(Data(repeating: 7, count: 32)))
    }
}
