import Testing
import TogoKit
@testable import Togo

@MainActor
struct KeyClipboardTests {
    final class FakePasteboard: KeyPasteboard {
        var changeCount = 1
        private(set) var cleared = false
        func clear() {
            cleared = true
            changeCount += 1
        }
    }

    private func flow(_ services: FakeOnboardingServices, _ pasteboard: FakePasteboard) -> AddMachineFlow {
        let flow = AddMachineFlow(services: services, save: { _ in }, finish: { _ in })
        flow.pasteboard = pasteboard
        return flow
    }

    @Test func clearsOnlyWhenNothingWasCopiedSince() {
        let board = FakePasteboard()
        let mark = ClipboardMark(board)
        board.changeCount += 1
        #expect(!mark.clearIfUnchanged(on: board))
        #expect(!board.cleared)

        let fresh = ClipboardMark(board)
        #expect(fresh.clearIfUnchanged(on: board))
        #expect(board.cleared)
    }

    @Test func pastedKeyGoesThroughTheSameImportAndClearsTheClipboard() async {
        let board = FakePasteboard()
        let services = FakeOnboardingServices()
        let flow = flow(services, board)
        await flow.importKey("KEY TEXT", label: "Pasted key", source: .clipboard(ClipboardMark(board)))
        #expect(services.importedTexts == ["KEY TEXT"])
        #expect(flow.draft.importedKey?.label == "Pasted key")
        #expect(flow.importError == nil)
        #expect(board.cleared)
    }

    @Test func pastedKeyLeavesNewerClipboardContentAlone() async {
        let board = FakePasteboard()
        let flow = flow(FakeOnboardingServices(), board)
        let mark = ClipboardMark(board)
        board.changeCount += 1
        await flow.importKey("KEY TEXT", label: "Pasted key", source: .clipboard(mark))
        #expect(!board.cleared)
    }

    @Test func fileImportNeverTouchesTheClipboard() async {
        let board = FakePasteboard()
        let flow = flow(FakeOnboardingServices(), board)
        await flow.importKey("KEY TEXT", label: "id_ed25519")
        #expect(flow.draft.importedKey != nil)
        #expect(!board.cleared)
    }

    @Test func badPasteShowsAPlainErrorAndKeepsTheClipboard() async throws {
        let board = FakePasteboard()
        let services = FakeOnboardingServices()
        services.importFails = true
        let flow = flow(services, board)
        await flow.importKey("hello", label: "Pasted key", source: .clipboard(ClipboardMark(board)))
        let message = try #require(flow.importError)
        #expect(!message.contains("!") && !message.contains("\u{2014}"))
        #expect(flow.draft.importedKey == nil)
        #expect(!board.cleared)
    }
}
