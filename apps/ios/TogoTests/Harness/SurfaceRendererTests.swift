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

    @Test func aDisabledReasonAlsoDisablesTheControl() {
        #expect(SurfaceValues.disabled(.string("Unavailable for this model")))
        #expect(SurfaceValues.disabled(.bool(true)))
        #expect(!SurfaceValues.disabled(.bool(false)))
        #expect(!SurfaceValues.disabled(nil))
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
