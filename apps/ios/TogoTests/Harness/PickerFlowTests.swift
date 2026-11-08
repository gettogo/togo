import Foundation
import Testing
import TogoKit
@testable import Togo

/// Pickers: a tap selects even with item actions present, a selection that lands closes the
/// host, a failure keeps it, and a restart goes through ActionGate.
@MainActor struct PickerFlowTests {
    private let picker = HarnessScreen.Picker(
        items: [.init(id: "opus", title: "Opus"), .init(id: "sonnet", title: "Sonnet")],
        selected: "opus", searchable: false, onSelect: "set_model",
        onSelectConfirm: HarnessSelectConfirm(text: "Restart with this model?", effect: .relaunch),
        itemActions: [HarnessActionRef(id: "role_smol", label: "Small")]
    )

    @Test func aTapSelectsWithItemActionsPresent() {
        let model = PickerModel(picker)
        #expect(model.tap(picker.items[1], in: picker) == .select)
        #expect(model.tap(picker.items[0], in: picker) == .none)
    }

    @Test func closesPerResult() {
        func closes(_ outcome: HarnessActionResult.Outcome) -> Bool {
            PickerModel.closes(HarnessActionResult(result: outcome))
        }
        let landed = [HarnessActionResult.Outcome.applied, .typed, .relaunched, .queued].map(closes)
        #expect(landed == [true, true, true, true])
        let stays = [HarnessActionResult.Outcome.failed, .fallback].map(closes)
        #expect(stays == [false, false])
        let none = PickerModel.closes(nil)
        #expect(none == false)
    }

    @Test func aSelectionCarriesItsConfirmAndThePreviousValue() {
        let change = PickerModel.change(selecting: picker.items[1], in: picker, setting: "Model")
        #expect(change.action == "set_model")
        #expect(change.target == "sonnet")
        #expect(change.args == ["id": .string("sonnet")])
        #expect(change.confirm?.effect == .relaunch)
        #expect(change.value == "Sonnet")
        #expect(change.previous?.label == "Opus")
        #expect(change.previous?.args == ["id": .string("opus")])
    }

    private func context(working: Bool, result: HarnessActionResult.Outcome, settled: @escaping @MainActor () -> Void = {}) -> PrimitiveContext {
        var context = PrimitiveContext(
            run: { _, _, _ in HarnessActionResult(result: result, message: result == .failed ? "Model not found" : nil) },
            openScreen: { _ in }, open: { _ in }, showTerminal: {}
        )
        context.working = { working }
        context.settled = settled
        context.note = { _, _, _ in }
        return context
    }

    @Test func noRelaunchMidTurnWithoutTheDialog() async {
        let actions = PrimitiveActions()
        var results: [HarnessActionResult?] = []
        let change = PickerModel.change(selecting: picker.items[1], in: picker, setting: "Model")
        await actions.change(change, context: context(working: true, result: .relaunched)) { results.append($0) }
        #expect(results.isEmpty)
        #expect(actions.asking?.dialog.offersAfterReply == true)
    }

    @Test func aFailedSelectionStays() async {
        let actions = PrimitiveActions()
        var result: HarnessActionResult?
        let change = PickerModel.change(selecting: picker.items[1], in: picker, setting: "Model")
        await actions.change(change, context: context(working: false, result: .failed)) { result = $0 }
        let closed = PickerModel.closes(result)
        #expect(closed == false)
        #expect(result?.message == "Model not found")
    }

    @Test func settingsChangesCarryTheFieldsConfirm() {
        let field = HarnessScreen.Settings.Field(
            id: "effort", label: "Effort", kind: .choice, value: .string("high"),
            options: [.init(id: "low", label: "Low"), .init(id: "high", label: "High")], action: "set_effort",
            actionConfirm: HarnessSelectConfirm(text: "Restart?", effect: .relaunch)
        )
        let change = SettingsLogic.change(field, to: .string("low"), action: "set_effort")
        #expect(change.value == "Low")
        #expect(change.previous?.label == "High")
        #expect(change.confirm?.effect == .relaunch)
        #expect(SettingsLogic.isSingleField(.init(sections: [.init(fields: [field])])))
        let secret = HarnessScreen.Settings.Field(id: "key", label: "API key", kind: .secret, value: .null, isSet: true, action: "set_key")
        #expect(SettingsLogic.change(secret, to: .string("sk-123"), action: "set_key").value == PrimitiveCopy.set)
        #expect(SettingsLogic.change(secret, to: .string("sk-123"), action: "set_key").previous == nil)
    }
}
