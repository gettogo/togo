import Testing
@testable import Togo

@MainActor struct PendingForgetTests {
    @Test(arguments: [PendingRemoval.Kind.unpair, .key(comment: "test-key", manual: false)])
    func abandoningRemovalRequiresAnExplicitChoice(_ kind: PendingRemoval.Kind) {
        let removal = PendingRemoval(id: "pending-test", name: "test", kind: kind)
        var model = PendingRemovalForget()
        var forgotten: [String] = []
        model.request(removal)
        #expect(forgotten.isEmpty)
        #expect(model.pending == removal)
        model.cancel()
        model.confirm { forgotten.append($0) }
        #expect(forgotten.isEmpty)
        model.request(removal)
        model.confirm { forgotten.append($0) }
        model.confirm { forgotten.append($0) }
        #expect(forgotten == [removal.id])
        #expect(model.pending == nil)
    }
}
