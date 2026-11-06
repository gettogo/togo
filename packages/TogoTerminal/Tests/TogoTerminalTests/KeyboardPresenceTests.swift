import GameController
import Observation
import Testing
@testable import TogoTerminal

@MainActor
struct KeyboardPresenceTests {
    final class FakeSource {
        var connected: Bool
        init(_ connected: Bool) { self.connected = connected }
    }

    final class Flag: @unchecked Sendable { var value = false }

    @Test func startsFromTheSource() {
        let source = FakeSource(true)
        let presence = KeyboardPresence(center: NotificationCenter(), isConnected: { source.connected })
        #expect(presence.hardwareKeyboardConnected)
    }

    @Test func followsConnectAndDisconnect() {
        let center = NotificationCenter()
        let source = FakeSource(false)
        let presence = KeyboardPresence(center: center, isConnected: { source.connected })
        var seen: [Bool] = []
        presence.onChange = { seen.append($0) }
        #expect(!presence.hardwareKeyboardConnected)

        source.connected = true
        center.post(name: .GCKeyboardDidConnect, object: nil)
        #expect(presence.hardwareKeyboardConnected)

        source.connected = false
        center.post(name: .GCKeyboardDidDisconnect, object: nil)
        #expect(!presence.hardwareKeyboardConnected)
        #expect(seen == [true, false])
    }

    @Test func repeatedNotificationsDoNotRepeatChanges() {
        let center = NotificationCenter()
        let source = FakeSource(true)
        let presence = KeyboardPresence(center: center, isConnected: { source.connected })
        var count = 0
        presence.onChange = { _ in count += 1 }
        center.post(name: .GCKeyboardDidConnect, object: nil)
        center.post(name: .GCKeyboardDidConnect, object: nil)
        #expect(count == 0)
    }

    @Test func readersAreToldWhenItChanges() async {
        let center = NotificationCenter()
        let source = FakeSource(false)
        let presence = KeyboardPresence(center: center, isConnected: { source.connected })
        let changed = Flag()
        withObservationTracking { _ = presence.hardwareKeyboardConnected } onChange: { changed.value = true }
        source.connected = true
        center.post(name: .GCKeyboardDidConnect, object: nil)
        #expect(changed.value)
    }
}
