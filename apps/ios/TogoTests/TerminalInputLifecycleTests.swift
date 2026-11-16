import Foundation
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct TerminalInputLifecycleTests {
    private final class Machine { var support = ImageSupport.unsupported }
    @Test func capabilitiesArrivingAfterToolCreationAreUsed() async throws {
        let machine = Machine()
        var pasted: [String] = []
        let images = ImagePasteFlow(support: machine.support, upload: { _, _ in "/image.png" },
                                   paste: { pasted.append($0) }, supportNow: { machine.support })
        #expect(!images.picksPhotos)
        images.explainUnsupported()
        #expect(images.phase == .unsupported)
        machine.support = .available
        #expect(images.picksPhotos)
        await images.begin(try #require(Fixtures.noise(width: 8, height: 8).pngData()))
        #expect(images.isAnnotating)
        await images.send(annotated: nil)
        #expect(pasted == [shellQuote(["/image.png"]) + " "])
        machine.support = .none
        #expect(!images.picksPhotos)
        images.explainUnsupported()
        #expect(images.phase == .noHelper)
    }

    @Test func closedTabToolsArePrunedAndCannotTypeLate() {
        var pasted: [String] = []
        func model() -> TerminalInputModel {
            TerminalInputModel(images: ImagePasteFlow(support: .available, upload: { _, _ in "" }, paste: { _ in }),
                               paste: { pasted.append($0) })
        }
        let first = TabID(hostID: UUID(), sessionID: "first")
        let second = TabID(hostID: first.hostID, sessionID: "second")
        let closed = model(), retained = model()
        var tools = [first: closed, second: retained]
        TerminalInputModel.prune(&tools, keeping: [second])
        #expect(tools.keys.sorted { $0.sessionID < $1.sessionID } == [second])
        #expect(closed.retired)
        #expect(!retained.retired)
        closed.type("late")
        retained.type("still here")
        #expect(pasted == ["still here"])
        TerminalInputModel.prune(&tools, keeping: [])
        #expect(tools.isEmpty)
    }
}
