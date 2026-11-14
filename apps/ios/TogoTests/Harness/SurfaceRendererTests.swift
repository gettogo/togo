import Foundation
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct SurfaceRendererTests {
    @Test func recordedResumePickerKeepsEveryOriginalAction() throws {
        let bundle = Bundle(for: SurfaceFixtureBundle.self)
        let url = try #require(bundle.url(forResource: "omp-native-picker", withExtension: "json"))
        let node = try JSONDecoder().decode(SurfaceNode.self, from: Data(contentsOf: url))
        #expect(node.kind == "picker")
        #expect(SurfaceNodeView.kinds.contains(node.kind))
        let actions = SurfaceValues.array(node.properties["actions"]).map(SurfaceValues.object)
        #expect(actions.compactMap { SurfaceValues.string($0["id"]) } == ["resume", "delete", "rename", "pin", "scope", "close"])
        #expect(SurfaceValues.text(node.properties["empty"]) == "No sessions yet")
        #expect(SurfacePickerContent.entries(node.properties).isEmpty)
    }

    @Test func chatShowsOriginalFormsWithoutDuplicatingTheTranscriptOrComposer() throws {
        let scene = try originalFormScene()
        let root = scene.views[0].document.root
        let form = try #require(SurfaceValues.chatInteraction(root, mode: .inline))
        #expect(form.properties["role"] == .string("omp.editor.ask"))
        var ordinary = root
        ordinary.children = []
        #expect(SurfaceValues.chatInteraction(ordinary, mode: .inline) == nil)
        #expect(SurfaceValues.chatInteraction(ordinary, mode: .screen)?.id == ordinary.id)
    }

    @Test func wholeFieldEditUsesUTF16OffsetsAndPreservesLiteralText() {
        let event = SurfaceValues.edit(from: "🧑🏽‍💻 draft", to: "<div>\n100%! /resume")
        #expect(event["len"] == .number(Double("🧑🏽‍💻 draft".utf16.count)))
        #expect(event["to"] == event["len"])
        #expect(event["text"] == .string("<div>\n100%! /resume"))
        #expect(event["cursor"] == .number(Double("<div>\n100%! /resume".utf16.count)))
    }

    @Test func originalInlineFormTakesTheSheetInsteadOfPastOutput() throws {
        let bundle = Bundle(for: SurfaceFixtureBundle.self)
        let url = try #require(bundle.url(forResource: "omp-native-form", withExtension: "json"))
        let data = try Data(contentsOf: url)
        let form = try JSONSerialization.jsonObject(with: data)
        let wrapper: [String: Any] = ["id": "session", "k": "col", "c": [
            ["id": "past", "k": "text", "p": ["text": "Past output"]],
            ["id": "dock", "k": "col", "c": [form]]
        ]]
        let root = try JSONDecoder().decode(SurfaceNode.self, from: JSONSerialization.data(withJSONObject: wrapper))
        let shown = SurfaceValues.presentedNode(root)
        #expect(shown.properties["role"] == .string("omp.editor.ask"))
        #expect(shown.id != root.id)
        func check(_ node: SurfaceNode) {
            #expect(SurfaceNodeView.kinds.contains(node.kind))
            node.children.forEach(check)
        }
        check(shown)
    }

    @Test func draftsSurviveLayoutAndSameRuntimeReopenButNotAnotherRuntime() {
        let drafts = SurfaceDrafts()
        let key = SurfaceDrafts.Key(surface: "session", instance: 1, node: "input")
        drafts.bind("one")
        drafts.set("phone text", key: key, producer: "desktop text")
        drafts.bind("one")
        #expect(drafts.draft(key)?.text == "phone text")
        #expect(drafts.draft(key)?.base == "desktop text")
        drafts.bind("two")
        #expect(drafts.draft(key) == nil)
    }

    @Test func unfocusedOriginalFieldKeepsItsUnappliedPhoneDraft() throws {
        let scene = try originalFormScene()
        func input(_ node: SurfaceNode) -> SurfaceNode? {
            if node.kind == "input" { return node }
            return node.children.lazy.compactMap(input).first
        }
        let field = try #require(input(scene.views[0].document.root))
        let drafts = SurfaceDrafts()
        drafts.bind("runtime")
        let key = SurfaceDrafts.Key(surface: scene.views[0].id, instance: scene.views[0].instance, node: field.id)
        drafts.set("unapplied phone text", key: key, producer: SurfaceValues.string(field.properties["text"]) ?? "")
        // Original OMP replaces an unfocused Input with a text well; its field
        // remains, and the same Input returns when the field gets focus again.
        func unfocus(_ node: SurfaceNode) -> SurfaceNode {
            var result = node
            result.children = node.children.filter { $0.id != field.id }.map(unfocus)
            return result
        }
        var missing = scene
        missing.views[0].document.root = unfocus(scene.views[0].document.root)
        drafts.reconcile(missing)
        #expect(drafts.draft(key)?.text == "unapplied phone text")
        drafts.reconcile(scene)
        #expect(drafts.draft(key)?.text == "unapplied phone text")
        missing.views = []
        drafts.reconcile(missing)
        #expect(drafts.draft(key) == nil)
    }

    private func originalFormScene() throws -> SurfaceScene {
        let url = try #require(Bundle(for: SurfaceFixtureBundle.self).url(forResource: "omp-native-form", withExtension: "json"))
        let form = try JSONSerialization.jsonObject(with: Data(contentsOf: url))
        let value: [String: Any] = [
            "program": "omp", "features": ["edit"], "revision": 1,
            "options": ["kinds": SurfaceNodeView.kinds.sorted(), "cols": 80, "dark": true, "reduceMotion": false],
            "views": [["id": "s:1", "mode": "inline", "listen": true, "closed": false, "instance": 1,
                       "document": ["root": ["id": "s:1", "k": "col", "c": [form]], "sequence": 1,
                                    "revision": 1, "suspended": false]]]
        ]
        return try SurfaceScene.decode(JSONSerialization.data(withJSONObject: value), kinds: SurfaceNodeView.kinds)
    }

    @Test func aClosedOriginalFormDoesNotLendItsDraftToAnotherField() throws {
        var scene = try originalFormScene()
        func input(_ node: SurfaceNode) -> SurfaceNode? {
            if node.kind == "input" { return node }
            return node.children.lazy.compactMap(input).first
        }
        let root = scene.views[0].document.root
        let field = try #require(input(root))
        let anchor = SurfaceValues.draftAnchor(for: field.id, in: root)
        #expect(root.node(id: anchor)?.properties["role"] == .string("omp.ask.field"))
        let key = SurfaceDrafts.Key(surface: scene.views[0].id, instance: scene.views[0].instance, node: field.id, anchor: anchor)
        let drafts = SurfaceDrafts()
        drafts.set("held", key: key, producer: "original")
        func remove(_ node: SurfaceNode) -> SurfaceNode {
            var result = node
            result.children = node.children.filter { $0.id != anchor }.map(remove)
            return result
        }
        scene.views[0].document.root = remove(root)
        drafts.reconcile(scene)
        #expect(drafts.draft(key) == nil)
    }

    @Test func aDisabledReasonAlsoDisablesTheControl() {
        #expect(SurfaceValues.disabled(.string("Unavailable for this model")))
        #expect(SurfaceValues.disabled(.bool(true)))
        #expect(!SurfaceValues.disabled(.bool(false)))
        #expect(!SurfaceValues.disabled(nil))
    }

    @Test func queryDraftWaitsForTheOriginalPickerToReflectIt() throws {
        let url = try #require(Bundle(for: SurfaceFixtureBundle.self).url(forResource: "omp-native-picker", withExtension: "json"))
        var scene = try originalFormScene()
        var picker = try JSONDecoder().decode(SurfaceNode.self, from: Data(contentsOf: url))
        picker.properties["query"] = .string("")
        scene.views[0].document.root = picker
        let drafts = SurfaceDrafts()
        let key = SurfaceDrafts.Key(surface: scene.views[0].id, instance: scene.views[0].instance,
                                   node: picker.id, anchor: picker.id, property: "query")
        let text = "100%! <div> /resume 🧑🏽‍💻"
        drafts.set(text, key: key, producer: "")
        drafts.reconcile(scene)
        #expect(drafts.draft(key)?.text == text)
        scene.views[0].document.root.properties["query"] = .string("desk query")
        drafts.reconcile(scene)
        #expect(drafts.draft(key)?.base == "")
        #expect(drafts.draft(key)?.text == text)
        scene.views[0].document.root.properties["query"] = .string(text)
        drafts.reconcile(scene)
        #expect(drafts.draft(key) == nil)
    }

    @Test func hiddenTextSpansAreNotExposedAsOrdinaryText() {
        #expect(SurfaceValues.text(.array([
            .object(["t": .string("visible"), "s": .string("strong")]),
            .object(["t": .string("hidden"), "s": .string("dim hide")])
        ])) == "visible")
    }

    @Test func pickerOrderAndGroupHeadingsDoNotInventSelectableRows() {
        let properties: [String: JSONValue] = [
            "items": .array([
                .object(["id": .string("first"), "label": .string("First")]),
                .object(["id": .string("second"), "label": .string("Second")])
            ]),
            "order": .array([.object(["group": .string("today"), "label": .string("Today")]),
                             .string("second"), .string("missing"), .string("first")])
        ]
        let entries = SurfacePickerContent.entries(properties)
        #expect(entries.map(\.id) == ["group:today", "second", "first"])
        #expect(entries[0].properties["group"] == .string("today"))
    }

    @Test func displayReceiptRequiresAnAttachedVisibleDraw() async throws {
        let window = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
            .filter { $0.activationState == .foregroundActive }.flatMap(\.windows).first { !$0.isHidden })
        let probe = SurfaceDisplayProbe.Probe()
        let stamp = SurfaceDisplayStamp(generation: "runtime", revision: 7)
        probe.stamp = stamp
        probe.frame = CGRect(x: 0, y: 0, width: 4, height: 4)
        var receipts: [SurfaceDisplayStamp] = []
        probe.displayed = { receipts.append($0) }
        probe.setNeedsDisplay()
        try await Task.sleep(for: .milliseconds(80))
        #expect(receipts.isEmpty)
        probe.isHidden = true
        window.addSubview(probe)
        defer { probe.removeFromSuperview(); probe.invalidateReceipt() }
        try await Task.sleep(for: .milliseconds(80))
        #expect(receipts.isEmpty)
        probe.isHidden = false
        probe.setNeedsDisplay()
        let deadline = ContinuousClock.now + .seconds(2)
        while receipts.isEmpty, ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        #expect(receipts == [stamp])
    }
}

private final class SurfaceFixtureBundle: NSObject {}
