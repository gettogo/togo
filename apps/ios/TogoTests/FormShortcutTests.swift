import Foundation
import Testing
@testable import Togo

@MainActor struct FormShortcutTests {
    @Test(arguments: [false, true])
    func navigationShortcutsCannotDismissOrStealFocusFromAForm(_ inline: Bool) {
        let store = AppStore(services: FakeAppServices())
        store.workspaceInline = inline
        let forms: [AppSheet] = [.addMachine, .editMachine(Samples.studio), .newSession(nil),
                                 .signIn(Samples.studio), .setUpHere(Samples.studio)]
        for form in forms {
            store.workspaceOpen = true
            store.sheet = form
            let search = store.sidebar.searchRequests
            let starts = store.startRequests
            store.showControlCenter()
            store.requestStart()
            #expect(store.sheet == form)
            #expect(store.workspaceOpen)
            #expect(store.sidebar.searchRequests == search)
            #expect(store.startRequests == starts)
        }
    }

    @Test func searchStillOpensNormallyWithoutAForm() {
        let store = AppStore(services: FakeAppServices())
        store.workspaceOpen = true
        store.sheet = .settings
        store.showControlCenter()
        #expect(store.sheet == nil)
        #expect(!store.workspaceOpen)
        #expect(store.sidebar.searchPending)
    }
}
