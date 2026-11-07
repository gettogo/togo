import Foundation
import Testing
import TogoKit
@testable import Togo

/// What the large picker shows where: the sidebar never repeats a filter, roles are a section
/// not filters, the selection's group leads, and rows carry the selector and facts as chips.
struct PickerLayoutTests {
    typealias Item = HarnessScreen.Picker.Item

    /// omp's model picker in miniature: provider groups, a provider filter that says the same,
    /// a reasoning toggle and role markers.
    private var models: HarnessScreen.Picker {
        HarnessScreen.Picker(
            items: [
                Item(id: "amazon-bedrock/nova", title: "Nova", subtitle: "300K context", group: "amazon-bedrock",
                     facets: ["provider": "amazon-bedrock"]),
                Item(id: "anthropic/opus", title: "Opus", subtitle: "200K context", badge: "reasoning", group: "anthropic",
                     facets: ["provider": "anthropic", "reasoning": "true", "set_role_default": "true"]),
                Item(id: "llama.cpp/qwen", title: "Qwen3.8 27B", subtitle: "64K context", group: "llama.cpp",
                     facets: ["provider": "llama.cpp", "reasoning": "true", "set_role_smol": "true"]),
            ],
            selected: "llama.cpp/qwen", searchable: true,
            filters: [
                .init(id: "provider", label: "Provider", kind: "choice"),
                .init(id: "reasoning", label: "Reasoning", kind: "toggle"),
                .init(id: "set_role_default", label: "Default", kind: "toggle"),
                .init(id: "set_role_smol", label: "Smol", kind: "toggle"),
            ],
            onSelect: "switch_model",
            itemActions: [
                HarnessActionRef(id: "set_role_default", label: "Default"),
                HarnessActionRef(id: "set_role_smol", label: "Smol"),
            ]
        )
    }

    private var resume: HarnessScreen.Picker {
        HarnessScreen.Picker(
            items: [Item(id: "0f1e2d3c", title: "Fix the build", subtitle: "2 h ago · main")],
            searchable: true, onSelect: "resume"
        )
    }

    @Test func aChoiceFilterThatPairsWithTheGroupsIsTheSidebarsNotTheMenus() {
        #expect(PickerModel.groupFilterIDs(models) == ["provider"])
        #expect(PickerModel.menuFilters(models, sidebar: true).map(\.id) == ["reasoning"])
        // A stack has no sidebar: the provider filter narrows its sections from the menu.
        #expect(PickerModel.menuFilters(models, sidebar: false).map(\.id) == ["provider", "reasoning"])
    }

    @Test func aFilterThatCutsAcrossGroupsIsNotAGroupFilter() {
        var picker = models
        picker.items[0].facets?["provider"] = "anthropic"
        #expect(PickerModel.groupFilterIDs(picker).isEmpty)
        #expect(PickerModel.groupFilterIDs(resume).isEmpty)
    }

    @Test func theSidebarLeadsWithTheSelectionsGroup() {
        let names = PickerModel.sidebarGroups(models).map(\.name)
        #expect(names == ["llama.cpp", "amazon-bedrock", "anthropic"])
        var picker = models
        picker.selected = nil
        #expect(PickerModel.sidebarGroups(picker).map(\.name) == ["amazon-bedrock", "anthropic", "llama.cpp"])
    }

    @Test func catalogRowsShowTheSelectorAndFactsAsChipsOnce() {
        let opus = models.items[1]
        #expect(PickerModel.selector(of: opus, in: models) == "anthropic/opus")
        // The badge "reasoning" and the Reasoning toggle are one chip; role markers are none.
        #expect(PickerModel.chips(of: opus, in: models) == ["200K context", "reasoning"])
        #expect(PickerModel.subtitleLine(of: opus, in: models) == nil)
    }

    @Test func ungroupedRowsKeepTheirSubtitleAsTextAndHideTheId() {
        let row = resume.items[0]
        #expect(PickerModel.selector(of: row, in: resume) == nil)
        #expect(PickerModel.chips(of: row, in: resume).isEmpty)
        #expect(PickerModel.subtitleLine(of: row, in: resume) == "2 h ago · main")
    }
}
