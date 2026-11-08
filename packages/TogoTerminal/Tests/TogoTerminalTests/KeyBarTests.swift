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
        // A plain input view: the keyboard style draws a black backdrop behind the row.
        #expect(bar.inputViewStyle == .default)
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

    @Test func slimKeysFillTheRowHeightAndAMinimumWidth() {
        let h = EngineHarness()
        let bar = KeyBarView(config: .claude, engine: h.engine)
        bar.frame = CGRect(x: 0, y: 0, width: 390, height: KeyBarView.rowHeight)
        bar.layoutIfNeeded()
        for item in KeyBarConfig.claude.items {
            let size = bar.button(for: item)?.bounds.size ?? .zero
            #expect(size.width >= KeyBarView.minimumKeyWidth && size.height >= KeyBarView.rowHeight, "\(item.title) is \(size)")
        }
    }

    /// Every key takes a touch at least 44 points square, while its face stays compact.
    @Test func everyKeyIsA44PointTarget() {
        let h = EngineHarness()
        for config in [KeyBarConfig.claude, .standard] {
            let bar = KeyBarView(config: config, engine: h.engine)
            bar.frame = CGRect(x: 0, y: 0, width: 390, height: KeyBarView.rowHeight)
            bar.layoutIfNeeded()
            for item in config.items {
                guard let button = bar.button(for: item) as? KeyTargetButton, !button.isHidden else {
                    Issue.record("\(item.title) is not a key target")
                    continue
                }
                let target = button.touchTarget
                #expect(target.width >= 44 && target.height >= 44, "\(item.title) takes \(target.size)")
                #expect(button.point(inside: CGPoint(x: -KeyBarView.keySpacing / 2 + 0.5, y: 1), with: nil))
                #expect(!button.point(inside: CGPoint(x: -KeyBarView.keySpacing, y: 1), with: nil))
            }
        }
        #expect(KeyBarView.rowHeight - 2 * KeyBarButton.faceInset <= 30, "the face stays as compact as before")
    }

    /// Armed reads apart from off by an outline, not only its tint, under Differentiate Without Color.
    @Test func armedModifierIsOutlinedWithoutColour() {
        let button = KeyBarButton.make(title: "Ctrl", symbol: nil, slim: true)
        KeyBarButton.show(.once, on: button, differentiateWithoutColor: true)
        #expect(button.configuration?.background.strokeWidth == 2)
        KeyBarButton.show(.off, on: button, differentiateWithoutColor: true)
        #expect(button.configuration?.background.strokeWidth != 2)
        KeyBarButton.show(.once, on: button, differentiateWithoutColor: false)
        #expect(button.configuration?.background.strokeWidth != 2)
    }

    @Test func keysThatFitShareTheWidth() {
        let pages = KeyRowLayout.pages(widths: [40, 60, 50], available: 300, spacing: 4)
        #expect(pages.count == 1)
        let total = pages[0].reduce(0) { $0 + $1.width } + 8
        #expect(abs(total - 300) < 0.001)
    }

    @Test func keysThatDoNotFitPageInWholeKeysThatFillEachPage() {
        let widths: [CGFloat] = [60, 60, 60, 60, 60, 60, 60]
        let pages = KeyRowLayout.pages(widths: widths, available: 200, spacing: 4)
        #expect(pages.count == 3)
        #expect(pages.flatMap { $0.map(\.index) } == Array(0..<7))
        for page in pages.dropLast() {
            let total = page.reduce(0) { $0 + $1.width } + 4 * CGFloat(page.count - 1)
            #expect(abs(total - 200) < 0.001)
        }
        #expect(pages.last?.first?.width ?? 0 <= 60 * KeyRowLayout.maximumStretch)
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

    @Test func composeItemRunsTheComposeClosureAndSendsNothing() throws {
        let h = EngineHarness()
        let bar = KeyBarView(config: .claude, engine: h.engine)
        var toggles = 0
        bar.onCompose = { toggles += 1 }
        bar.perform(try item("Compose", in: .claude))
        bar.perform(try item("Compose", in: .standard))
        #expect(toggles == 2)
        #expect(h.joinedInput.isEmpty)
    }

    /// While the host keeps its compose bar away, the keys that open it go away too, and come
    /// back where they were.
    @Test func composeKeysHideWhileHostHidesCompose() throws {
        let h = EngineHarness()
        let bar = KeyBarView(config: .standard, engine: h.engine)
        let compose = try #require(bar.button(for: try item("Compose", in: .standard)))
        let dictate = try #require(bar.button(for: try item("Dictate", in: .standard)))
        let escape = try #require(bar.button(for: try item("Esc", in: .standard)))
        #expect(!compose.isHidden && !dictate.isHidden)

        bar.hidesCompose = true
        #expect(compose.isHidden && dictate.isHidden)
        #expect(!escape.isHidden)

        bar.hidesCompose = false
        #expect(!compose.isHidden && !dictate.isHidden)
    }

    @Test func keyBarDefaultHasDictateKey() throws {
        let h = EngineHarness()
        let dictate = try item("Dictate", in: .standard)
        #expect(dictate.symbol == "mic")
        #expect(dictate.action == .dictate)
        #expect(try item("Dictate", in: .claude).action == .dictate)
        let bar = KeyBarView(config: .standard, engine: h.engine)
        var requests = 0
        bar.onDictate = { requests += 1 }
        bar.perform(dictate)
        #expect(requests == 1)
        #expect(h.joinedInput.isEmpty)
    }

    /// The system keyboard's own dictation arrives as one insert and takes the paste filter.
    @Test func dictationIntoTerminalIsFiltered() async {
        let h = EngineHarness()
        _ = KeyBarView(config: .standard, engine: h.engine)
        h.engine.textInputView.insertText("a\u{1b}b")
        #expect(await waitUntil { !h.input.isEmpty })
        await settle()
        #expect(h.joinedInput == Data("ab".utf8))
    }
}
