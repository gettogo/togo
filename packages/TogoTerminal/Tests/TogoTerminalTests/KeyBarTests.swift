import Foundation
import Testing
import UIKit
@testable import TogoTerminal

@MainActor
@Suite struct KeyBarTests {
    func item(_ title: String, in config: KeyBarConfig) throws -> KeyBarItem {
        try #require(config.items.first { $0.title == title })
    }

    @Test func barInstallsItselfAsAccessory() {
        let h = EngineHarness()
        let bar = KeyBarView(config: .standard, engine: h.engine)
        #expect(h.engine.inputAccessory === bar)
        #expect(bar.inputViewStyle == .keyboard)
    }

    @Test func stepsEncodeThroughEngine() async throws {
        let h = EngineHarness()
        let bar = KeyBarView(config: .claude, engine: h.engine)
        bar.perform(try item("Rewind", in: .claude))
        #expect(await waitUntil { h.joinedInput.count >= 2 })
        #expect(h.joinedInput == Data([0x1b, 0x1b]))

        h.input.removeAll()
        bar.perform(try item("Interrupt", in: .claude))
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data([0x03]))

        h.input.removeAll()
        let pending = h.engine.run([.text("héllo"), .delay(milliseconds: 20), .text("!")])
        await pending?.value
        #expect(await waitUntil { h.joinedInput.count >= Data("héllo!".utf8).count })
        #expect(String(decoding: h.joinedInput, as: UTF8.self) == "héllo!")
    }

    @Test func textStepIsNeverBracketed() async {
        let h = EngineHarness()
        h.feed("\u{1b}[?2004h")
        let bar = KeyBarView(config: .standard, engine: h.engine)
        bar.perform(KeyBarItem(title: "Compact", action: .steps([.text("/compact")])))
        #expect(await waitUntil { !h.input.isEmpty })
        await settle()
        #expect(h.joinedInput == Data("/compact".utf8))
    }

    @Test func ctrlLatchOnceThenOff() async throws {
        let h = EngineHarness()
        let bar = KeyBarView(config: .standard, engine: h.engine)
        bar.perform(try item("Ctrl", in: .standard))
        #expect(bar.latch.ctrl == .once)
        h.engine.textInputView.insertText("c")
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data([0x03]))
        #expect(bar.latch.ctrl == .off)

        h.input.removeAll()
        h.engine.textInputView.insertText("c")
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("c".utf8))
    }

    @Test func ctrlLatchLocked() async throws {
        let h = EngineHarness()
        let bar = KeyBarView(config: .standard, engine: h.engine)
        let ctrl = try item("Ctrl", in: .standard)
        bar.perform(ctrl)
        bar.perform(ctrl)
        #expect(bar.latch.ctrl == .locked)
        h.engine.textInputView.insertText("a")
        h.engine.textInputView.insertText("b")
        #expect(await waitUntil { h.joinedInput.count >= 2 })
        #expect(h.joinedInput == Data([0x01, 0x02]))
        #expect(bar.button(for: ctrl)?.accessibilityValue == "Locked")

        bar.perform(ctrl)
        #expect(bar.latch.ctrl == .off)
    }

    @Test func slowSecondTapDisarms() {
        var clock = Date(timeIntervalSince1970: 0)
        let latch = ModifierLatch(now: { clock })
        latch.tapCtrl()
        clock += 1
        latch.tapCtrl()
        #expect(latch.ctrl == .off)
    }

    @Test func latchAppliesToBarKeysAndAltPrefixesEscape() async throws {
        let h = EngineHarness()
        let bar = KeyBarView(config: .standard, engine: h.engine)
        bar.perform(try item("Alt", in: .standard))
        bar.perform(try item("Left", in: .standard))
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("\u{1b}[1;3D".utf8))
        #expect(bar.latch.alt == .off)

        let latch = ModifierLatch()
        latch.tapAlt()
        #expect(latch.apply(to: Array("x".utf8)) == [0x1b, UInt8(ascii: "x")])
        latch.tapCtrl()
        #expect(latch.apply(to: Array("ab".utf8)) == Array("ab".utf8))
        #expect(latch.apply(to: Array("[".utf8)) == [0x1b])
        #expect(latch.ctrl == .off)
    }

    @Test func arrowsUseSymbolsWithLabels() throws {
        let h = EngineHarness()
        let bar = KeyBarView(config: .standard, engine: h.engine)
        for title in ["Left", "Down", "Up", "Right"] {
            let arrow = try item(title, in: .standard)
            #expect(arrow.symbol?.hasPrefix("arrow.") == true)
            #expect(bar.button(for: arrow)?.accessibilityLabel == title)
        }
    }

    @Test func dpadSwapsArrows() throws {
        let h = EngineHarness()
        let toggle = KeyBarItem(title: "Pad", action: .dpad)
        let config = KeyBarConfig(items: KeyBarConfig.standard.items + [toggle])
        let bar = KeyBarView(config: config, engine: h.engine)
        let up = try item("Up", in: config)
        // The simulator reports the Mac's keyboard on some runs, which collapses the bar.
        bar.presence.refresh(connected: false)
        let heightBefore = bar.intrinsicContentSize.height
        bar.perform(toggle)
        #expect(bar.isShowingDPad)
        #expect(bar.button(for: up)?.isHidden == true)
        #expect(bar.intrinsicContentSize.height > heightBefore)
        bar.perform(toggle)
        #expect(bar.button(for: up)?.isHidden == false)
    }

    @Test func barHidesWithHardwareKeyboard() {
        let h = EngineHarness()
        let bar = KeyBarView(config: .standard, engine: h.engine)
        let connected = bar.presence.hardwareKeyboardConnected
        bar.presence.refresh(connected: !connected)
        #expect(bar.isHidden == !connected)
        #expect((bar.intrinsicContentSize.height == 0) == !connected)
    }

    @Test func barTargetsAreAtLeast44Points() {
        let h = EngineHarness()
        let bar = KeyBarView(config: .claude, engine: h.engine)
        bar.frame = CGRect(x: 0, y: 0, width: 390, height: KeyBarView.rowHeight)
        bar.layoutIfNeeded()
        for item in KeyBarConfig.claude.items {
            let size = bar.button(for: item)?.bounds.size ?? .zero
            #expect(size.width >= 44 && size.height >= 44, "\(item.title) is \(size)")
        }
    }

    @Test func latchLocksThroughAccessibilityActions() throws {
        let h = EngineHarness()
        let bar = KeyBarView(config: .standard, engine: h.engine)
        let actions = try #require(bar.button(for: try item("Alt", in: .standard))?.accessibilityCustomActions)
        #expect(actions.map(\.name) == ["Lock", "Unlock"])
        let lock = try #require(actions.first?.actionHandler)
        let unlock = try #require(actions.last?.actionHandler)
        #expect(lock(actions[0]))
        #expect(bar.latch.alt == .locked)
        #expect(unlock(actions[1]))
        #expect(bar.latch.alt == .off)
        #expect(bar.button(for: try item("Esc", in: .standard))?.accessibilityCustomActions?.isEmpty ?? true)
    }
}
