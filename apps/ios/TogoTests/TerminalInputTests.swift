import Foundation
import Testing
import TogoKit
import UIKit
@testable import Togo

/// The terminal's input tools: the terminal takes the typing itself, and dictation and pictures
/// end by typing into it as a paste. Nothing here can press Enter.
@MainActor
struct TerminalInputTests {
    private func tools(log: Log, support: ImageSupport = .available, upload: String = "/tmp/togo/shot.png") -> TerminalInputModel {
        TerminalInputModel(
            images: ImagePasteFlow(support: support, upload: { _, _ in upload }, paste: { log.events.append("paste:\($0)") }),
            paste: { log.events.append("paste:\($0)") }
        )
    }

    @Test func typedWordsArePastedCleanAndNeverSubmitted() {
        let log = Log()
        let tools = tools(log: log)
        tools.type("run\u{1b}[2J the tests\u{9b}\n")
        tools.type("   ")
        tools.type("\u{1b}")
        #expect(log.events == ["paste:run[2J the tests"])
    }

    @Test func withoutSpeechThereIsNoDictation() {
        #expect(tools(log: Log()).dictation == nil)
    }

    /// A picture that went up has its path typed at the prompt, with no Enter after it.
    @Test func aSentPictureTypesItsPath() async throws {
        let log = Log()
        let tools = tools(log: log)
        let png = try #require(Fixtures.noise(width: 64, height: 48).pngData())
        await tools.images.begin(png)
        #expect(tools.images.isAnnotating)
        await tools.images.send(annotated: nil)
        #expect(tools.images.phase == .idle)
        #expect(log.events.count == 1)
        #expect(log.events.first?.hasPrefix("paste:") == true)
        #expect(log.events.first?.contains("/tmp/togo/shot.png") == true)
    }

    /// Words waiting for a tab are only ever handed over once.
    @Test func waitingWordsAreTakenOnce() {
        let store = AppStore(services: FakeAppServices())
        let tab = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.pendingInput[tab] = "keep going"
        #expect(store.takePendingInput(tab) == "keep going")
        #expect(store.takePendingInput(tab) == nil)
    }
}

@MainActor final class Log {
    var events: [String] = []
}
