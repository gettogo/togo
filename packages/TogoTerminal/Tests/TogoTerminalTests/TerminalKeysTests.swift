import Foundation
import Testing
import UIKit
@testable import TogoTerminal

@Suite struct ControllerGestureTests {
    typealias Direction = ControllerGesture.Direction

    @Test func shortTouchWithoutMovingIsATap() {
        var g = ControllerGesture()
        #expect(g.touchBegan(at: .zero, time: 0).isEmpty)
        #expect(g.touchMoved(to: CGPoint(x: 3, y: 4), time: 0.05).isEmpty)
        #expect(g.touchEnded(time: 0.2) == [.tap])
        #expect(g.phase == .idle)
    }

    @Test func slowReleaseWithoutMovingIsNothing() {
        var g = ControllerGesture()
        _ = g.touchBegan(at: .zero, time: 0)
        #expect(g.touchEnded(time: 0.3).isEmpty)
    }

    @Test(arguments: [
        (CGPoint(x: 10, y: 0), Direction.right), (CGPoint(x: -10, y: 0), .left),
        (CGPoint(x: 0, y: 10), .down), (CGPoint(x: 0, y: -10), .up),
        (CGPoint(x: 9, y: -9), .right), (CGPoint(x: -6, y: 8), .down),
    ])
    func moveBeforeTheHoldIsAFlickInTheNearestQuadrant(point: CGPoint, expected: Direction) {
        var g = ControllerGesture()
        _ = g.touchBegan(at: .zero, time: 0)
        #expect(g.touchMoved(to: point, time: 0.1) == [.arrow(expected)])
        #expect(g.phase == .flick(expected))
        #expect(g.touchEnded(time: 0.5) == [.flickEnded])
    }

    @Test func movingLessThanTheSlopIsNotAFlick() {
        var g = ControllerGesture()
        _ = g.touchBegan(at: .zero, time: 0)
        #expect(g.touchMoved(to: CGPoint(x: 7.9, y: 0), time: 0.1).isEmpty)
        #expect(g.phase == .undecided)
    }

    @Test func changingQuadrantSendsTheNewArrowAtOnce() {
        var g = ControllerGesture()
        _ = g.touchBegan(at: .zero, time: 0)
        #expect(g.touchMoved(to: CGPoint(x: 12, y: 0), time: 0.1) == [.arrow(.right)])
        #expect(g.touchMoved(to: CGPoint(x: 14, y: 2), time: 0.15).isEmpty)
        #expect(g.touchMoved(to: CGPoint(x: 2, y: 20), time: 0.2) == [.arrow(.down)])
    }

    @Test func farFromTouchdownTheRepeatIsFast() {
        var g = ControllerGesture()
        _ = g.touchBegan(at: .zero, time: 0)
        _ = g.touchMoved(to: CGPoint(x: 12, y: 0), time: 0.1)
        #expect(!g.fast)
        _ = g.touchMoved(to: CGPoint(x: 31, y: 0), time: 0.2)
        #expect(g.fast)
        _ = g.touchMoved(to: CGPoint(x: 20, y: 0), time: 0.3)
        #expect(!g.fast)
    }

    @Test func stayingStillForTheHoldDelayIsAHold() {
        var g = ControllerGesture()
        _ = g.touchBegan(at: .zero, time: 0)
        #expect(g.holdTimerFired(time: 0.3).isEmpty, "the timer is checked against the delay")
        #expect(g.holdTimerFired(time: 0.35) == [.holdBegan])
        #expect(g.phase == .held)
        #expect(g.touchMoved(to: CGPoint(x: 40, y: 0), time: 0.5).isEmpty, "a held face does not flick")
        #expect(g.touchEnded(time: 0.6) == [.holdEnded])
    }

    @Test func aFlickThatBeganFirstWinsOverTheTimer() {
        var g = ControllerGesture()
        _ = g.touchBegan(at: .zero, time: 0)
        _ = g.touchMoved(to: CGPoint(x: 12, y: 0), time: 0.2)
        #expect(g.holdTimerFired(time: 0.35).isEmpty)
        #expect(g.phase == .flick(.right))
    }

    @Test func aMoveAfterTheHoldDelayDoesNotFlick() {
        var g = ControllerGesture()
        _ = g.touchBegan(at: .zero, time: 0)
        #expect(g.touchMoved(to: CGPoint(x: 12, y: 0), time: 0.4).isEmpty)
    }

    @Test func withFlicksOffMovesAreIgnoredButTapAndHoldRemain() {
        var g = ControllerGesture(flicks: false)
        _ = g.touchBegan(at: .zero, time: 0)
        #expect(g.touchMoved(to: CGPoint(x: 30, y: 0), time: 0.1).isEmpty)
        #expect(g.touchEnded(time: 0.2) == [.tap])
        _ = g.touchBegan(at: .zero, time: 1)
        #expect(g.holdTimerFired(time: 1.35) == [.holdBegan])
    }

