import Foundation
import Testing
@testable import TogoKit

/// Streamed text may arrive as a tail with `appendFrom`, the UTF-8 byte offset it continues at.
struct HarnessAppendTests {
    typealias S = HarnessSamples

    static func tail(_ id: String, seq: Int, from: Int, text: String, partial: Bool = true, thinking: Bool = false) -> HarnessItem {
        var t = HarnessItem.Text(text: text, partial: partial)
        t.appendFrom = from
        return HarnessItem(id: id, seq: seq, turnId: "u1", at: S.at(0), body: thinking ? .thinking(t) : .assistant(t))
    }

    static func text(_ item: HarnessItem?) -> HarnessItem.Text? {
        switch item?.body {
        case .assistant(let t)?, .thinking(let t)?: t
        default: nil
        }
    }

    @Test func decodesAppendFromAndOmitsItWhenAbsent() throws {
        let json = #"{"id":"m","seq":2,"kind":"assistant","at":"2026-01-01T00:00:00Z","assistant":{"text":"lo","partial":true,"appendFrom":3}}"#
        let item = try JSONDecoder().decode(HarnessItem.self, from: Data(json.utf8))
        #expect(Self.text(item)?.appendFrom == 3)
        let full = try JSONEncoder().encode(HarnessItem.Text(text: "a", partial: false))
        #expect(!String(decoding: full, as: UTF8.self).contains("appendFrom"))
    }

    @Test func tailAtTheHeldLengthAppends() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.reply("m", seq: 1, turn: "u1", text: "héllo", partial: true)]))
        #expect(timeline.apply(S.items([Self.tail("m", seq: 2, from: "héllo".utf8.count, text: " world")])) == .applied)
        let text = Self.text(timeline.item("m"))
        #expect(text?.text == "héllo world")
        #expect(text?.appendFrom == nil)
        #expect(timeline.cursor == 2)
    }

    @Test func tailMayEndTheStream() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.reply("m", seq: 1, turn: "u1", text: "ab", partial: true)]))
        #expect(timeline.apply(S.items([Self.tail("m", seq: 2, from: 2, text: "c", partial: false)])) == .applied)
        #expect(Self.text(timeline.item("m")) == HarnessItem.Text(text: "abc", partial: false))
    }

    @Test func fullTextStillReplaces() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.reply("m", seq: 1, turn: "u1", text: "ab", partial: true)]))
        #expect(timeline.apply(S.items([S.reply("m", seq: 2, turn: "u1", text: "abcd", partial: true)])) == .applied)
        #expect(Self.text(timeline.item("m"))?.text == "abcd")
    }

    @Test func mismatchedBaseReopensWithoutMovingTheCursor() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.reply("m", seq: 1, turn: "u1", text: "ab", partial: true)]))
        #expect(timeline.apply(S.items([Self.tail("m", seq: 2, from: 5, text: "x")])) == .reopen)
        #expect(Self.text(timeline.item("m"))?.text == "ab")
        #expect(timeline.cursor == 1)
    }

    @Test func tailForAnUnknownItemReopens() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.user("u1", seq: 1)]))
        #expect(timeline.apply(S.items([Self.tail("m", seq: 2, from: 0, text: "x")])) == .reopen)
        #expect(timeline.item("m") == nil)
    }

    @Test func tailOfAnotherKindReopens() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.reply("m", seq: 1, turn: "u1", text: "ab", partial: true)]))
        #expect(timeline.apply(S.items([Self.tail("m", seq: 2, from: 2, text: "x", thinking: true)])) == .reopen)
    }

    @Test func feedReopensFromTheCursorAfterAMismatch() {
        var feed = HarnessFeedState()
        feed.load(S.open(cursor: 1, items: [S.reply("m", seq: 1, turn: "u1", text: "ab", partial: true)]))
        #expect(feed.apply(S.items([Self.tail("m", seq: 2, from: 9, text: "x")])) == .reopen)
        #expect(feed.openAfter == 1)
        feed.load(S.open(cursor: 2, items: [S.reply("m", seq: 2, turn: "u1", text: "abcx", partial: true)]))
        #expect(Self.text(feed.timeline.item("m"))?.text == "abcx")
    }

    @Test func tailInAnOpenResultThatDoesNotFitIsSkipped() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.reply("m", seq: 1, turn: "u1", text: "ab", partial: true)]))
        timeline.load(S.open(cursor: 2, items: [Self.tail("m", seq: 2, from: 7, text: "x")]))
        #expect(Self.text(timeline.item("m"))?.text == "ab")
    }
}

/// One malformed item must not fail a whole open, page or notification.
struct HarnessLenientItemsTests {
    static let good = #"{"id":"m","seq":2,"kind":"assistant","at":"2026-01-01T00:00:00Z","assistant":{"text":"hi","partial":false}}"#
    static let bad = #"{"id":"x","seq":1,"kind":"assistant","at":"2026-01-01T00:00:00Z","assistant":{"partial":false}}"#
    static var mission: String {
        String(decoding: try! JSONEncoder().encode(HarnessSamples.mission(rev: 1)), as: UTF8.self)
    }

    @Test func openSkipsAMalformedItem() throws {
        let json = #"{"epoch":"e1","cursor":2,"more":false,"mission":"# + Self.mission + #","items":["# + Self.bad + "," + Self.good + "]}"
        let result = try JSONDecoder().decode(HarnessOpenResult.self, from: Data(json.utf8))
        #expect(result.items.map(\.id) == ["m"])
    }

    @Test func pageSkipsAMalformedItem() throws {
        let json = #"{"more":true,"items":["# + Self.bad + "," + Self.good + "]}"
        let page = try JSONDecoder().decode(HarnessPage.self, from: Data(json.utf8))
        #expect(page.items.map(\.id) == ["m"])
        #expect(page.more)
    }
}
