import Foundation
import Testing
@testable import TogoKit

/// The program's own pickers, recorded from OMP 18.20 in isolated sessions
/// (paths shortened, lists trimmed, shapes untouched), mapped to the shared
/// picker screen.
struct NativePickerScreenTests {
    static let at = WireDate(Date(timeIntervalSince1970: 1_800_000_000))

    static func node(_ json: String) throws -> SurfaceNode {
        try JSONDecoder().decode(SurfaceNode.self, from: Data(json.utf8))
    }

    /// A recorded picker node from Fixtures/omp-picker-<name>.json.
    static func recorded(_ name: String) throws -> String {
        let url = try #require(Bundle.module.url(forResource: "omp-picker-\(name)", withExtension: "json", subdirectory: "Fixtures"))
        return try String(contentsOf: url, encoding: .utf8)
    }

    @Test func conversationPickerKeepsRowsGroupsPreviewAndRowActions() throws {
        let screen = try NativePickerScreen.map(try Self.node(Self.recorded("resume")), workflow: "resume", fallbackTitle: "Resume", fetchedAt: Self.at)
        let picker = try #require(screen.picker)
        #expect(screen.primitive == .picker)
        #expect(screen.id == "n")
        #expect(screen.title == "Resume")
        #expect(picker.items.map(\.title) == ["Native check A"])
        let item = try #require(picker.items.first)
        #expect(item.id == "/sessions/project/a.jsonl")
        #expect(item.subtitle == "Reply with only these four words: Native check A ready")
        #expect(item.group == "Today")
        #expect(item.facets?["size"] == "3.2KB")
        #expect(item.facets?["when"] == "now")
        #expect(item.facets?["native.dot"] == "success")
        // In use now is the session's own file, which is not a row: nothing
        // is marked selected, and the cursor is never taken for it.
        #expect(picker.selected == nil)
        #expect(picker.onSelect == NativePickerScreen.activate)
        #expect(picker.onSelectLabel == "Resume")
        #expect(picker.searchable)
        #expect(picker.searchMode == "remote")
        #expect(picker.selectionBehavior == "preview")
        #expect(picker.itemActions == nil)
        let row = try #require(picker.contextActions)
        #expect(row.map(\.id) == ["native.action.delete", "native.action.rename", "native.action.pin"])
        #expect(row.map(\.label) == ["Delete", "Rename", "Pin"])
        #expect(row.map(\.role) == [.destructive, .normal, .normal])
        #expect(row[0].args == ["act": .string("delete")])
        // The list-wide action; Close is the system's, Resume is the choice.
        #expect(screen.actions.map(\.id) == ["native.action.scope"])
        #expect(screen.actions.first?.label == "All projects")
        let preview = try #require(item.preview)
        #expect(preview.subtitle == "Native check A")
        #expect(preview.body.kind == "kv")
        #expect(preview.body.kv?.map(\.label) == ["Folder", "Created", "Modified", "Size", "Status"])
        #expect(preview.body.kv?.first?.value == "/tmp/001")
        #expect(preview.body.kv?.last?.value == "done")
        let sections = try #require(preview.sections)
        #expect(sections.map(\.title) == ["Conversation", nil])
        #expect(sections.map(\.body.kind) == ["markdown", "markdown"])
        #expect(sections.map(\.body.text) == ["Reply with only these four words: Native check A ready", "Native check A ready"])
    }

    @Test(arguments: ["", "Native check A"])
    func sourceSearchIsPreserved(query: String) throws {
        var node = try Self.node(Self.recorded("resume"))
        node.properties["query"] = .string(query)
        let screen = try NativePickerScreen.map(node, workflow: "resume", fallbackTitle: "Resume", fetchedAt: Self.at)
        #expect(screen.picker?.query == query)
    }

    @Test func aMarkedCurrentRowIsSelected() throws {
        var json = try Self.recorded("resume")
        json = json.replacingOccurrences(of: "/sessions/project/current.jsonl", with: "/sessions/project/a.jsonl")
        let screen = try NativePickerScreen.map(try Self.node(json), workflow: "resume", fallbackTitle: "Resume", fetchedAt: Self.at)
        #expect(screen.picker?.selected == "/sessions/project/a.jsonl")
    }

    @Test func signInPickerHasNoRowActionsAndUsesTheProgramsTitle() throws {
        let screen = try NativePickerScreen.map(try Self.node(Self.recorded("login")), workflow: "login", fallbackTitle: "Sign in", fetchedAt: Self.at)
        let picker = try #require(screen.picker)
        #expect(picker.items.count == 3)
        #expect(picker.onSelectLabel == "Sign in")
        #expect(picker.contextActions == nil)
        #expect(screen.actions.isEmpty)
        #expect(picker.selected == nil)
        #expect(picker.items.allSatisfy({ $0.preview == nil }))
        #expect(!screen.title.isEmpty)
    }