    @Test func cancelEndsWhateverWasHappening() {
        var g = ControllerGesture()
        _ = g.touchBegan(at: .zero, time: 0)
        _ = g.touchMoved(to: CGPoint(x: 12, y: 0), time: 0.1)
        #expect(g.touchCancelled() == [.cancelled])
        #expect(g.phase == .idle)
        #expect(g.touchCancelled().isEmpty)
    }

    @Test func repeatTiers() {
        #expect(ArrowRepeat.delay(beforeRepeat: 0, fast: false) == 0.4)
        #expect(ArrowRepeat.delay(beforeRepeat: 0, fast: true) == 0.4)
        #expect(abs(ArrowRepeat.delay(beforeRepeat: 1, fast: false) - 1 / 12) < 0.0001)
        #expect(abs(ArrowRepeat.delay(beforeRepeat: 5, fast: true) - 1 / 25) < 0.0001)
    }
}

@MainActor
@Suite struct ControllerFaceTests {
    @Test func eventsReachTheCallbacks() {
        let face = ControllerFace(now: { 0 })
        var arrows: [ControllerGesture.Direction] = []
        var taps = 0, holds = 0, ended = 0
        face.onArrow = { arrows.append($0) }
        face.onTap = { taps += 1 }
        face.onHoldEnded = { holds += 1 }
        face.onFlickEnded = { ended += 1 }
        face.feed([.arrow(.up), .flickEnded, .tap, .holdBegan, .holdEnded])
        #expect(arrows == [.up] && taps == 1 && holds == 1 && ended == 1)
    }

    @Test func voiceOverSwipesAndActionsSendArrowsWithoutTheGesture() {
        let face = ControllerFace(now: { 0 })
        var arrows: [ControllerGesture.Direction] = []
        var ended = 0
        face.onArrow = { arrows.append($0) }
        face.onFlickEnded = { ended += 1 }
        face.accessibilityIncrement()
        face.accessibilityDecrement()
        let actions = face.accessibilityCustomActions ?? []
        #expect(actions.map(\.name) == ["Left", "Right", "Edit keys"])
        _ = actions[0].actionHandler?(actions[0])
        #expect(arrows == [.up, .down, .left])
        #expect(ended == 3, "an accessibility arrow is one press, never a repeat")
        #expect(face.accessibilityTraits.contains(.adjustable) && face.accessibilityTraits.contains(.button))
    }
}

@Suite struct TerminalKeysPlanTests {
    func situation(_ change: (inout TerminalKeysSituation) -> Void = { _ in }) -> TerminalKeysSituation {
        var s = TerminalKeysSituation()
        change(&s)
        return s
    }

    @Test func nothingShowsWithTheKeyboardDownAndNothingWaiting() {
        #expect(TerminalKeysPlan.plan(situation()).isEmpty)
    }

    @Test func typingOnAPhoneStandsTheColumnWithFourKeys() {
        let plan = TerminalKeysPlan.plan(situation { $0.typing = true })
        #expect(plan.column?.keys == [.esc, .ctrl, .mode, .stop])
        #expect(plan.row == [.key(.hideKeyboard)], "the keyboard's way down is always there")
        #expect(!plan.controllerInBar)
    }

    @Test func compactHeightKeepsEscAndCtrl() {
        let plan = TerminalKeysPlan.plan(situation { $0.typing = true; $0.compactHeight = true })
        #expect(plan.column?.keys == [.esc, .ctrl])
    }

    @Test func aVerticalBarTakesTheControllerAndTheRowLeadsWithTheFourKeys() {
        let plan = TerminalKeysPlan.plan(situation { $0.typing = true; $0.verticalBar = true })
        #expect(plan.column == nil && plan.controllerInBar)
        #expect(plan.row == [.key(.esc), .key(.ctrl), .key(.mode), .key(.stop), .key(.hideKeyboard)])
    }

    @Test func aVerticalBarWithTheKeyboardDownShowsOnlyShortcuts() {
        let snippet = KeyBarItem(title: "/compact", action: .steps([.text("/compact")]))
        let plan = TerminalKeysPlan.plan(situation { $0.verticalBar = true; $0.shortcuts = [snippet] })
        #expect(plan.row == [.shortcut(snippet)] && plan.controllerInBar)
    }

