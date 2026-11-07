import Foundation
import Testing
import TogoKit
@testable import Togo

/// Pickers start with their groups shut except the selection's, open what a search matches,
/// and list what is not set up after the items.
@MainActor struct PickerSetupTests {
    private func item(_ id: String, _ group: String, facets: [String: String]? = nil) -> HarnessScreen.Picker.Item {
        .init(id: id, title: id, group: group, facets: facets)
    }

    private var picker: HarnessScreen.Picker {
        HarnessScreen.Picker(
            items: [item("bedrock/a", "amazon-bedrock"), item("bedrock/b", "amazon-bedrock"),
                    item("llama/q", "llama.cpp", facets: ["set_role_default": "true"]), item("kimi/k", "moonshot")],
            selected: "kimi/k", searchable: true, onSelect: "switch_model",
            itemActions: [HarnessActionRef(id: "set_role_default", label: "Default")],
            setup: [.init(id: "anthropic", title: "Anthropic (Claude Pro/Max)"), .init(id: "openai-codex", title: "ChatGPT Plus/Pro")],
            setupAction: HarnessActionRef(id: "login", label: "Sign in")
        )
    }

    @Test func onlyTheSelectionsGroupStartsOpen() {
        let model = PickerModel(picker)
        let groups = model.groups(picker)
        #expect(groups.map { model.isExpanded($0) } == [false, false, true])
        #expect(!model.isSetupExpanded)
    }

    @Test func withoutASelectionTheDefaultHoldersGroupOpens() {
        var p = picker
        p.selected = nil
        let model = PickerModel(p)
        #expect(model.groups(p).map { model.isExpanded($0) } == [false, true, false])
    }

    @Test func aPickersOnlyGroupIsOpen() {
        let p = HarnessScreen.Picker(items: [item("a", "one"), item("b", "one")], searchable: false, onSelect: "use")
        let model = PickerModel(p)
        #expect(model.isExpanded(model.groups(p)[0]))
    }

    @Test func aSearchOpensTheGroupsItMatches() {
        var model = PickerModel(picker)
        model.query = "bedrock"
        let groups = model.groups(picker)
        #expect(groups.map(\.name) == ["amazon-bedrock"])
        #expect(groups.allSatisfy { model.isExpanded($0) })
        model.query = "anthropic"
        #expect(model.setupItems(picker).map(\.id) == ["anthropic"])
        #expect(model.isSetupExpanded)
    }

    @Test func aFilterLeavingOneGroupShowsIt() {
        let model = PickerModel(picker)
        let group = model.groups(picker)[0]
        #expect(!model.isExpanded(group))
        #expect(model.isExpanded(group, alone: true))
    }

    @Test func foldingTogglesOneGroup() {
        var model = PickerModel(picker)
        let bedrock = model.groups(picker)[0]
        model.toggle(bedrock)
        #expect(model.isExpanded(bedrock))
        model.toggle(bedrock)
        #expect(!model.isExpanded(bedrock))
        model.toggle(id: PickerModel.setupID)
        #expect(model.isSetupExpanded)
    }

    @Test func setupHidesUnderAFilterOrWithoutAnAction() {
        var model = PickerModel(picker)
        #expect(model.setupItems(picker).count == 2)
        model.filterValues["provider"] = "moonshot"
        #expect(model.setupItems(picker).isEmpty)
        var p = picker
        p.setupAction = nil
        #expect(PickerModel(p).setupItems(p).isEmpty)
    }

    @Test func setupDecodesFromTheHelper() throws {
        let json = #"{"items":[],"searchable":true,"onSelect":"use","setup":[{"id":"zai","title":"Z.AI"}],"setupAction":{"id":"add_provider_key","label":"Add key","role":"normal"},"setupInput":"secret"}"#
        let p = try JSONDecoder().decode(HarnessScreen.Picker.self, from: Data(json.utf8))
        #expect(p.setup?.first?.title == "Z.AI")
        #expect(p.setupAction?.label == "Add key")
        #expect(p.setupInput == "secret")
    }

    /// Signed in from "Not set up": back on the list, the provider's models show in its group,
    /// open, whatever search or filter hid them before.
    @Test func aProviderJustSetUpOpensItsGroup() {
        var model = PickerModel(picker)
        model.query = "anthr"
        model.filterValues["provider"] = "moonshot"
        var after = picker
        after.items.append(item("anthropic/claude-haiku-4-5", "anthropic"))
        after.setup = [.init(id: "openai-codex", title: "ChatGPT Plus/Pro")]
        model.reveal(setUp: .init(id: "anthropic", title: "Anthropic (Claude Pro/Max)"))
        let anthropic = model.groups(after).first { $0.name == "anthropic" }
        #expect(anthropic.map { model.isExpanded($0) } == true)
        #expect(anthropic?.items.map(\.id) == ["anthropic/claude-haiku-4-5"])
        #expect(model.query.isEmpty)
    }

    /// omp's switch says it is for this session only; the item's button uses those words.
    @Test func selectLabelDecodesFromTheHelper() throws {
        let json = #"{"items":[],"searchable":true,"onSelect":"switch_model","onSelectLabel":"Use for this session"}"#
        let p = try JSONDecoder().decode(HarnessScreen.Picker.self, from: Data(json.utf8))
        #expect(p.onSelectLabel == "Use for this session")
    }
}
