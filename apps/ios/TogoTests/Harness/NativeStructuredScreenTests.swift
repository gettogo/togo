import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct NativeStructuredScreenTests {
    @Test func allOriginalPreferencesPagesMapToSharedNativeFields() throws {
        let data = try fixture("omp-native-preferences-pages")
        let pages = try JSONDecoder().decode([SurfaceNode].self, from: data)
        #expect(pages.count == 11)
        var kinds: Set<String> = []
        var count = 0
        for page in pages {
            let screen = try NativeStructuredScreen.preferences(page, title: "Settings")
            let settings = try #require(screen.settings)
            #expect(screen.primitive == .settings)
            let fields = settings.sections.flatMap(\.fields)
            count += fields.count
            kinds.formUnion(fields.map { $0.kind.rawValue })
            #expect(fields.allSatisfy { $0.action != nil })
            for field in fields where field.kind == .secret { #expect(field.value == .null) }
        }
        #expect(count > 300)
        #expect(kinds.isSuperset(of: ["toggle", "choice", "multi", "number", "text", "action"]))
    }

    @Test(arguments: ["hotkeys", "session", "tools"])
    func originalReadOnlyOverlaysUseNativeDetails(_ command: String) throws {
        let node = try JSONDecoder().decode(SurfaceNode.self, from: fixture("omp-native-" + command))
        let screen = try NativeStructuredScreen.detail(node, title: command)
        #expect(screen.primitive == .detail)
        let detail = try #require(screen.detail)
        #expect(!screen.title.isEmpty)
        #expect(!detail.actions.contains { $0.label == "Close" })
        #expect(DetailCardLogic.kind(detail.body) != .empty)
        if command == "hotkeys" {
            #expect((detail.sections ?? []).count == 2)
            #expect((detail.sections ?? []).allSatisfy { $0.body.kind == "kv" })
            #expect((detail.body.kv ?? []).count == 4)
            #expect((detail.sections ?? []).reduce(0) { $0 + ($1.body.kv ?? []).count } == 41)
        }
    }

    @Test func multipleChoiceFeedbackUsesOriginalOptionLabels() {
        let field = HarnessScreen.Settings.Field(id: "tools", label: "Tools", kind: .multi,
            value: .array([.string("read")]), options: [.init(id: "read", label: "Read"), .init(id: "write", label: "Write")], action: "native.change")
        #expect(SettingsLogic.words(field, .array([.string("read"), .string("write")])) == "Read, Write")
        #expect(SettingsLogic.isEditable(field))
    }

    @Test func originalFormUsesNativeFieldsAndOriginalSubmit() throws {
        let node = try JSONDecoder().decode(SurfaceNode.self, from: fixture("omp-native-form"))
        let screen = try NativeFormScreen.map(node, title: "Form")
        let fields = try #require(screen.settings).sections.flatMap(\.fields)
        let memo = try #require(fields.first { $0.id == "p" })
        // Original input has no password flag: its value must not be shown.
        #expect(memo.kind == .secret)
        #expect(memo.value == .null)
        #expect(memo.isSet == true)
        #expect(fields.contains { $0.kind == .multi && $0.options?.map(\.label) == ["Red", "Blue"] })
        #expect(fields.contains { $0.action == "native.optionFocus" })
        let mode = try #require(fields.first { $0.kind == .choice })
        #expect(mode.value == .string("o.body0/0/s0/2/f2/o0"))
        #expect(screen.actions.contains { $0.args?["act"] == .string("submit") })
        #expect(!screen.actions.contains { $0.args?["act"] == .string("hide") })
    }

    @Test func sourcePasswordFlagCannotBeOverriddenByPlainWorkflow() throws {
        let node = try input("password", properties: ["secret": .bool(true), "text": .string("private")])
        let field = NativeFormScreen.editor(node, label: "Password", workflow: "resume")
        #expect(field.kind == .secret)
        #expect(field.value == .null)
        #expect(SettingsLogic.initialText(field).isEmpty)
    }

    @Test func knownRenameKeepsExistingTitle() throws {
        let node = try input("r", properties: ["text": .string("Saved conversation")])
        let field = NativeFormScreen.editor(node, label: "Rename session", workflow: "resume")
        #expect(field.kind == .text)
        #expect(field.value == .string("Saved conversation"))
    }

    @Test func unmaskedInputMetadataDoesNotMeanNonSecret() throws {
        let node = try input("r", properties: ["secret": .bool(false), "text": .string("Plain answer")])
        let legacy = NativeFormScreen.editor(node, label: "Answer", workflow: "form")
        let negotiated = NativeFormScreen.editor(node, label: "Answer", workflow: "form", features: ["secret"])
        #expect(legacy.kind == .secret && legacy.value == .null)
        #expect(negotiated.kind == .secret && negotiated.value == .null)
    }

    @Test func renameSubmitIsAbsentWithoutCompanionSupport() throws {
        let node = try JSONDecoder().decode(SurfaceNode.self, from: fixture("omp-native-rename"))
        #expect(throws: (any Error).self) { try NativeFormScreen.map(node, title: "Resume", workflow: "resume") }
    }

    @Test func recordedRenameProvidesOneOriginalSubmit() throws {
        let node = try JSONDecoder().decode(SurfaceNode.self, from: fixture("omp-native-rename"))
        let screen = try NativeFormScreen.map(node, title: "Resume", workflow: "resume", features: ["submit"])
        #expect(screen.title == "Rename Session")
        let fields = try #require(screen.settings).sections.flatMap(\.fields)
        #expect(fields.first?.value == .string("Native check A"))
        #expect(screen.actions.count == 1)
        #expect(screen.actions.first?.id == "native.editorSubmit.r")
    }

    @Test func renamePresentationExcludesUnderlyingScopeAndKeyboardHints() throws {
        let root = try JSONDecoder().decode(SurfaceNode.self, from: fixture("omp-native-rename-view"))
        let node = NativeStructuredScreen.primary(root)
        #expect(node.properties["head"] == .string("Rename Session"))
        #expect(node.node(id: "n.scope") == nil)
        let screen = try NativeFormScreen.map(node, title: "Resume", workflow: "resume", features: ["submit"])
        #expect(screen.settings?.sections.flatMap(\.fields).map(\.id) == ["r"])
        #expect(screen.settings?.sections.contains { $0.footer == "all projects" } == false)
    }

    @Test func recordedLoginKeepsCodeWriteOnlyAndExposesOriginalActions() throws {
        let node = try JSONDecoder().decode(SurfaceNode.self, from: fixture("omp-native-login"))
        let screen = try NativeFormScreen.map(node, title: "Sign in", workflow: "login")
        let fields = try #require(screen.settings).sections.flatMap(\.fields)
        #expect(fields.filter { $0.action == "native.edit" }.allSatisfy { $0.kind == .secret && $0.value == .null })
        #expect(Set(screen.actions.compactMap { SurfaceValues.string($0.args?["act"]) }) == ["copy", "open", "submit"])
        #expect(screen.settings?.sections.contains { $0.footer?.contains("Waiting for browser authentication") == true } == true)
    }

    @Test func recordedRoleStripCommitsOnOneTapIncludingCurrentFocus() throws {
        let node = try JSONDecoder().decode(SurfaceNode.self, from: fixture("omp-native-role-strip"))
        let screen = try NativeFormScreen.map(node, title: "Models", workflow: "model")
        let picker = try #require(screen.picker)
        #expect(picker.onSelect == "native.strip")
        #expect(picker.selected == nil)
        #expect(picker.items.map(\.title) == ["web", "fallbacks:parallel", "fallbacks:web/*"])
        #expect(screen.actions.isEmpty)
    }

    @Test func recordedDockInputUsesOriginalHeadingAndSubmitWithoutReadingItsValue() throws {
        let node = try JSONDecoder().decode(SurfaceNode.self, from: fixture("omp-native-hook-input"))
        let screen = try NativeFormScreen.map(NativeStructuredScreen.primary(node), title: "Native-entry")
        #expect(screen.title == "Neutral entry check")
        let sections = try #require(screen.settings).sections
        let field = try #require(sections.flatMap(\.fields).first)
        #expect(sections.flatMap(\.fields).count == 1)
        #expect(field.label == "Neutral entry check")
        #expect(field.kind == .secret && field.value == .null)
        #expect(field.placeholder == "Enter neutral text")
        #expect(sections.compactMap(\.footer).isEmpty)
        #expect(screen.actions.count == 1)
        #expect(screen.actions[0].label == "Submit")
        #expect(screen.actions[0].args?["act"] == .string("submit"))
    }

    @Test func recordedPickerOrderOwnsVisibleRowsAcrossSearchAndClear() throws {
        let nodes = try JSONDecoder().decode([SurfaceNode].self, from: fixture("omp-native-resume-search"))
        #expect(nodes.count == 3)
        var counts: [Int] = []
        for node in nodes {
            #expect(!SurfaceValues.array(node.properties["items"]).isEmpty)
            let screen = try NativePickerScreen.map(node, workflow: "resume", fallbackTitle: "Resume", fetchedAt: WireDate(.now))
            counts.append(try #require(screen.picker).items.count)
        }
        #expect(counts == [1, 0, 1])
        var noMatch = nodes[1]
        noMatch.properties["current"] = .array(SurfaceValues.entries(noMatch.properties["items"]).map { .string($0.id) })
        #expect(try NativePickerScreen.map(noMatch, workflow: "resume", fallbackTitle: "Resume", fetchedAt: WireDate(.now)).picker?.selected == nil)
        noMatch.properties.removeValue(forKey: "order")
        // Legacy nodes without an order still expose their supplied items.
        #expect(try NativePickerScreen.map(noMatch, workflow: "resume", fallbackTitle: "Resume", fetchedAt: WireDate(.now)).picker?.items.count == 1)
    }

    private func fixture(_ name: String) throws -> Data {
        let url = try #require(Bundle(for: NativeStructuredFixtureBundle.self).url(forResource: name, withExtension: "json"))
        return try Data(contentsOf: url)
    }

    private func input(_ id: String, properties: [String: JSONValue]) throws -> SurfaceNode {
        let data = try JSONEncoder().encode(JSONValue.object(["id": .string(id), "k": .string("input"), "p": .object(properties)]))
        return try JSONDecoder().decode(SurfaceNode.self, from: data)
    }
}

private final class NativeStructuredFixtureBundle {}
