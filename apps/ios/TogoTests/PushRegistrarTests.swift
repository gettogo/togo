import Foundation
import Testing
import UserNotifications
@testable import Togo

@MainActor struct PushRegistrarTests {
    @Test func registersOnRefreshOnlyWhenAllowed() async {
        var registered = 0
        let denied = PushRegistrar(permissions: FakePermissions(.denied), registerForRemote: { registered += 1 })
        await denied.refresh()
        #expect(denied.authorization == .denied)
        #expect(registered == 0)

        let allowed = PushRegistrar(permissions: FakePermissions(.provisional), registerForRemote: { registered += 1 })
        await allowed.refresh()
        #expect(allowed.authorization == .provisional)
        #expect(registered == 1)
    }

    @Test func refreshNeverAsks() async {
        let permissions = FakePermissions(.notDetermined)
        let registrar = PushRegistrar(permissions: permissions, registerForRemote: {})
        await registrar.refresh()
        #expect(registrar.authorization == .notDetermined)
        #expect(permissions.asked.withLock { $0 } == 0)
    }

    @Test func asksOnceThenRegisters() async {
        var registered = 0
        let permissions = FakePermissions(.notDetermined)
        let registrar = PushRegistrar(permissions: permissions, registerForRemote: { registered += 1 })
        await registrar.requestAuthorization()
        await registrar.requestAuthorization()
        #expect(permissions.asked.withLock { $0 } == 1)
        #expect(registrar.authorization == .authorized)
        #expect(registered == 2)
    }

    @Test func deniedAnswerStaysDenied() async {
        let permissions = FakePermissions(.notDetermined, answer: .denied)
        let registrar = PushRegistrar(permissions: permissions, registerForRemote: {})
        await registrar.requestAuthorization()
        #expect(registrar.authorization == .denied)
    }

    @Test func tokenIsLowercaseHexAndChangesAreReported() async {
        let registrar = PushRegistrar(permissions: FakePermissions(.authorized), registerForRemote: {})
        var changes = 0
        registrar.onTokenChange = { changes += 1 }

        registrar.didRegister(token: Data([0x0a, 0xff, 0x10]))
        registrar.didRegister(token: Data([0x0a, 0xff, 0x10]))
        registrar.didRegister(token: Data([0xbe, 0xef]))
        for _ in 0..<20 { await Task.yield() }

        #expect(registrar.apnsToken == "beef")
        #expect(changes == 2)
    }

    @Test func failedRegistrationKeepsTheLastToken() {
        let registrar = PushRegistrar(permissions: FakePermissions(.authorized), registerForRemote: {})
        registrar.didRegister(token: Data([0x01]))
        registrar.didFail(URLError(.notConnectedToInternet))
        #expect(registrar.apnsToken == "01")
    }

    @Test func bannerSuppressedForTheSessionInFront() {
        let router = NotificationRouter()
        router.frontSession = { (bib: "04", machine: "Studio") }
        #expect(router.presentation(for: "togo://session/04?host=studio") == [.list])
        #expect(router.presentation(for: "togo://session/05?host=Studio") == [.banner, .list, .sound])
        #expect(router.presentation(for: "togo://session/04?host=Build%20box") == [.banner, .list, .sound])
        #expect(router.presentation(for: nil) == [.banner, .list, .sound])
        router.frontSession = { nil }
        #expect(router.presentation(for: "togo://session/04?host=Studio") == [.banner, .list, .sound])
    }

    @Test func tapOpensTheLinkAndActionsGoToTheHandler() async {
        let router = NotificationRouter()
        var opened: [DeepLink] = []
        var actions: [String] = []
        router.open = { opened.append($0) }
        router.onAction = { actions.append($0.actionIdentifier) }

        await router.handle(NotificationResponse(actionIdentifier: UNNotificationDefaultActionIdentifier, link: "togo://session/04?host=Studio"))
        await router.handle(NotificationResponse(actionIdentifier: "togo.approve", link: nil, requestId: "r1", hostId: "h_x"))
        await router.handle(NotificationResponse(actionIdentifier: UNNotificationDismissActionIdentifier, link: nil))

        #expect(opened == [.session(bib: "04", host: "Studio")])
        #expect(actions == ["togo.approve"])
    }

    @Test func actionUserInfoCarriesOnlyIds() throws {
        let response = NotificationResponse(actionIdentifier: "togo.deny", link: nil, requestId: "r1", hostId: "h_x", sessionId: "s7")
        let info = try #require(response.userInfo["togo"] as? [String: Any])
        #expect(info["requestId"] as? String == "r1")
        #expect(info["hostId"] as? String == "h_x")
        #expect(info["sessionId"] as? String == "s7")
        #expect(info.count == 3)
    }
}