    @Test func modelPickerKeepsGroupsBadgesTabsAndScopes() throws {
        let screen = try NativePickerScreen.map(try Self.node(Self.recorded("model")), workflow: "model", fallbackTitle: "Model", fetchedAt: Self.at)
        let picker = try #require(screen.picker)
        #expect(picker.items.count == 4)
        #expect(picker.items.first?.group == "Recent")
        #expect(picker.items.first?.badge == "free")
        #expect(picker.items.first?.facets?["native.mono"] == "true")
        #expect(picker.onSelectLabel == "Assign role")
        let tabs = screen.actions.filter({ $0.id.hasPrefix(NativePickerScreen.tabPrefix) })
        let scopes = screen.actions.filter({ $0.id.hasPrefix(NativePickerScreen.scopePrefix) })
        #expect(tabs.first?.id == "native.tab.all")
        #expect(tabs.first?.args?["selected"] == .bool(true))
        #expect(tabs.dropFirst().allSatisfy({ $0.args?["selected"] == .bool(false) }))
        #expect(scopes.count == 3)
        #expect(scopes.contains(where: { $0.args?["selected"] == .bool(true) }))
        let preview = try #require(picker.items.first(where: { $0.preview != nil })?.preview)
        #expect(preview.subtitle == "Parallel")
        #expect(preview.body.kv?.isEmpty == false)
    }

    /// What the program says about the list and about each row beyond its
    /// title reaches the shared picker.
    @Test func listAndRowAppearanceAreKept() throws {
        let resume = try #require(try NativePickerScreen.map(try Self.node(Self.recorded("resume")), workflow: "resume", fallbackTitle: "Resume", fetchedAt: Self.at).picker)
        let list = try #require(resume.presentation)
        #expect(list.emptyText == "No sessions in this folder yet")
        #expect(list.searchPrompt?.hasPrefix("Search sessions in") == true)
        #expect(list.noun == "sessions")
        #expect(list.state == "ready")
        #expect(list.total == 1)
        #expect(list.columns == [.init(id: "when", format: "time"), .init(id: "size", format: "dim")])
        #expect(resume.items.first?.appearance?.status == "success")

        let model = try #require(try NativePickerScreen.map(try Self.node(Self.recorded("model")), workflow: "model", fallbackTitle: "Model", fetchedAt: Self.at).picker)
        let row = try #require(model.items.first?.appearance)
        #expect(row.badges == [.init(text: "free", tone: "success")])
        #expect(row.chips == [.init(text: "web", on: false, auto: true)])
        #expect(row.mark == .init(text: "We", icon: "provider:web", seed: "web"))

        let tree = try #require(try NativePickerScreen.map(try Self.node(Self.recorded("tree")), workflow: "tree", fallbackTitle: "Tree", fetchedAt: Self.at).picker)
        #expect(tree.items.first?.appearance?.depth == 0)
        #expect(tree.items.first?.appearance?.nodeKind == "marker")
        #expect(tree.items.first?.appearance?.status == "accent")

        // A list or a row that says nothing extra carries nothing extra.
        let bare = try #require(try NativePickerScreen.map(try Self.node(#"{"id":"n","k":"picker","p":{"items":[{"id":"a","label":"A"}]}}"#), workflow: "login", fallbackTitle: "", fetchedAt: Self.at).picker)
        #expect(bare.presentation == nil)
        #expect(bare.items.first?.appearance == nil)
    }

    /// Nothing is guessed: a picker whose extra actions are not classified
    /// for the workflow is refused, naming them.
    @Test func unclassifiedActionsAreRefused() throws {
        let tree = try NativePickerScreen.map(try Self.node(Self.recorded("tree")), workflow: "tree", fallbackTitle: "Tree", fetchedAt: Self.at)
        #expect(tree.picker?.contextActions?.map(\.id) == ["native.action.summarize", "native.action.label"])
        #expect(tree.picker?.contextActions?.allSatisfy({ $0.role == .normal }) == true)
        #expect(tree.actions.contains(where: { $0.id == "native.action.filter" }))
        #expect(tree.picker?.onSelectLabel == "Switch")
        // A label given as styled spans reads as its text.
        #expect(tree.picker?.items.first?.title == "[thinking: high]")
        #expect(tree.picker?.items.first?.facets?["native.depth"] == "0")
        #expect(tree.picker?.items.first?.facets?["native.node"] == "marker")
        #expect(tree.picker?.selected == tree.picker?.items.first?.id)
        #expect(throws: NativePickerScreen.Unsupported.unknownActions(workflow: "other", actions: ["delete", "rename", "pin", "scope"])) {
            try NativePickerScreen.map(try Self.node(Self.recorded("resume")), workflow: "other", fallbackTitle: "", fetchedAt: Self.at)
        }
    }

    @Test func otherNodesAndNestedControlsAreRefused() throws {
        #expect(throws: NativePickerScreen.Unsupported.notAPicker(kind: "prefs")) {
            try NativePickerScreen.map(try Self.node(#"{"id":"s","k":"prefs"}"#), workflow: "settings", fallbackTitle: "", fetchedAt: Self.at)
        }
        let rename = #"{"id":"n","k":"picker","p":{"items":[],"actions":[]},"c":[{"id":"n.r","k":"input","p":{"text":"Native check A"}}]}"#
        #expect(throws: NativePickerScreen.Unsupported.nestedControl(kind: "input")) {
            try NativePickerScreen.map(try Self.node(rename), workflow: "resume", fallbackTitle: "", fetchedAt: Self.at)
        }
        let nameless = #"{"id":"n","k":"picker","p":{"items":[{"id":"x"}]}}"#
        #expect(throws: NativePickerScreen.Unsupported.malformedItem) {
            try NativePickerScreen.map(try Self.node(nameless), workflow: "login", fallbackTitle: "", fetchedAt: Self.at)
        }
    }
}