    @Test func aHardwareKeyboardHidesTheColumnAndTheFourKeys() {
        let prompt = PromptKey(id: "1", label: "Yes", filled: true, kind: .answer)
        let plan = TerminalKeysPlan.plan(situation {
            $0.hardwareKeyboard = true; $0.verticalBar = true; $0.typing = true; $0.promptKeys = [prompt]
        })
        #expect(plan.column == nil)
        #expect(plan.row == [.prompt(prompt)], "a hardware keyboard has no Hide key")
    }

    @Test func hideKeyboardAloneIsReasonToDrawTheRow() {
        let plan = TerminalKeysPlan.plan(situation { $0.typing = true; $0.verticalBar = true; $0.hardwareKeyboard = false })
        #expect(plan.row.last == .key(.hideKeyboard))
        #expect(!TerminalKeysPlan.plan(situation { $0.keep = true; $0.config.showColumn = .alsoOnDemand }).row.contains(.key(.hideKeyboard)))
    }

    @Test func keepHoldsTheColumnOnlyWhenTheSettingAllowsIt() {
        #expect(TerminalKeysPlan.plan(situation { $0.keep = true }).column == nil)
        let kept = TerminalKeysPlan.plan(situation { $0.keep = true; $0.config.showColumn = .alsoOnDemand })
        #expect(kept.column?.keys == [.esc, .ctrl, .mode, .stop])
        let hardware = TerminalKeysPlan.plan(situation {
            $0.keep = true; $0.config.showColumn = .alsoOnDemand; $0.hardwareKeyboard = true
        })
        #expect(hardware.column?.keys == [], "kept under a hardware keyboard: the controller alone")
    }

    @Test func promptKeysFollowTheSettingAndAnAgentWithoutAModeDropsIt() {
        let prompt = PromptKey(id: "1", label: "Yes", kind: .answer)
        let off = TerminalKeysPlan.plan(situation { $0.promptKeys = [prompt]; $0.config.promptKeys = false })
        #expect(off.row.isEmpty)
        let noMode = TerminalKeysPlan.plan(situation { $0.typing = true; $0.hasMode = false })
        #expect(noMode.column?.keys == [.esc, .ctrl, .stop])
    }
}

@Suite struct TerminalKeysConfigTests {
    @Test func roundTripsAndReadsMissingFieldsAsDefaults() throws {
        var config = TerminalKeysConfig()
        config.controller = false
        config.shortcuts = [KeyBarItem(title: "Deploy", action: .steps([.text("make deploy")]))]
        config.shortcutsByAgent = ["claude": [KeyBarItem(title: "/compact", action: .steps([.text("/compact")]))]]
        config.palette = [.enter, .tab]
        let data = try JSONEncoder().encode(config)
        #expect(TerminalKeysConfig.decode(data) == config)
        let sparse = try #require(#"{"version":2,"promptKeys":false}"#.data(using: .utf8))
        let decoded = TerminalKeysConfig.decode(sparse)
        #expect(!decoded.promptKeys && decoded.controller && decoded.palette == KeyRef.defaultPalette)
    }

    @Test func unknownKeysAndBadItemsAreDroppedNotFatal() throws {
        var config = TerminalKeysConfig()
        config.shortcuts = [KeyBarItem(title: "ok", action: .steps([.text("x")]))]
        config.palette = [.tab]
        var json = try #require(try JSONSerialization.jsonObject(with: JSONEncoder().encode(config)) as? [String: Any])
        json["palette"] = ["tab", "hologram"]
        json["shortcuts"] = try #require(json["shortcuts"] as? [Any]) + [["nonsense": 1]]
        let decoded = TerminalKeysConfig.decode(try JSONSerialization.data(withJSONObject: json))
        #expect(decoded.palette == [.tab])
        #expect(decoded.shortcuts.map(\.title) == ["ok"])
        #expect(TerminalKeysConfig.decode(Data("junk".utf8)) == TerminalKeysConfig())
    }

    @Test func anUneditedOldBarBecomesTheDefaults() throws {
        for preset in [KeyBarConfig.standard, .claude, .omp] {
            let data = try JSONEncoder().encode(preset)
            #expect(TerminalKeysConfig.decode(data) == TerminalKeysConfig())
        }
    }

    @Test func aCustomisedOldBarKeepsItsOwnKeysAsShortcuts() throws {
        var bar = KeyBarConfig.standard
        let deploy = KeyBarItem(title: "Deploy", action: .steps([.text("make deploy")]))
        bar.items.insert(deploy, at: 2)
        bar.items.removeLast()
        let config = TerminalKeysConfig.decode(try JSONEncoder().encode(bar))
        #expect(config.shortcuts == [deploy], "built-in keys live in the column and palette now")
        #expect(config.controller && config.promptKeys)
    }

    @Test func shortcutsForASessionPutTheAgentsOwnFirst() {
        var config = TerminalKeysConfig()
        let all = KeyBarItem(title: "git st", action: .steps([.text("git status")]))
        let own = KeyBarItem(title: "/compact", action: .steps([.text("/compact")]))
        config.shortcuts = [all]
        config.shortcutsByAgent = ["claude": [own]]
        #expect(config.shortcuts(for: "Claude") == [own, all])
        #expect(config.shortcuts(for: "shell") == [all])
        #expect(config.shortcuts(for: nil) == [all])
    }

    @Test func agentTable() {
        #expect(AgentKeys.keys(for: "shell").mode == KeyStroke(.tab))
        #expect(AgentKeys.keys(for: "OpenCode").mode == KeyStroke(.tab))
        #expect(AgentKeys.keys(for: "claude") == AgentKeys(mode: KeyStroke(.tab, .shift), stop: KeyStroke(.escape)))
        #expect(AgentKeys.keys(for: "codex").mode == nil)
        #expect(AgentKeys.keys(for: "never-heard-of") == .fallback)
        #expect(AgentKeys.keys(for: nil) == .fallback)
    }
}

@MainActor
@Suite struct TerminalKeysOverlayTests {
    /// Holds the engine's harness for the test's whole run: the engine calls it back.
    @MainActor final class Fixture {
        let h = EngineHarness()
        let terminal: TerminalViewController
        let host = UIView(frame: CGRect(x: 0, y: 0, width: 400, height: 800))
        let overlay: TerminalKeysOverlay

        init() {
            terminal = TerminalViewController(engine: h.engine)
            terminal.view.frame = host.bounds
            host.addSubview(terminal.view)
            let presence = KeyboardPresence(center: NotificationCenter(), isConnected: { false })
            overlay = TerminalKeysOverlay(engine: h.engine, terminal: terminal, host: host, presence: presence)
        }
    }

    @Test func nothingShowsUntilSomethingIsNeeded() {
        let f = Fixture()
        #expect(f.overlay.row.isHidden && f.overlay.column.isHidden)
        #expect(f.overlay.plan.isEmpty)
    }

    @Test func promptKeysBringTheRowAndItsAnswerReachesTheHost() {
        let f = Fixture()
        var answered: [String] = []
        f.overlay.onPrompt = { answered.append($0.id) }
        let yes = PromptKey(id: "opt-1", label: "Allow once", filled: true, kind: .answer)
        var input = TerminalKeysInput()
        input.promptKeys = [yes]
        f.overlay.input = input
        #expect(!f.overlay.row.isHidden && f.overlay.row.items == [.prompt(yes)])
        let button = f.overlay.row.subviews.compactMap { $0 as? UIScrollView }.first?.subviews.first?.subviews
            .compactMap { $0 as? UIButton }.first
        button?.sendActions(for: .touchUpInside)
        #expect(answered == ["opt-1"])
    }

    @Test func keysSendThroughTheLatchAndTheAgentTable() async {
        let f = Fixture()
        var input = TerminalKeysInput()
        input.agent = "shell"
        f.overlay.input = input
        f.overlay.press(.mode)
        #expect(await waitUntil { !f.h.input.isEmpty })
        #expect(f.h.joinedInput == Data([0x09]), "a shell's Mode is Tab")
        f.h.input.removeAll()
        f.overlay.press(.ctrl)
        #expect(f.overlay.latch.ctrl == .once)
        f.overlay.press(.esc)
        #expect(await waitUntil { !f.h.input.isEmpty })
        #expect(f.overlay.latch.ctrl == .off, "a one-shot modifier is spent by the next key")
        f.h.input.removeAll()
        f.overlay.perform(KeyBarItem(title: "ls", action: .steps([.text("ls")])))
        #expect(await waitUntil { f.h.joinedInput.count >= 2 })
        #expect(f.h.joinedInput == Data("ls".utf8))
    }

    @Test func theGridYieldsOnlyWhatIsShown() {
        let f = Fixture()
        let base = f.terminal.gridMargins
        var input = TerminalKeysInput()
        input.promptKeys = [PromptKey(id: "1", label: "Yes", kind: .answer)]
        f.overlay.input = input
        #expect(f.terminal.gridMargins.bottom == base.bottom + InputsRowView.height + 6)
        #expect(f.terminal.gridMargins.right == base.right, "no column without a keyboard")
        f.overlay.input = TerminalKeysInput()
        #expect(f.terminal.gridMargins == base)
    }

    @Test func aFlickOnAnAttachedFaceSendsTheArrow() async {
        let f = Fixture()
        let face = ControllerFace(now: { 0 })
        f.overlay.attach(face)
        face.feed([.arrow(.down), .flickEnded])
        #expect(await waitUntil { !f.h.input.isEmpty })
        #expect(f.h.joinedInput == Data([0x1b, 0x5b, 0x42]))
    }
}
